package jmap_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"imap-jmap/jmap"
	"imap-jmap/jmap/spectest"
)

var (
	oidSignedDataTest        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidDataTest              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSHA256Test            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidRSASHA256Test         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidMessageDigestAttrTest = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
)

type testSignerInfo struct {
	Version                   int
	IssuerAndSerialNumber     testIssuerAndSerial
	DigestAlgorithm           pkix.AlgorithmIdentifier
	AuthenticatedAttributes   asn1.RawValue `asn1:"optional,tag:0"`
	DigestEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedDigest           []byte
	UnauthenticatedAttributes asn1.RawValue `asn1:"optional,tag:1"`
}

type testIssuerAndSerial struct {
	IssuerName   asn1.RawValue
	SerialNumber asn1.RawValue
}

type testEncapsulatedContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type testSignedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	ContentInfo      testEncapsulatedContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      []testSignerInfo `asn1:"set"`
}

type testContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

// generateSMIMETestMessage builds a multipart/signed MIME email with real CMS SignedData.
func generateSMIMETestMessage(t *testing.T, from, to, subject, body string, notBefore, notAfter time.Time, tamperBody bool, malformedSig bool) ([]byte, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate RSA key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(123456789),
		Subject: pkix.Name{
			CommonName:   from,
			Organization: []string{"Example Org"},
		},
		EmailAddresses:        []string{from},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("Failed to parse created certificate: %v", err)
	}

	contentToSign := []byte(body)
	h := sha256.Sum256(contentToSign)

	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, h[:])
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	var rawIssuer asn1.RawValue
	_, err = asn1.Unmarshal(cert.RawIssuer, &rawIssuer)
	if err != nil {
		rawIssuer = asn1.RawValue{FullBytes: cert.RawIssuer}
	}

	si := testSignerInfo{
		Version: 1,
		IssuerAndSerialNumber: testIssuerAndSerial{
			IssuerName:   rawIssuer,
			SerialNumber: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagInteger, Bytes: cert.SerialNumber.Bytes()},
		},
		DigestAlgorithm:           pkix.AlgorithmIdentifier{Algorithm: oidSHA256Test},
		DigestEncryptionAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidRSASHA256Test},
		EncryptedDigest:           sig,
	}

	sd := testSignedData{
		Version:          1,
		DigestAlgorithms: []pkix.AlgorithmIdentifier{{Algorithm: oidSHA256Test}},
		ContentInfo:      testEncapsulatedContentInfo{ContentType: oidDataTest},
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, Bytes: certDER, IsCompound: true},
		SignerInfos:      []testSignerInfo{si},
	}

	sdBytes, err := asn1.Marshal(sd)
	if err != nil {
		t.Fatalf("Failed to marshal SignedData: %v", err)
	}

	ci := testContentInfo{
		ContentType: oidSignedDataTest,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, Bytes: sdBytes, IsCompound: true},
	}

	ciBytes, err := asn1.Marshal(ci)
	if err != nil {
		t.Fatalf("Failed to marshal ContentInfo: %v", err)
	}

	sigB64 := base64.StdEncoding.EncodeToString(ciBytes)
	if malformedSig {
		sigB64 = "!!corrupted_base64_and_not_asn1!!"
	}

	bodyDelivered := body
	if tamperBody {
		bodyDelivered = body + "\r\n[TAMPERED CONTENT INSERTED HERE]"
	}

	boundary := "----boundary_smime_test_12345"
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/signed; protocol=\"application/pkcs7-signature\"; micalg=sha-256; boundary=\"%s\"\r\n\r\n--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n--%s\r\nContent-Type: application/pkcs7-signature; name=\"smime.p7s\"\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=\"smime.p7s\"\r\n\r\n%s\r\n--%s--\r\n",
		from, to, subject, time.Now().Format(time.RFC1123Z), boundary, boundary, bodyDelivered, boundary, sigB64, boundary)

	return []byte(msg), cert, priv
}

