package main

import (
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/rooms"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "loopback address for development HTTP server")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal(err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Fatal("refusing a non-loopback plaintext listener; deploy WSS/TLS before remote access")
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.NewHandler(rooms.NewStore()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8192,
	}
	log.Printf("Spiral development API listening on %s (loopback only)", *listen)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
