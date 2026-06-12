package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ServerTLS creates a tls.Config for a gRPC or HTTP server.
// If certFile or keyFile is empty, TLS is disabled (returns nil, nil).
// For production, mTLS can be enabled by setting clientCAFile to a non-empty path.
func ServerTLS(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, nil // TLS not configured
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		// Prefer modern, secure cipher suites.
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
	}

	// Enable mTLS if a client CA is provided.
	if clientCAFile != "" {
		caCert, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client CA: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse client CA certificate")
		}

		cfg.ClientAuth = tls.RequireAndVerifyClientCert
		cfg.ClientCAs = caCertPool
	}

	return cfg, nil
}

// ClientTLS creates a tls.Config for a gRPC or HTTP client.
// If serverCAFile is empty, the system's root CAs are used (standard TLS).
// For mTLS, provide both certFile and keyFile for the client certificate.
func ClientTLS(serverCAFile, certFile, keyFile string) (*tls.Config, error) {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load custom server CA if provided.
	if serverCAFile != "" {
		caCert, err := os.ReadFile(serverCAFile)
		if err != nil {
			return nil, fmt.Errorf("read server CA: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse server CA certificate")
		}
		cfg.RootCAs = caCertPool
	}

	// Load client certificate for mTLS if provided.
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}

// LoadTLSFromEnv reads TLS configuration from environment variables.
// Environment variables:
//
//	PARYTY_TLS_CERT_FILE     — Path to server certificate PEM
//	PARYTY_TLS_KEY_FILE      — Path to server private key PEM
//	PARYTY_TLS_CLIENT_CA_FILE — Path to client CA certificate for mTLS
//
// Returns nil config if no cert file is set.
func LoadTLSFromEnv() (*tls.Config, error) {
	certFile := os.Getenv("PARYTY_TLS_CERT_FILE")
	keyFile := os.Getenv("PARYTY_TLS_KEY_FILE")
	clientCAFile := os.Getenv("PARYTY_TLS_CLIENT_CA_FILE")

	if certFile == "" {
		return nil, nil
	}

	return ServerTLS(certFile, keyFile, clientCAFile)
}

// RequireMTLS returns true if PARYTY_TLS_CERT_FILE and PARYTY_TLS_CA_FILE are set.
// This is the enforcement gate — if true, all connections must use mTLS.
//
// In production, this should return true to ensure encrypted and authenticated
// communication between agents and the cluster.
func RequireMTLS() bool {
	return os.Getenv("PARYTY_TLS_CERT_FILE") != "" &&
		os.Getenv("PARYTY_TLS_CA_FILE") != ""
}