// TestRFC9219_Vectors_ValidSignature tests an S/MIME signed email with valid certificate and signature.
func TestRFC9219_Vectors_ValidSignature(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.1", spectest.MUST,
		"Valid S/MIME signatures validate and return smimeStatus signed when certificate path is trusted.")

	now := time.Now()
	rawMsg, cert, _ := generateSMIMETestMessage(t, "signer@example.com", "recipient@example.com", "Valid S/MIME", "This is authentic text.", now.Add(-1*time.Hour), now.Add(24*time.Hour), false, false)

	// Build trust pool containing the certificate as a trusted root
	roots := x509.NewCertPool()
	roots.AddCert(cert)

	res := jmap.VerifySMIMEMessage(rawMsg, roots)
	if res == nil {
		t.Fatalf("Expected SMIMEVerification, got nil")
	}
	if res.Status != "signed" {
		t.Errorf("Expected status 'signed', got %q (errors: %v)", res.Status, res.Errors)
	}
	if res.VerifiedWith == nil || *res.VerifiedWith != "signer@example.com" {
		t.Errorf("Expected verifiedWith 'signer@example.com', got %v", res.VerifiedWith)
	}
	if len(res.Errors) != 0 {
		t.Errorf("Expected no errors for valid signature, got %v", res.Errors)
	}
}

// TestRFC9219_Vectors_UntrustedCertificateWarning tests that valid signatures from untrusted roots return signed/warning.
func TestRFC9219_Vectors_UntrustedCertificateWarning(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.1.1", spectest.MUST,
		"A signature that succeeded verification but uses an untrusted/self-signed certificate returns signed/warning.")

	now := time.Now()
	rawMsg, _, _ := generateSMIMETestMessage(t, "untrusted@example.com", "recipient@example.com", "Untrusted S/MIME", "Hello world.", now.Add(-1*time.Hour), now.Add(24*time.Hour), false, false)

	// Verify with empty root pool (untrusted certificate)
	emptyRoots := x509.NewCertPool()
	res := jmap.VerifySMIMEMessage(rawMsg, emptyRoots)
	if res == nil {
		t.Fatalf("Expected SMIMEVerification, got nil")
	}
	if res.Status != "signed/warning" {
		t.Errorf("Expected status 'signed/warning', got %q", res.Status)
	}
	hasUntrusted := false
	for _, e := range res.Errors {
		if e == "untrusted" {
			hasUntrusted = true
		}
	}
	if !hasUntrusted {
		t.Errorf("Expected 'untrusted' in errors, got %v", res.Errors)
	}
}

// TestRFC9219_Vectors_ExpiredCertificate tests that signatures with expired certificates return signed/failed.
func TestRFC9219_Vectors_ExpiredCertificate(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.1.1", spectest.MUST,
		"Signatures verified with expired certificates report signed/failed with expired error.")

	now := time.Now()
	// Certificate expired 2 days ago
	rawMsg, cert, _ := generateSMIMETestMessage(t, "expired@example.com", "recipient@example.com", "Expired S/MIME", "Expired message.", now.Add(-48*time.Hour), now.Add(-24*time.Hour), false, false)

	roots := x509.NewCertPool()
	roots.AddCert(cert)

	res := jmap.VerifySMIMEMessage(rawMsg, roots)
	if res == nil {
		t.Fatalf("Expected SMIMEVerification, got nil")
	}
	if res.Status != "signed/failed" {
		t.Errorf("Expected status 'signed/failed', got %q", res.Status)
	}
	hasExpired := false
	for _, e := range res.Errors {
		if e == "expired" {
			hasExpired = true
		}
	}
	if !hasExpired {
		t.Errorf("Expected 'expired' in errors, got %v", res.Errors)
	}
}

// TestRFC9219_Vectors_TamperedBody tests that messages modified after signing return signed/failed with tampered error.
func TestRFC9219_Vectors_TamperedBody(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.1.1", spectest.MUST,
		"Messages with tampered body or invalid digest return signed/failed with tampered error.")

	now := time.Now()
	rawMsg, cert, _ := generateSMIMETestMessage(t, "signer@example.com", "recipient@example.com", "Tampered S/MIME", "Original text.", now.Add(-1*time.Hour), now.Add(24*time.Hour), true, false)

	roots := x509.NewCertPool()
	roots.AddCert(cert)

	res := jmap.VerifySMIMEMessage(rawMsg, roots)
	if res == nil {
		t.Fatalf("Expected SMIMEVerification, got nil")
	}
	if res.Status != "signed/failed" {
		t.Errorf("Expected status 'signed/failed', got %q", res.Status)
	}
	hasTampered := false
	for _, e := range res.Errors {
		if e == "tampered" {
			hasTampered = true
		}
	}
	if !hasTampered {
		t.Errorf("Expected 'tampered' in errors, got %v", res.Errors)
	}
}

