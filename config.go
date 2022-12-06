package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"time"
)

// Application configuration.
type config struct {
	ListenAddr         string
	CredentialSecret   string
	AuthValidityWindow time.Duration
	DBPath             string
	RescueProxyAPIAddr string
	RocketscanAPIURL   string
	Debug              bool
}

// Parse command-line arguments.
// Returns a config struct with the parsed arguments.
func parseArguments() (config, error) {
	addr := flag.String("addr", "0.0.0.0:8080", "Address on which to listen to HTTP requests")
	credentialSecret := flag.String("hmac-secret", "test-secret", "The secret to use for HMAC")
	dbPath := flag.String("db-path", "db.sqlite3", "sqlite3 database path")
	authValidityWindow := flag.String("auth-valid-for", "360h", "The duration after which a credential should be considered invalid, eg, 360h for 15 days")
	proxyAPIAddr := flag.String("rescue-proxy-api-addr", "127.0.0.1:8000", "Address for the Rescue Proxy gRPC API")
	rocketscanAPIURL := flag.String("rocketscan-api-url", "http://127.0.0.1", "URL for the Rocketscan REST API")
	debug := flag.Bool("debug", false, "Whether to enable verbose logging")
	flag.Parse()

	if *credentialSecret == "" {
		return config{}, errors.New("invalid -hmac-secret argument")
	}

	authValidityDuration, err := time.ParseDuration(*authValidityWindow)
	if err != nil {
		return config{}, fmt.Errorf("invalid -auth-valid-for argument: %v", err)
	}

	if _, _, err := net.SplitHostPort(*proxyAPIAddr); err != nil {
		return config{}, fmt.Errorf("invalid -rescue-proxy-api-addr argument: %v", err)
	}

	if url, err := url.Parse(*rocketscanAPIURL); err != nil {
		return config{}, fmt.Errorf("invalid -rocketscan-api-url argument: %v", err)
	} else if url.Scheme != "" && url.Scheme != "http" && url.Scheme != "https" {
		return config{}, fmt.Errorf("invalid -rocketscan-api-url argument: invalid scheme '%s'", url.Scheme)
	}

	return config{
		ListenAddr:         *addr,
		CredentialSecret:   *credentialSecret,
		AuthValidityWindow: authValidityDuration,
		DBPath:             *dbPath,
		RescueProxyAPIAddr: *proxyAPIAddr,
		RocketscanAPIURL:   *rocketscanAPIURL,
		Debug:              *debug,
	}, nil
}
