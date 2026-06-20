package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequireMTLS_Enabled verifies that RequireMTLS returns true when both
// PARYTY_TLS_CERT_FILE and PARYTY_TLS_CLIENT_CA_FILE are set.
func TestRequireMTLS_Enabled(t *testing.T) {
	// Set environment variables
	os.Setenv("PARYTY_TLS_CERT_FILE", "/path/to/cert.pem")
	os.Setenv("PARYTY_TLS_CLIENT_CA_FILE", "/path/to/ca.pem")
	defer os.Unsetenv("PARYTY_TLS_CERT_FILE")
	defer os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")

	result := RequireMTLS()
	assert.True(t, result, "RequireMTLS should return true when both env vars are set")
}

// TestRequireMTLS_Disabled verifies that RequireMTLS returns false when
// environment variables are not set.
func TestRequireMTLS_Disabled(t *testing.T) {
	// Ensure environment variables are not set
	os.Unsetenv("PARYTY_TLS_CERT_FILE")
	os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")

	result := RequireMTLS()
	assert.False(t, result, "RequireMTLS should return false when env vars are not set")
}

// TestRequireMTLS_PartialConfig verifies that RequireMTLS returns false when
// only one of the required environment variables is set.
func TestRequireMTLS_PartialConfig(t *testing.T) {
	// Test with only cert file set
	os.Setenv("PARYTY_TLS_CERT_FILE", "/path/to/cert.pem")
	os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")
	defer os.Unsetenv("PARYTY_TLS_CERT_FILE")

	result := RequireMTLS()
	assert.False(t, result, "RequireMTLS should return false when only cert file is set")

	// Test with only CA file set
	os.Unsetenv("PARYTY_TLS_CERT_FILE")
	os.Setenv("PARYTY_TLS_CLIENT_CA_FILE", "/path/to/ca.pem")
	defer os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")

	result = RequireMTLS()
	assert.False(t, result, "RequireMTLS should return false when only CA file is set")
}

// TestServerTLS_ValidCerts verifies that ServerTLS creates a valid tls.Config
// when valid certificate files are provided.
func TestServerTLS_ValidCerts(t *testing.T) {
	// Create temporary test certificates
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "cert.pem")
	keyFile := filepath.Join(tempDir, "key.pem")
	caFile := filepath.Join(tempDir, "ca.pem")

	// Create test certificates
	err := createTestCertificates(certFile, keyFile, caFile)
	require.NoError(t, err, "Failed to create test certificates")

	// Test without client CA (no mTLS)
	cfg, err := ServerTLS(certFile, keyFile, "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, tls.VersionTLS13, int(cfg.MinVersion))
	assert.Nil(t, cfg.ClientCAs, "Client CAs should be nil when no client CA file is provided")

	// Test with client CA (mTLS enabled)
	cfg, err = ServerTLS(certFile, keyFile, caFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, tls.VersionTLS13, int(cfg.MinVersion))
	assert.NotNil(t, cfg.ClientCAs, "Client CAs should be set when client CA file is provided")
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
}

// TestServerTLS_MissingFiles verifies that ServerTLS returns an error when
// certificate files are missing.
func TestServerTLS_MissingFiles(t *testing.T) {
	// Test with non-existent cert file
	cfg, err := ServerTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "load server certificate")

	// Test with non-existent client CA file
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "cert.pem")
	keyFile := filepath.Join(tempDir, "key.pem")

	// Create test certificates
	err = createTestCertificates(certFile, keyFile, "")
	require.NoError(t, err)

	cfg, err = ServerTLS(certFile, keyFile, "/nonexistent/ca.pem")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "read client CA")
}

// TestServerTLS_EmptyPaths verifies that ServerTLS returns nil when cert/key
// paths are empty.
func TestServerTLS_EmptyPaths(t *testing.T) {
	cfg, err := ServerTLS("", "", "")
	assert.NoError(t, err)
	assert.Nil(t, cfg, "TLS config should be nil when cert/key paths are empty")

	cfg, err = ServerTLS("/path/to/cert.pem", "", "")
	assert.NoError(t, err)
	assert.Nil(t, cfg, "TLS config should be nil when key path is empty")

	cfg, err = ServerTLS("", "/path/to/key.pem", "")
	assert.NoError(t, err)
	assert.Nil(t, cfg, "TLS config should be nil when cert path is empty")
}

