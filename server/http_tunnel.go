package server

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

/*
 * The purpose of this feature is  to allow tunneling of arbitrary HTTP(S) connections.
 * One use case for this is to allow tunneling of all Pelion edge traffic over an authenticated
 * proxy, so that we can function behind restrictive firewalls that only allow HTTP(S)
 * traffic to pass through them.  As opposed to adding tuneling code to each service, it
 * would be easier to have edge-proxy handle tunneling configuration in one place.
 */

func handleTunneling(w http.ResponseWriter, r *http.Request) {
	log.Printf("HTTP CONNECT: opening connection to %s\n", r.Host)
	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		log.Printf("HTTP CONNECT: failed to open connection to %s\n", r.Host)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	go transfer(destConn, clientConn)
	go transfer(clientConn, destConn)
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer source.Close()
	// Copy until EOF, or Error
	io.Copy(destination, source)
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		log.Printf("HTTP proxy: failed to round trip to %s\n", req.URL)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// StartHTTPTunnel starts a server that accepts to the HTTP CONNECT method to proxy arbitrary TCP connections.
// It can be used to tunnel HTTPS connections.
func StartHTTPTunnel(addr string) {
	log.Printf("Starting HTTPS proxy on %s\n", addr)
	server := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				handleTunneling(w, r)
			} else {
				handleHTTP(w, r)
			}
		}),
		// Disable HTTP/2.  HTTP/2 doesn't support hijacking.  https://github.com/golang/go/issues/14797#issuecomment-196103814
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	log.Fatal(server.ListenAndServe())
}