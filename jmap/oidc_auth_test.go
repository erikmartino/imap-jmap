package jmap

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCAuthBackend_ValidateToken(t *testing.T) {
	// Generate RSA test key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}
	pubKey := &privateKey.PublicKey

	// Create mock JWKS server
	jwksHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nStr := base64.RawURLEncoding.EncodeToString(pubKey.N.Bytes())
		eBytes := bigIntToBytes(pubKey.E)
		eStr := base64.RawURLEncoding.EncodeToString(eBytes)

		jwks := map[string]any{
			"keys": []map[string]any{
				{
					"kid": "test-key-1",
					"kty": "RSA",
					"alg": "RS256",
					"use": "sig",
					"n":   nStr,
					"e":   eStr,
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})
	jwksServer := httptest.NewServer(jwksHandler)
	defer jwksServer.Close()

	issuer := "https://auth.example.com"
	oidcBackend, err := NewOIDCAuthBackend(OIDCConfig{
		Issuer:  issuer,
		JWKSURL: jwksServer.URL,
	})
	if err != nil {
		t.Fatalf("NewOIDCAuthBackend: %v", err)
	}

	// Create valid JWT
	header := jwtHeader{Alg: "RS256", Kid: "test-key-1", Typ: "JWT"}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	claims := jwtClaims{
		Iss:               issuer,
		Sub:               "user-123",
		PreferredUsername: "alice@example.com",
		Exp:               time.Now().Add(1 * time.Hour).Unix(),
	}
	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signedInput := headerB64 + "." + claimsB64
	hashed := sha256.Sum256([]byte(signedInput))
	sigBytes, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)

	validToken := signedInput + "." + sigB64

	// Test ValidateToken
	accountID, err := oidcBackend.ValidateToken(context.Background(), validToken)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	expectedAccountID := AccountIDForSubject("alice@example.com")
	if accountID != expectedAccountID {
		t.Errorf("Expected accountID %q, got %q", expectedAccountID, accountID)
	}
}

func bigIntToBytes(e int) []byte {
	if e == 0 {
		return []byte{0}
	}
	var res []byte
	for e > 0 {
		res = append([]byte{byte(e & 0xff)}, res...)
		e >>= 8
	}
	return res
}
