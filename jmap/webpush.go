package jmap

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

// decodeBase64URL decodes base64url data with or without padding.
func decodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	// Remove any internal newlines or spaces
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\t", "")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// encodeBase64URL encodes data into unpadded base64url format.
func encodeBase64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// EncryptWebPushPayload encrypts a plaintext message for a Web Push recipient per RFC 8291 (aes128gcm).
func EncryptWebPushPayload(plaintext []byte, receiverP256dhBase64, receiverAuthBase64 string) ([]byte, error) {
	receiverPubBytes, err := decodeBase64URL(receiverP256dhBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver p256dh key: %w", err)
	}
	receiverAuthBytes, err := decodeBase64URL(receiverAuthBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver auth secret: %w", err)
	}
	if len(receiverAuthBytes) < 16 {
		return nil, fmt.Errorf("receiver auth secret must be at least 16 bytes")
	}

	receiverPub, err := ecdh.P256().NewPublicKey(receiverPubBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse receiver public key: %w", err)
	}

	senderPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral sender key: %w", err)
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	return encryptWebPushWithKeys(plaintext, receiverPub, receiverAuthBytes, senderPriv, salt, 4096)
}

// encryptWebPushWithKeys is the core RFC 8291 encryption routine admitting deterministic keys for testing.
func encryptWebPushWithKeys(plaintext []byte, receiverPub *ecdh.PublicKey, authSecret []byte, senderPriv *ecdh.PrivateKey, salt []byte, rs uint32) ([]byte, error) {
	senderPubBytes := senderPriv.PublicKey().Bytes()
	receiverPubBytes := receiverPub.Bytes()

	// 1. ECDH shared secret
	ecdhSecret, err := senderPriv.ECDH(receiverPub)
	if err != nil {
		return nil, fmt.Errorf("ecdh failed: %w", err)
	}

	// 2. Derive IKM per RFC 8291 §3.2
	// info = "WebPush: info\x00" || receiver_pub || sender_pub
	keyInfo := append([]byte("WebPush: info\x00"), receiverPubBytes...)
	keyInfo = append(keyInfo, senderPubBytes...)

	prkKey := hkdf.Extract(sha256.New, ecdhSecret, authSecret)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prkKey, keyInfo), ikm); err != nil {
		return nil, fmt.Errorf("hkdf expand ikm: %w", err)
	}

	// 3. Derive CEK and Nonce per RFC 8291 §3.3 & RFC 8188 §2.2
	prk := hkdf.Extract(sha256.New, ikm, salt)

	cekInfo := []byte("Content-Encoding: aes128gcm\x00")
	cek := make([]byte, 16)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, cekInfo), cek); err != nil {
		return nil, fmt.Errorf("hkdf expand cek: %w", err)
	}

	nonceInfo := []byte("Content-Encoding: nonce\x00")
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, nonceInfo), nonce); err != nil {
		return nil, fmt.Errorf("hkdf expand nonce: %w", err)
	}

	// 4. Record to encrypt: plaintext + delimiter 0x02 (RFC 8188 §2)
	record := make([]byte, len(plaintext)+1)
	copy(record, plaintext)
	record[len(plaintext)] = 0x02

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher gcm: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, record, nil)

	// 5. Build RFC 8188 header (86 bytes)
	header := make([]byte, 86)
	copy(header[0:16], salt)
	binary.BigEndian.PutUint32(header[16:20], rs)
	header[20] = byte(len(senderPubBytes)) // 65
	copy(header[21:86], senderPubBytes)

	return append(header, ciphertext...), nil
}

// DecryptWebPushPayload decrypts an RFC 8291 message given the receiver's private key and auth secret.
func DecryptWebPushPayload(encrypted []byte, receiverPriv *ecdh.PrivateKey, authSecret []byte) ([]byte, error) {
	if len(encrypted) < 86+16 {
		return nil, fmt.Errorf("encrypted payload too short")
	}

	salt := encrypted[0:16]
	// rs := binary.BigEndian.Uint32(encrypted[16:20])
	idLen := int(encrypted[20])
	if idLen != 65 {
		return nil, fmt.Errorf("unsupported key id length: %d", idLen)
	}
	senderPubBytes := encrypted[21 : 21+idLen]
	ciphertext := encrypted[21+idLen:]

	senderPub, err := ecdh.P256().NewPublicKey(senderPubBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sender public key: %w", err)
	}

	receiverPubBytes := receiverPriv.PublicKey().Bytes()

	// 1. ECDH shared secret
	ecdhSecret, err := receiverPriv.ECDH(senderPub)
	if err != nil {
		return nil, fmt.Errorf("ecdh failed: %w", err)
	}

	// 2. IKM
	keyInfo := append([]byte("WebPush: info\x00"), receiverPubBytes...)
	keyInfo = append(keyInfo, senderPubBytes...)

	prkKey := hkdf.Extract(sha256.New, ecdhSecret, authSecret)
	ikm := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prkKey, keyInfo), ikm); err != nil {
		return nil, fmt.Errorf("hkdf expand ikm: %w", err)
	}

	// 3. CEK & Nonce
	prk := hkdf.Extract(sha256.New, ikm, salt)

	cekInfo := []byte("Content-Encoding: aes128gcm\x00")
	cek := make([]byte, 16)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, cekInfo), cek); err != nil {
		return nil, fmt.Errorf("hkdf expand cek: %w", err)
	}

	nonceInfo := []byte("Content-Encoding: nonce\x00")
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, nonceInfo), nonce); err != nil {
		return nil, fmt.Errorf("hkdf expand nonce: %w", err)
	}

	// 4. Decrypt
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher gcm: %w", err)
	}
	record, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("gcm decrypt failed: %w", err)
	}

	// 5. Trim trailing delimiter (0x02)
	if len(record) == 0 || record[len(record)-1] != 0x02 {
		return nil, fmt.Errorf("missing record delimiter")
	}
	return record[:len(record)-1], nil
}