// TestClientTLS_ValidConfig verifies that ClientTLS creates a valid tls.Config.
func TestClientTLS_ValidConfig(t *testing.T) {
	// Create temporary test certificates
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "cert.pem")
	keyFile := filepath.Join(tempDir, "key.pem")
	caFile := filepath.Join(tempDir, "ca.pem")

	// Create test certificates
	err := createTestCertificates(certFile, keyFile, caFile)
	require.NoError(t, err, "Failed to create test certificates")

	// Test with custom server CA
	cfg, err := ClientTLS(caFile, "", "")
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, tls.VersionTLS13, int(cfg.MinVersion))
	assert.NotNil(t, cfg.RootCAs, "Root CAs should be set when server CA file is provided")
	assert.Nil(t, cfg.Certificates, "Client certificates should be nil when not provided")

	// Test with client certificate (mTLS)
	cfg, err = ClientTLS(caFile, certFile, keyFile)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs, "Root CAs should be set")
	assert.Len(t, cfg.Certificates, 1, "Client certificate should be set")
}

// TestClientTLS_MissingFiles verifies that ClientTLS returns an error when
// certificate files are missing.
func TestClientTLS_MissingFiles(t *testing.T) {
	// Test with non-existent server CA file
	cfg, err := ClientTLS("/nonexistent/ca.pem", "", "")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "read server CA")

	// Test with non-existent client certificate
	tempDir := t.TempDir()
	caFile := filepath.Join(tempDir, "ca.pem")

	// Create test certificates
	err = createTestCertificates("", "", caFile)
	require.NoError(t, err)

	cfg, err = ClientTLS(caFile, "/nonexistent/cert.pem", "/nonexistent/key.pem")
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "load client certificate")
}

// TestLoadTLSFromEnv verifies that LoadTLSFromEnv reads TLS configuration
// from environment variables.
func TestLoadTLSFromEnv(t *testing.T) {
	// Test with no environment variables set
	os.Unsetenv("PARYTY_TLS_CERT_FILE")
	os.Unsetenv("PARYTY_TLS_KEY_FILE")
	os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")

	cfg, err := LoadTLSFromEnv()
	assert.NoError(t, err)
	assert.Nil(t, cfg, "TLS config should be nil when no env vars are set")

	// Test with environment variables set
	// Note: This would require actual certificate files, so we'll just test
	// that the function doesn't panic
	os.Setenv("PARYTY_TLS_CERT_FILE", "/path/to/cert.pem")
	os.Setenv("PARYTY_TLS_KEY_FILE", "/path/to/key.pem")
	os.Setenv("PARYTY_TLS_CLIENT_CA_FILE", "/path/to/ca.pem")
	defer os.Unsetenv("PARYTY_TLS_CERT_FILE")
	defer os.Unsetenv("PARYTY_TLS_KEY_FILE")
	defer os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")

	// This will fail because files don't exist, but we're testing the function
	// doesn't panic and handles errors correctly
	_, err = LoadTLSFromEnv()
	// We expect an error because the files don't exist
	assert.Error(t, err)
}

// createTestCertificates creates real self-signed test certificates.
func createTestCertificates(certFile, keyFile, caFile string) error {
	// Generate CA certificate if caFile is requested.
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Paryty Test CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	if caFile != "" {
		caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCertDER})
		if err := os.WriteFile(caFile, caPEM, 0644); err != nil {
			return err
		}
	}

	if certFile == "" && keyFile == "" {
		return nil
	}

	// Generate server certificate signed by the CA.
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "Paryty Test Server"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return err
	}
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	if certFile != "" {
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCertDER})
		if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
			return err
		}
	}
	if keyFile != "" {
		keyDER, err := x509.MarshalECPrivateKey(serverKey)
		if err != nil {
			return err
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
		if err := os.WriteFile(keyFile, keyPEM, 0644); err != nil {
			return err
		}
	}

	return nil
}
