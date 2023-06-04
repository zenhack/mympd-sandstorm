package main

import (
	"context"
	"io"
	"net"
	"net/http"
)

func main() {
	host := "127.0.0.1:8000"

	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp", host)
		},
	}
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		req.URL.Scheme = "http"
		req.URL.Host = host
		resp, err := transport.RoundTrip(req)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error() + "\n"))
			return
		}
		hdr := w.Header()
		for k, v := range resp.Header {
			hdr[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
	panic(http.ListenAndServe(":8001", nil))
}
