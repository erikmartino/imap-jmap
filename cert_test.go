package main

import (
	"bytes"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestGetOrGenerateCertificate_ReuseExplicitPaths(t *testing.T) {
	tempDir := t.TempDir()
	certFile := filepath.Join(tempDir, "test_cert.pem")
	keyFile := filepath.Join(tempDir, "test_key.pem")

	// First call: files do not exist yet -> should generate and save them
	cert1, err := getOrGenerateCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("first getOrGenerateCertificate failed: %v", err)
	}

	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		t.Fatalf("expected cert file %s to be created", certFile)
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		t.Fatalf("expected key file %s to be created", keyFile)
	}

	// Second call: files exist -> should load and reuse the exact same certificate
	cert2, err := getOrGenerateCertificate(certFile, keyFile)
	if err != nil {
		t.Fatalf("second getOrGenerateCertificate failed: %v", err)
	}

	if !bytes.Equal(cert1.Certificate[0], cert2.Certificate[0]) {
		t.Fatalf("expected certificate DER bytes to match exactly across calls (reuse)")
	}

	parsed1, err := x509.ParseCertificate(cert1.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse cert1: %v", err)
	}
	parsed2, err := x509.ParseCertificate(cert2.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse cert2: %v", err)
	}

	if parsed1.SerialNumber.Cmp(parsed2.SerialNumber) != 0 {
		t.Fatalf("expected serial numbers to match: %v vs %v", parsed1.SerialNumber, parsed2.SerialNumber)
	}
}

func TestGetOrGenerateCertificate_DefaultReuse(t *testing.T) {
	// With empty paths, it should resolve from default locations and reuse
	cert1, err := getOrGenerateCertificate("", "")
	if err != nil {
		t.Fatalf("first getOrGenerateCertificate failed: %v", err)
	}

	cert2, err := getOrGenerateCertificate("", "")
	if err != nil {
		t.Fatalf("second getOrGenerateCertificate failed: %v", err)
	}

	if !bytes.Equal(cert1.Certificate[0], cert2.Certificate[0]) {
		t.Fatalf("expected default certificate DER bytes to match on reuse")
	}
}
