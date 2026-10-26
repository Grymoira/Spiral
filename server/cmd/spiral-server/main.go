package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

// listenLoopback checks both the requested hostname and the bound TCP address.
// A hostname such as localhost must not be allowed to resolve to a public IP.
func listenLoopback(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("refusing a non-loopback plaintext listener")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	bound, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !bound.IP.IsLoopback() {
		_ = listener.Close()
		return nil, fmt.Errorf("refusing non-loopback bound address: %s", address)
	}
	return listener, nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "listening address (public binds require TLS)")
	cert := flag.String("tls-cert", "", "PEM certificate for HTTPS/WSS")
	key := flag.String("tls-key", "", "PEM private key for HTTPS/WSS")
	flag.Parse()
	if (*cert == "") != (*key == "") {
		log.Fatal("both tls-cert and tls-key must be provided")
	}
	var listener net.Listener
	var err error
	if *cert == "" {
		listener, err = listenLoopback(*listen)
	} else {
		listener, err = net.Listen("tcp", *listen)
	}
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           api.NewHandler(rooms.NewStore()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8192,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	if *cert == "" {
		log.Printf("Spiral local HTTP/WS listening on %s (loopback only)", listener.Addr())
	} else {
		log.Printf("Spiral HTTPS/WSS listening on %s (experimental)", listener.Addr())
	}
	if *cert != "" {
		err = server.ServeTLS(listener, *cert, *key)
	} else {
		err = server.Serve(listener)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