// TestRFC9219_Vectors_MalformedSignature tests that malformed signature parts return signed/failed.
func TestRFC9219_Vectors_MalformedSignature(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.1.1", spectest.MUST,
		"Messages with malformed CMS or corrupted signature parts return signed/failed with malformed error.")

	now := time.Now()
	rawMsg, cert, _ := generateSMIMETestMessage(t, "signer@example.com", "recipient@example.com", "Malformed S/MIME", "Test text.", now.Add(-1*time.Hour), now.Add(24*time.Hour), false, true)

	roots := x509.NewCertPool()
	roots.AddCert(cert)

	res := jmap.VerifySMIMEMessage(rawMsg, roots)
	if res == nil {
		t.Fatalf("Expected SMIMEVerification, got nil")
	}
	if res.Status != "signed/failed" {
		t.Errorf("Expected status 'signed/failed', got %q", res.Status)
	}
	hasMalformed := false
	for _, e := range res.Errors {
		if e == "malformed" {
			hasMalformed = true
		}
	}
	if !hasMalformed {
		t.Errorf("Expected 'malformed' in errors, got %v", res.Errors)
	}
}

// TestRFC9219_Vectors_UnsignedMessageReturnsNull tests that unsigned emails return null for smimeStatus.
func TestRFC9219_Vectors_UnsignedMessageReturnsNull(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.1", spectest.MUST,
		"A message without S/MIME signature returns null for smimeStatus.")

	plainMsg := []byte("From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Unsigned\r\n\r\nHello plain text.\r\n")
	res := jmap.VerifySMIMEMessage(plainMsg, nil)
	if res != nil {
		t.Errorf("Expected nil for unsigned email, got %+v", res)
	}
}

// TestRFC9219_Vectors_EmailQueryBySmimeStatus tests Email/query filtering by smimeStatus per RFC 9219 Section 4.2.
func TestRFC9219_Vectors_EmailQueryBySmimeStatus(t *testing.T) {
	spectest.Require(t, "RFC9219", "4.2", spectest.MUST,
		"Email/query matches emails by smimeStatus filter condition.")

	srv := newTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx := seedCtx()

	// Seed signed email
	signedStatus := "signed"
	_, err := srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs:  map[jmap.Id]bool{"mb-inbox": true},
		Subject:     "Signed Message For Query",
		SMIMEStatus: &signedStatus,
	})
	if err != nil {
		t.Fatalf("Failed to seed signed email: %v", err)
	}

	// Seed failed email
	failedStatus := "signed/failed"
	_, err = srv.MailBackend.CreateEmail(ctx, &jmap.Email{
		MailboxIDs:  map[jmap.Id]bool{"mb-inbox": true},
		Subject:     "Failed S/MIME For Query",
		SMIMEStatus: &failedStatus,
	})
	if err != nil {
		t.Fatalf("Failed to seed failed email: %v", err)
	}

	using := []string{jmap.CoreCapabilityURI, jmap.MailCapabilityURI, jmap.SmimeCapabilityURI}

	// Query for smimeStatus == "signed"
	res := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"smimeStatus": "signed",
			},
		}, "q1"},
	})

	if len(res.MethodResponses) == 0 || res.MethodResponses[0].Name != "Email/query" {
		t.Fatalf("Expected Email/query response, got %v", res.MethodResponses)
	}
	ids, _ := res.MethodResponses[0].Args["ids"].([]any)
	if len(ids) == 0 {
		t.Errorf("Expected matching signed email ID, got 0")
	}

	// Query for smimeStatus == "signed/failed"
	resFailed := postJMAP(t, ts.URL, using, []any{
		[]any{"Email/query", map[string]any{
			"accountId": "primary",
			"filter": map[string]any{
				"smimeStatus": "signed/failed",
			},
		}, "q2"},
	})
	idsFailed, _ := resFailed.MethodResponses[0].Args["ids"].([]any)
	if len(idsFailed) == 0 {
		t.Errorf("Expected matching failed email ID, got 0")
	}
}
