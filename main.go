package main

import (
	"bytes"
	"context"
	"io"
	"math"
	"net"
	"net/http"
	"os"

	capnp "capnproto.org/go/capnp/v3"
	"zenhack.net/go/tempest/capnp/grain"
	"zenhack.net/go/tempest/capnp/ip"
	bridgecp "zenhack.net/go/tempest/capnp/sandstormhttpbridge"
	"zenhack.net/go/tempest/exp/sandstormhttpbridge"
	"zenhack.net/go/util"
	"zenhack.net/go/util/exn"
	"zenhack.net/go/util/maybe"
	"zenhack.net/go/util/sync/mutex"
)

const host = "127.0.0.1:8000"

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

func restoreState(ctx context.Context, bridge bridgecp.SandstormHttpBridge) (State, error) {
	return exn.Try(func(throw exn.Thrower) State {
		token, err := os.ReadFile("/var/ipnetwork-proxy/token")
		throw(err)
		network, err := restoreIpNetwork(ctx, token, bridge)
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
		}
	})
}

func restoreIpNetwork(ctx context.Context, token []byte, bridge bridgecp.SandstormHttpBridge) (ip.IpNetwork, error) {
	apiFut, rel := bridge.GetSandstormApi(ctx, nil)
	defer rel()
	restoreFut, rel := apiFut.Api().Restore(ctx, func(p grain.SandstormApi_restore_Params) error {
		return p.SetToken(token)
	})
	defer rel()
	return ip.IpNetwork(restureFut.Cap().AddRef()), nil
}

type Server struct {
	state *mutex.Mutex[maybe.Maybe[State]]
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.RequestURI == "" {
		var done bool
		state.With(func(ms *maybe.Maybe[State]) {
			_, ok := ms.Get()
			if !ok {
			}
		})
	}

	if req.Header.Get("Upgrade") == "websocket" {
		proxyWebSocket(w, req)
	} else {
		proxyNormalRequest(w, req)
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
	bridge, err := sandstormhttpbridge.Connect(context.Background())
	util.Chkfatal(err)

	srv := &Server{
		state: mutex.New(maybe.Maybe[State]{}),
	}
	http.Handle("/", srv)
	panic(http.ListenAndServe(":8001", nil))
}
