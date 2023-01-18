package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strings"
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
	AllowedOrigins     []string
	SecureGRPC         bool
	Debug              bool
}

// Check that URL is valid.
func checkURL(data string, allowedSchemes ...string) error {
	url, err := url.Parse(data)
	if err != nil {
		return err
	}

	// Check scheme
	for _, scheme := range allowedSchemes {
		if url.Scheme == scheme {
			return nil
		}
	}
	return errors.New("invalid URL scheme")
}

// Parse command-line arguments.
// Returns a config struct with the parsed arguments.
func parseArguments() (config, error) {
	addr := flag.String("addr", "0.0.0.0:8080", "Address on which to listen to HTTP requests")
	credentialSecret := flag.String("hmac-secret", "test-secret", "The secret to use for HMAC")
	dbPath := flag.String("db-path", "db.sqlite3", "sqlite3 database path")
	authValidityWindow := flag.String("auth-valid-for", "360h", "The duration after which a credential should be considered invalid, eg, 360h for 15 days")
	proxyAPIAddr := flag.String("rescue-proxy-api-addr", "", "Address for the Rescue Proxy gRPC API")
	rocketscanAPIURL := flag.String("rocketscan-api-url", "", "URL for the Rocketscan REST API")
	allowedOrigins := flag.String("allowed-origins", "http://localhost:8080", "Comma-separated list of allowed CORS origins")
	secureGRPC := flag.Bool("secure-grpc", true, "Whether to enforce gRPC over TLS")
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

	if err := checkURL(*rocketscanAPIURL, "http", "https", ""); err != nil {
		return config{}, fmt.Errorf("invalid -rocketscan-api-url argument: %v", err)
	}

	// Check that CORS allowed origins are valid.
	origins := strings.Split(*allowedOrigins, ",")
	if *allowedOrigins != "*" {
		for _, origin := range origins {
			if err := checkURL(origin, []string{"http", "https"}); err != nil {
				return config{}, fmt.Errorf("invalid -allowed-origins argument: %v", err)
			}
		}
	}

	return config{
		ListenAddr:         *addr,
		CredentialSecret:   *credentialSecret,
		AuthValidityWindow: authValidityDuration,
		DBPath:             *dbPath,
		RescueProxyAPIAddr: *proxyAPIAddr,
		RocketscanAPIURL:   *rocketscanAPIURL,
		AllowedOrigins:     origins,
		SecureGRPC:         *secureGRPC,
		Debug:              *debug,
	}, nil
}
