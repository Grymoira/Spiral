package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Grymoira/Spiral/server/internal/api"
	"github.com/Grymoira/Spiral/server/internal/observability"
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

func logLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level: expected debug, info, warn or error")
	}
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8787", "listening address (public binds require TLS)")
	cert := flag.String("tls-cert", "", "PEM certificate for HTTPS/WSS")
	key := flag.String("tls-key", "", "PEM private key for HTTPS/WSS")
	level := flag.String("log-level", "info", "JSONL log threshold (debug, info, warn, error)")
	flag.Parse()
	parsedLevel, levelErr := logLevel(*level)
	if levelErr != nil {
		fmt.Fprintln(os.Stderr, levelErr)
		os.Exit(2)
	}
	logger := observability.NewLogger(os.Stdout, parsedLevel)
	slog.SetDefault(logger)
	if (*cert == "") != (*key == "") {
		logger.Error("server.configuration_rejected", "component", "server", "reason", "tls_pair_required")
		os.Exit(2)
	}
	var listener net.Listener
	var err error
	if *cert == "" {
		listener, err = listenLoopback(*listen)
	} else {
		listener, err = net.Listen("tcp", *listen)
	}
	if err != nil {
		logger.Error("server.listen_failed", "component", "server", "reason", "listen_failed", "error", err.Error())
		os.Exit(1)
	}
	defer listener.Close()
	handler := observability.HTTPLogs(api.NewHandler(rooms.NewStore()), logger)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8192,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	transport := "http_ws_loopback"
	if *cert != "" {
		transport = "https_wss"
	}
	logger.Info("server.started", "component", "server", "transport", transport, "listen_address", listener.Addr().String())
	if *cert != "" {
		err = server.ServeTLS(listener, *cert, *key)
	} else {
		err = server.Serve(listener)
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server.terminated", "component", "server", "reason", "serve_failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("server.stopped", "component", "server")
}