// GenerateVAPIDToken generates a signed VAPID authorization token per RFC 8292 / RFC 9749.
func GenerateVAPIDToken(targetURL string, vapidKey *ecdsa.PrivateKey, subject string) (authHeader string, err error) {
	if vapidKey == nil {
		return "", nil
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return "", err
	}
	audience := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// Header: {"alg":"ES256","typ":"JWT"}
	hdrJSON := `{"alg":"ES256","typ":"JWT"}`
	hdrPart := encodeBase64URL([]byte(hdrJSON))

	// Claims: {"aud":"...","exp":...,"sub":"..."}
	exp := time.Now().Add(12 * time.Hour).Unix()
	claims := map[string]any{
		"aud": audience,
		"exp": exp,
	}
	if subject != "" {
		claims["sub"] = subject
	}
	claimsJSON, _ := json.Marshal(claims)
	claimsPart := encodeBase64URL(claimsJSON)

	signingInput := hdrPart + "." + claimsPart
	hashed := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, vapidKey, hashed[:])
	if err != nil {
		return "", err
	}

	rBytes := r.Bytes()
	sBytes := s.Bytes()
	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	token := signingInput + "." + encodeBase64URL(sigBytes)

	pubBytes := elliptic.Marshal(elliptic.P256(), vapidKey.X, vapidKey.Y)
	kPart := encodeBase64URL(pubBytes)

	return fmt.Sprintf("vapid t=%s, k=%s", token, kPart), nil
}

// DispatchWebPushStateChange sends a StateChange event to a PushSubscription per RFC 8620 §7.2, RFC 8030, RFC 8291, and RFC 9749.
func DispatchWebPushStateChange(ctx context.Context, sub *PushSubscription, accountID, typeName, newState string, vapidKey *ecdsa.PrivateKey, vapidSubject string) error {
	if sub == nil || sub.URL == "" {
		return nil
	}

	// Check expiration
	if sub.Expires != nil && *sub.Expires != "" {
		if exp, err := time.Parse(time.RFC3339, *sub.Expires); err == nil {
			if time.Now().UTC().After(exp) {
				return nil
			}
		}
	}

	// Check type filter
	if len(sub.Types) > 0 {
		matched := false
		for _, t := range sub.Types {
			if strings.EqualFold(t, typeName) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
	}

	// Construct StateChange payload
	stateChange := &StateChange{
		Type: "StateChange",
		Changed: map[string]map[string]string{
			accountID: {
				typeName: newState,
			},
		},
	}
	payloadBytes, err := json.Marshal(stateChange)
	if err != nil {
		return err
	}

	var reqBody []byte
	var contentEncoding string
	var contentType string

	if sub.Keys != nil && sub.Keys.P256dh != "" && sub.Keys.Auth != "" {
		encrypted, encErr := EncryptWebPushPayload(payloadBytes, sub.Keys.P256dh, sub.Keys.Auth)
		if encErr != nil {
			log.Printf("WebPush: encryption error for sub %s: %v", sub.ID, encErr)
			return encErr
		}
		reqBody = encrypted
		contentType = "application/octet-stream"
		contentEncoding = "aes128gcm"
	} else {
		reqBody = payloadBytes
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", sub.URL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", contentType)
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Topic", "jmap")

	if vapidKey != nil {
		if authHdr, err := GenerateVAPIDToken(sub.URL, vapidKey, vapidSubject); err == nil && authHdr != "" {
			req.Header.Set("Authorization", authHdr)
		}
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return ErrSubscriptionGone
	}
	return nil
}

// ErrSubscriptionGone indicates the push endpoint returned 404 or 410 gone (RFC 8030 §6.5).
var ErrSubscriptionGone = fmt.Errorf("push subscription gone")
