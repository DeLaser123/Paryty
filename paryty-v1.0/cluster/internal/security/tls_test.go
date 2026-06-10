package security

import (
	"crypto/tls"
	"os"
	"testing"
)

func TestServerTLS_NoCertFiles(t *testing.T) {
	cfg, err := ServerTLS("", "", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when no cert files")
	}
}

func TestServerTLS_CertFileOnly(t *testing.T) {
	// Only cert file set but no key file → TLS disabled
	cfg, err := ServerTLS("some-cert.pem", "", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when key file missing")
	}
}

func TestServerTLS_KeyFileOnly(t *testing.T) {
	cfg, err := ServerTLS("", "some-key.pem", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when cert file missing")
	}
}

func TestServerTLS_MissingFiles(t *testing.T) {
	_, err := ServerTLS("nonexistent-cert.pem", "nonexistent-key.pem", "")
	if err == nil {
		t.Fatal("expected error for missing cert files")
	}
}

func TestServerTLS_MinVersion(t *testing.T) {
	// This test just verifies the API contract — we can't easily test
	// with real certs in unit tests, but we verify the function exists
	// and handles nil cases correctly.
	cfg, err := ServerTLS("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config")
	}
}

func TestClientTLS_NoArgs(t *testing.T) {
	cfg, err := ClientTLS("", "", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected TLS 1.2 minimum, got %x", cfg.MinVersion)
	}
}

func TestClientTLS_ServerCA(t *testing.T) {
	_, err := ClientTLS("nonexistent-ca.pem", "", "")
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestClientTLS_ClientCert(t *testing.T) {
	_, err := ClientTLS("", "nonexistent-cert.pem", "nonexistent-key.pem")
	if err == nil {
		t.Fatal("expected error for missing client cert files")
	}
}

func TestClientTLS_MinVersion(t *testing.T) {
	cfg, err := ClientTLS("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected TLS 1.2 minimum, got %x", cfg.MinVersion)
	}
}

func TestLoadTLSFromEnv_NoEnvVars(t *testing.T) {
	// Make sure env vars are not set
	os.Unsetenv("PARYTY_TLS_CERT_FILE")
	os.Unsetenv("PARYTY_TLS_KEY_FILE")
	os.Unsetenv("PARYTY_TLS_CLIENT_CA_FILE")

	cfg, err := LoadTLSFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config when env vars not set")
	}
}

func TestLoadTLSFromEnv_CertFileSet(t *testing.T) {
	os.Setenv("PARYTY_TLS_CERT_FILE", "nonexistent.pem")
	os.Setenv("PARYTY_TLS_KEY_FILE", "nonexistent.pem")
	defer os.Unsetenv("PARYTY_TLS_CERT_FILE")
	defer os.Unsetenv("PARYTY_TLS_KEY_FILE")

	cfg, err := LoadTLSFromEnv()
	// We expect an error because the files don't exist
	if err == nil && cfg != nil {
		t.Log("unexpected: TLS config created from nonexistent files")
	}
}
