package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"io"
	"math"
	"net"
	"net/http"
	"os"

	capnp "capnproto.org/go/capnp/v3"
	"zenhack.net/go/tempest/capnp/grain"
	"zenhack.net/go/tempest/capnp/ip"
	"zenhack.net/go/tempest/capnp/powerbox"
	bridgecp "zenhack.net/go/tempest/capnp/sandstorm-http-bridge"
	"zenhack.net/go/tempest/pkg/exp/sandstormhttpbridge"
	"zenhack.net/go/util"
	"zenhack.net/go/util/exn"
	"zenhack.net/go/util/sync/mutex"
	"zenhack.net/go/util/thunk"
)

//go:embed template.html
var templateBytes string

var tmpl = template.Must(template.New("netcfg").Parse(templateBytes))

const host = "127.0.0.1:8000"

const tokenPath = "/var/ipnetwork-proxy/token"

var (
	dialer    = &net.Dialer{}
	transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", host)
		},
	}
)

type State struct {
	token   []byte
	network ip.IpNetwork
	config  Config
}

func (s State) HasNetwork() bool {
	return s.network.IsValid()
}

func (s State) HasConfig() bool {
	return s.config.IsValid()
}

func (s State) Ready() bool {
	return s.HasNetwork() && s.HasConfig()
}

// Returns the packed, base64 encoded powerbox descriptor needed to
// request network access. XXX: this is a bit of a hack; this really
// has nothing to do with the State, but it's the easiest way to get this
// into the template.
func (s State) PowerboxQuery() string {
	return powerboxQuery.Force()
}

var powerboxQuery = thunk.Lazy(func() string {
	msg, seg := capnp.NewSingleSegmentMessage(nil)
	desc, err := powerbox.NewRootPowerboxDescriptor(seg)
	util.Chkfatal(err)
	tags, err := desc.NewTags(1)
	util.Chkfatal(err)
	tag := tags.At(0)
	tag.SetId(ip.IpNetwork_TypeID)
	buf := &bytes.Buffer{}
	util.Chkfatal(capnp.NewPackedEncoder(buf).Encode(msg))
	return base64.StdEncoding.EncodeToString(buf.Bytes())
})

func restoreState(ctx context.Context, api grain.SandstormApi) (State, error) {
	return exn.Try(func(throw exn.Thrower) State {
		token, err := os.ReadFile(tokenPath)
		throw(err)
		network, err := restoreIpNetwork(ctx, token, api)
		throw(err)
		configBytes, err := os.ReadFile("/var/ipnetwork-proxy/config")
		throw(err)
		msg, err := capnp.Unmarshal(configBytes)
		throw(err)
		msg.ResetReadLimit(math.MaxUint64)
		cfg, err := ReadRootConfig(msg)
		throw(err)

		return State{
			token:   token,
			network: network,
			config:  cfg,
		}
	})
}

func restoreIpNetwork(ctx context.Context, token []byte, api grain.SandstormApi) (ip.IpNetwork, error) {
	fut, rel := api.Restore(ctx, func(p grain.SandstormApi_restore_Params) error {
		return p.SetToken(token)
	})
	defer rel()
	return ip.IpNetwork(fut.Cap().AddRef()), nil
}

type Server struct {
	api    grain.SandstormApi
	bridge bridgecp.SandstormHttpBridge
	state  mutex.Mutex[State]
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.RequestURI == "/_ipnetwork-proxy/powerbox-token" {
		s.handlePostToken(w, req)
		return
	} else if req.RequestURI == "/" {
		state := mutex.With1(&s.state, func(st *State) State {
			return *st
		})
		if !state.Ready() {
			tmpl.Execute(w, &state)
			return
		}
	}

	if req.Header.Get("Upgrade") == "websocket" {
		proxyWebSocket(w, req)
	} else {
		proxyNormalRequest(w, req)
	}
}

func (s *Server) handlePostToken(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	var payload struct{ Token string }
	err := json.NewDecoder(req.Body).Decode(&payload)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("reading json body: " + err.Error() + "\n"))
		return
	}
	sCtxFut, rel := s.bridge.GetSessionContext(ctx, func(p bridgecp.SandstormHttpBridge_getSessionContext_Params) error {
		return p.SetId(req.Header.Get("X-Sandstorm-Session-Id"))
	})
	defer rel()
	sCtx := sCtxFut.Context()
	claimFut, rel := sCtx.ClaimRequest(ctx, func(p grain.SessionContext_claimRequest_Params) error {
		return p.SetRequestToken(payload.Token)
	})
	defer rel()
	claimedCap := claimFut.Cap().AddRef()
	saveFut, rel := s.api.Save(ctx, func(p grain.SandstormApi_save_Params) error {
		return exn.Try0(func(throw exn.Thrower) {
			throw(p.SetCap(claimedCap.AddRef()))
			label, err := p.NewLabel()
			throw(err)
			throw(label.SetDefaultText("Network access for connecting to MPD"))
		})
	})
	defer rel()
	err = exn.Try0(func(throw exn.Thrower) {
		saveRes, err := saveFut.Struct()
		throw(err)
		token, err := saveRes.Token()
		throw(err)
		s.state.With(func(s *State) {
			s.token = token
			s.network = ip.IpNetwork(claimedCap)
		})
		// FIXME: do this write atomically:
		throw(os.WriteFile(tokenPath, token, 0600))
	})
	if err != nil {
		serverError(w, err)
		return
	}
}

func proxyNormalRequest(w http.ResponseWriter, req *http.Request) {
	req.URL.Scheme = "http"
	req.URL.Host = host
	resp, err := transport.RoundTrip(req)
	if err != nil {
		serverError(w, err)
		return
	}
	hdr := w.Header()
	for k, v := range resp.Header {
		hdr[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func proxyWebSocket(w http.ResponseWriter, req *http.Request) {
	_, clientRW, err := w.(http.Hijacker).Hijack()
	if err != nil {
		serverError(w, err)
		return
	}
	serverConn, err := net.Dial("tcp", host)
	if err != nil {
		serverError(w, err)
		return
	}
	req.Body = io.NopCloser(&bytes.Buffer{})
	if err = req.Write(serverConn); err != nil {
		println("error writing request to server: " + err.Error())
		return
	}
	go io.Copy(clientRW, serverConn)
	io.Copy(serverConn, clientRW)
}

func serverError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(err.Error() + "\n"))
}

func main() {
	ctx := context.Background()
	bridge, err := sandstormhttpbridge.Connect(ctx)
	util.Chkfatal(err)

	apiFut, rel := bridge.GetSandstormApi(ctx, nil)
	api := apiFut.Api().AddRef()
	go rel()

	srv := &Server{
		bridge: bridge,
		api:    api,
	}
	state, err := restoreState(ctx, api)
	if err == nil {
		srv.state = mutex.New(state)
	}

	http.Handle("/", srv)
	panic(http.ListenAndServe(":8001", nil))
}
