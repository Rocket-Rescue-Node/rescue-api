package main

import (
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Application configuration.
type config struct {
	ListenAddr           string
	MetricsAddr          string
	CredentialSecret     []byte
	DBPath               string
	RescueProxyAPIAddr   string
	RocketscanAPIURL     string
	AllowedOrigins       []string
	SecureGRPC           bool
	Debug                bool
	EnableSoloValidators bool
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
	metricsAddr := flag.String("metrics-addr", "0.0.0.0:9000", "Address on which to listen for /metrics requests")
	credentialSecret := flag.String("hmac-secret", "",
		`The secret to use for HMAC.
Value must be at least 32 bytes of entropy, base64-encoded.
Use 'dd if=/dev/urandom bs=4 count=8 | base64' if you need to generate a new secret.`,
	)
	dbPath := flag.String("db-path", "db.sqlite3", "sqlite3 database path")
	proxyAPIAddr := flag.String("rescue-proxy-api-addr", "", "Address for the Rescue Proxy gRPC API")
	rocketscanAPIURL := flag.String("rocketscan-api-url", "", "URL for the Rocketscan REST API")
	allowedOrigins := flag.String("allowed-origins", "http://localhost:8080", "Comma-separated list of allowed CORS origins")
	secureGRPC := flag.Bool("secure-grpc", true, "Whether to use gRPC over TLS")
	debug := flag.Bool("debug", false, "Whether to enable verbose logging")
	enableSoloValidators := flag.Bool("enable-solo-validators", true, "Whether or not to enable solo validator credentials")
	flag.Parse()

	if *credentialSecret == "" {
		return config{}, errors.New("missing -hmac-secret, at least one must be provided")
	}
	secret, err := base64.StdEncoding.DecodeString(*credentialSecret)
	if err != nil {
		return config{}, errors.New("invalid -hmac-secret, please see the usage output for how to create a valid secret")
	}
	if len(secret) < 32 {
		return config{}, errors.New("base64 decoded secret with length %d is shorter than the required 32 bytes")
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
			if err := checkURL(origin, "http", "https"); err != nil {
				return config{}, fmt.Errorf("invalid -allowed-origins argument: %v", err)
			}
		}
	}

	return config{
		ListenAddr:           *addr,
		MetricsAddr:          *metricsAddr,
		CredentialSecret:     secret,
		DBPath:               *dbPath,
		RescueProxyAPIAddr:   *proxyAPIAddr,
		RocketscanAPIURL:     *rocketscanAPIURL,
		AllowedOrigins:       origins,
		SecureGRPC:           *secureGRPC,
		Debug:                *debug,
		EnableSoloValidators: *enableSoloValidators,
	}, nil
}
