package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
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

func main() {

	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Upgrade") == "websocket" {
			proxyWebSocket(w, req)
		} else {
			proxyNormalRequest(w, req)
		}
	})
	panic(http.ListenAndServe(":8001", nil))
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
