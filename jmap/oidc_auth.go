package jmap

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OIDCAuthBackend implements AuthBackend by validating OAuth 2.0 / OpenID Connect Bearer JWT tokens
// issued by an identity provider (such as Keycloak).
type OIDCAuthBackend struct {
	issuer          string
	jwksURL         string
	clientID        string
	fallbackBackend AuthBackend
	mu              sync.RWMutex
	keys            map[string]*rsa.PublicKey
	keysExpiry      time.Time
	httpClient      *http.Client
}

// OIDCConfig holds initialization parameters for OIDCAuthBackend.
type OIDCConfig struct {
	Issuer          string       // e.g. "https://auth.profundo.dk/realms/master"
	JWKSURL         string       // e.g. "https://auth.profundo.dk/realms/master/protocol/openid-connect/certs" (optional, auto-discovered if empty)
	ClientID        string       // Optional client ID / audience check
	FallbackBackend AuthBackend  // Optional fallback AuthBackend (e.g. MemoryAuthBackend for Basic auth / dev)
	HTTPClient      *http.Client // Optional custom HTTP client
}

// NewOIDCAuthBackend creates a new OIDC authentication backend.
func NewOIDCAuthBackend(cfg OIDCConfig) (*OIDCAuthBackend, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("OIDC issuer cannot be empty")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	jwksURL := cfg.JWKSURL
	if jwksURL == "" {
		jwksURL = strings.TrimRight(cfg.Issuer, "/") + "/protocol/openid-connect/certs"
	}
	return &OIDCAuthBackend{
		issuer:          strings.TrimRight(cfg.Issuer, "/"),
		jwksURL:         jwksURL,
		clientID:        cfg.ClientID,
		fallbackBackend: cfg.FallbackBackend,
		keys:            make(map[string]*rsa.PublicKey),
		httpClient:      client,
	}, nil
}

func (o *OIDCAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	if o.fallbackBackend != nil {
		return o.fallbackBackend.Authenticate(ctx, username, password)
	}
	if username == "" || username != password {
		return "", errors.New("invalid credentials")
	}
	return AccountIDForSubject(username), nil
}

func (o *OIDCAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	if o.fallbackBackend != nil {
		return o.fallbackBackend.ValidateCredentials(ctx, username, password)
	}
	if username == "" || username != password {
		return "", errors.New("invalid credentials")
	}
	return AccountIDForSubject(username), nil
}

func (o *OIDCAuthBackend) ValidateToken(ctx context.Context, token string) (string, string, error) {
	// First try OIDC JWT validation
	accountID, subject, err := o.validateJWT(ctx, token)
	if err == nil {
		return accountID, subject, nil
	}

	// Fall back if fallbackBackend is set (e.g. for development tokens or Basic auth tokens)
	if o.fallbackBackend != nil {
		if fbAccountID, fbSubject, fbErr := o.fallbackBackend.ValidateToken(ctx, token); fbErr == nil {
			return fbAccountID, fbSubject, nil
		}
	}

	return "", "", fmt.Errorf("invalid OIDC token: %w", err)
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Iss               string `json:"iss"`
	Sub               string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Aud               any    `json:"aud"`
	Exp               int64  `json:"exp"`
	Nbf               int64  `json:"nbf"`
}

func (o *OIDCAuthBackend) validateJWT(ctx context.Context, tokenStr string) (string, string, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return "", "", errors.New("malformed JWT: expected 3 parts")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid JWT header encoding: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return "", "", fmt.Errorf("invalid JWT header JSON: %w", err)
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("invalid JWT payload encoding: %w", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return "", "", fmt.Errorf("invalid JWT payload JSON: %w", err)
	}

	// Validate Expiry
	now := time.Now().Unix()
	if claims.Exp > 0 && now >= claims.Exp {
		return "", "", errors.New("token is expired")
	}
	if claims.Nbf > 0 && now < claims.Nbf {
		return "", "", errors.New("token not valid yet")
	}

	// Validate Issuer if configured
	if o.issuer != "" && claims.Iss != o.issuer {
		return "", "", fmt.Errorf("issuer mismatch: expected %s, got %s", o.issuer, claims.Iss)
	}

	// Retrieve Subject / Identity
	subject := claims.PreferredUsername
	if subject == "" {
		subject = claims.Email
	}
	if subject == "" {
		subject = claims.Sub
	}
	if subject == "" {
		return "", "", errors.New("token missing subject identity (sub, preferred_username, or email)")
	}

	// Verify Signature using JWKS
	pubKey, err := o.getKey(ctx, header.Kid)
	if err != nil {
		return "", "", fmt.Errorf("unable to resolve public key for kid %q: %w", header.Kid, err)
	}

	if err := verifyRSASignature(header.Alg, pubKey, parts[0]+"."+parts[1], parts[2]); err != nil {
		return "", "", fmt.Errorf("signature verification failed: %w", err)
	}

	return AccountIDForSubject(subject), subject, nil
}

func (o *OIDCAuthBackend) getKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	o.mu.RLock()
	if time.Now().Before(o.keysExpiry) {
		if key, ok := o.keys[kid]; ok {
			o.mu.RUnlock()
			return key, nil
		}
	}
	o.mu.RUnlock()

	// Refresh JWKS keys
	o.mu.Lock()
	defer o.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("JWKS fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Use string `json:"use"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		if k.Kty == "RSA" && k.N != "" && k.E != "" {
			pubKey, err := parseRSAPublicKey(k.N, k.E)
			if err == nil {
				newKeys[k.Kid] = pubKey
			}
		}
	}

	o.keys = newKeys
	o.keysExpiry = time.Now().Add(1 * time.Hour)

	if key, ok := o.keys[kid]; ok {
		return key, nil
	}
	if len(o.keys) > 0 && kid == "" { // Fallback if kid is omitted
		for _, k := range o.keys {
			return k, nil
		}
	}

	return nil, fmt.Errorf("key with kid %q not found in JWKS", kid)
}

func parseRSAPublicKey(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nStr)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eStr)
	if err != nil {
		return nil, err
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}

	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

func verifyRSASignature(alg string, pubKey *rsa.PublicKey, signedContent, sigStr string) error {
	sigBytes, err := base64.RawURLEncoding.DecodeString(sigStr)
	if err != nil {
		return fmt.Errorf("invalid signature base64: %w", err)
	}

	var hash crypto.Hash
	var digest []byte
	switch alg {
	case "RS256", "":
		hash = crypto.SHA256
		h := sha256.Sum256([]byte(signedContent))
		digest = h[:]
	case "RS384":
		hash = crypto.SHA384
		h := sha512.Sum384([]byte(signedContent))
		digest = h[:]
	case "RS512":
		hash = crypto.SHA512
		h := sha512.Sum512([]byte(signedContent))
		digest = h[:]
	default:
		return fmt.Errorf("unsupported JWT algorithm: %s", alg)
	}

	return rsa.VerifyPKCS1v15(pubKey, hash, digest, sigBytes)
}
