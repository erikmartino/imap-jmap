package jmap

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
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
// issued by an identity provider (such as Keycloak or mock-ldap).
type OIDCAuthBackend struct {
	issuer          string
	jwksURL         string
	clientID        string
	encSecretKey    []byte
	fallbackBackend AuthBackend
	mu              sync.RWMutex
	keys            map[string]*rsa.PublicKey
	keysExpiry      time.Time
	httpClient      *http.Client
}

var _ AuthBackend = (*OIDCAuthBackend)(nil)
var _ TokenCredentialsExtractor = (*OIDCAuthBackend)(nil)

// OIDCConfig holds initialization parameters for OIDCAuthBackend.
type OIDCConfig struct {
	Issuer          string       // e.g. "https://auth.profundo.dk/realms/master"
	JWKSURL         string       // e.g. "https://auth.profundo.dk/realms/master/protocol/openid-connect/certs" (optional, auto-discovered if empty)
	ClientID        string       // Optional client ID / audience check
	SecretKey       string       // Symmetric key for decrypting enc_sec credentials payload (optional)
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
		jwksURL = strings.TrimRight(cfg.Issuer, "/") + "/oauth/keys"
	}
	var encKey []byte
	if cfg.SecretKey != "" {
		h := sha256.Sum256([]byte(cfg.SecretKey))
		encKey = h[:]
	}
	return &OIDCAuthBackend{
		issuer:          strings.TrimRight(cfg.Issuer, "/"),
		jwksURL:         jwksURL,
		clientID:        cfg.ClientID,
		encSecretKey:    encKey,
		fallbackBackend: cfg.FallbackBackend,
		keys:            make(map[string]*rsa.PublicKey),
		httpClient:      client,
	}, nil
}

// Authenticate validates username/password credentials. The OIDC backend
// itself never accepts passwords: it authenticates via JWT tokens issued by the
// identity provider. Credentials are only honored when a fallback backend
// (e.g. MemoryAuthBackend for development) is explicitly configured; without
// one, every credential attempt fails closed rather than accepting the
// development "username == password" convention in a production OIDC
// deployment (AUTH-2).
func (o *OIDCAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	if o.fallbackBackend != nil {
		return o.fallbackBackend.Authenticate(ctx, username, password)
	}
	return "", errors.New("credentials are not accepted when no credential backend is configured; use an OIDC access token")
}

// ValidateCredentials verifies username/password credentials (HTTP Basic
// authentication). Like Authenticate, it fails closed when no credential
// backend is configured: plain "username == password" matches MUST NOT be
// accepted in production (AUTH-2).
func (o *OIDCAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	if o.fallbackBackend != nil {
		return o.fallbackBackend.ValidateCredentials(ctx, username, password)
	}
	return "", errors.New("credentials are not accepted when no credential backend is configured; use an OIDC access token")
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

// ExtractCredentials decrypts upstream username and password from the OIDC token's enc_sec claim,
// or delegates to the fallback backend if configured.
func (o *OIDCAuthBackend) ExtractCredentials(ctx context.Context, token string) (string, string, bool) {
	if extractor, ok := o.fallbackBackend.(TokenCredentialsExtractor); ok {
		if u, p, okExt := extractor.ExtractCredentials(ctx, token); okExt {
			return u, p, true
		}
	}

	if len(o.encSecretKey) == 0 {
		return "", "", false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", false
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}

	var claims struct {
		EncSec string `json:"enc_sec"`
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil || claims.EncSec == "" {
		return "", "", false
	}

	return DecryptCredentialsPayload(claims.EncSec, o.encSecretKey)
}

// DecryptCredentialsPayload decrypts an AES-GCM base64 encoded credential string.
func DecryptCredentialsPayload(cipherBase64 string, secretKey []byte) (string, string, bool) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(cipherBase64)
	if err != nil {
		return "", "", false
	}

	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", "", false
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", false
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", "", false
	}

	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", "", false
	}

	var payload struct {
		Username  string `json:"u"`
		Password  string `json:"p"`
		ExpiresAt int64  `json:"exp"`
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return "", "", false
	}

	if payload.ExpiresAt > 0 && time.Now().Unix() > payload.ExpiresAt {
		return "", "", false
	}

	return payload.Username, payload.Password, true
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
