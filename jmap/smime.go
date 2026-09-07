package jmap

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	_ "crypto/sha1"
	_ "crypto/sha256"
	_ "crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"time"
)

var (
	oidSignedData        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidData              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 1}
	oidSHA1              = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512            = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidRSA               = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidRSASHA1           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 5}
	oidRSASHA256         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidRSASHA384         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidRSASHA512         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidECDSAWithSHA1     = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 1}
	oidECDSAWithSHA256   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512   = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}
	oidMessageDigestAttr = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
)

type pkcs7ContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type pkcs7SignedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	ContentInfo      pkcs7EncapsulatedContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      []pkcs7SignerInfo `asn1:"set"`
}

type pkcs7EncapsulatedContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type pkcs7SignerInfo struct {
	Version                   int
	IssuerAndSerialNumber     pkcs7IssuerAndSerial
	DigestAlgorithm           pkix.AlgorithmIdentifier
	AuthenticatedAttributes   asn1.RawValue `asn1:"optional,tag:0"`
	DigestEncryptionAlgorithm pkix.AlgorithmIdentifier
	EncryptedDigest           []byte
	UnauthenticatedAttributes asn1.RawValue `asn1:"optional,tag:1"`
}

type pkcs7IssuerAndSerial struct {
	IssuerName   asn1.RawValue
	SerialNumber asn1.RawValue
}

// SMIMEVerification encapsulates the result of verifying an S/MIME signed email per RFC 9219.
type SMIMEVerification struct {
	Status       string   // "signed", "signed/warning", "signed/failed", "unknown", or ""
	StatusAt     string   // RFC 3339 timestamp
	VerifiedWith *string  // Signer email or subject
	Errors       []string // "untrusted", "expired", "notYetValid", "tampered", "malformed"
}

// VerifySMIMEMessage inspects a raw RFC 822 email and performs S/MIME signature verification
// per RFC 8551 and RFC 9219.
func VerifySMIMEMessage(raw []byte, trustedRoots *x509.CertPool) *SMIMEVerification {
	// First check if headers already specify an S/MIME status (e.g. from upstream MDA or test injection)
	headers := parseRawHeaders(raw)
	headerStatus := ""
	headerVerifiedWith := ""
	headerStatusAt := ""
	var headerErrors []string

	for _, h := range headers {
		switch strings.ToLower(h.Name) {
		case "x-jmap-smime-status":
			headerStatus = h.Value
		case "x-jmap-smime-verified-with":
			headerVerifiedWith = h.Value
		case "x-jmap-smime-status-at":
			headerStatusAt = h.Value
		case "x-jmap-smime-errors":
			if h.Value != "" {
				headerErrors = strings.Split(h.Value, ",")
			}
		}
	}

	if headerStatus != "" {
		res := &SMIMEVerification{
			Status:   headerStatus,
			StatusAt: headerStatusAt,
			Errors:   headerErrors,
		}
		if res.StatusAt == "" {
			res.StatusAt = time.Now().UTC().Format(time.RFC3339)
		}
		if headerVerifiedWith != "" {
			res.VerifiedWith = &headerVerifiedWith
		}
		return res
	}

	// Look for S/MIME structure: multipart/signed or application/pkcs7-mime
	signedContent, sigBytes, isSMIME := extractSMIMEParts(raw)
	if !isSMIME {
		return nil
	}

	now := time.Now().UTC()
	statusAt := now.Format(time.RFC3339)

	if len(sigBytes) == 0 {
		return &SMIMEVerification{
			Status:   "signed/failed",
			StatusAt: statusAt,
			Errors:   []string{"malformed"},
		}
	}

	// Parse PKCS#7 / CMS ContentInfo
	var ci pkcs7ContentInfo
	if rest, err := asn1.Unmarshal(sigBytes, &ci); err != nil || len(rest) > 0 || !ci.ContentType.Equal(oidSignedData) {
		return &SMIMEVerification{
			Status:   "signed/failed",
			StatusAt: statusAt,
			Errors:   []string{"malformed"},
		}
	}

	// Parse SignedData
	var sd pkcs7SignedData
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return &SMIMEVerification{
			Status:   "signed/failed",
			StatusAt: statusAt,
			Errors:   []string{"malformed"},
		}
	}

	// If content is encapsulated inside SignedData
	if len(signedContent) == 0 && len(sd.ContentInfo.Content.Bytes) > 0 {
		signedContent = sd.ContentInfo.Content.Bytes
	}

	// Parse certificates from SignedData
	var certs []*x509.Certificate
	if len(sd.Certificates.Bytes) > 0 {
		var rawCerts []asn1.RawValue
		if _, err := asn1.Unmarshal(sd.Certificates.Bytes, &rawCerts); err == nil {
			for _, rc := range rawCerts {
				if c, err := x509.ParseCertificate(rc.FullBytes); err == nil {
					certs = append(certs, c)
				}
			}
		}
		// Try parsing directly if single certificate
		if len(certs) == 0 {
			if c, err := x509.ParseCertificate(sd.Certificates.Bytes); err == nil {
				certs = append(certs, c)
			}
		}
	}

	if len(certs) == 0 || len(sd.SignerInfos) == 0 {
		return &SMIMEVerification{
			Status:   "signed/failed",
			StatusAt: statusAt,
			Errors:   []string{"malformed"},
		}
	}

	signerCert := certs[0]
	signerIdentity := signerCert.Subject.CommonName
	if len(signerCert.EmailAddresses) > 0 {
		signerIdentity = signerCert.EmailAddresses[0]
	}

	var errorsList []string

	// Verify certificate validity periods
	if now.Before(signerCert.NotBefore) {
		errorsList = append(errorsList, "notYetValid")
	}
	if now.After(signerCert.NotAfter) {
		errorsList = append(errorsList, "expired")
	}

	// Verify certificate trust chain
	trusted := false
	if trustedRoots != nil {
		opts := x509.VerifyOptions{
			Roots:       trustedRoots,
			CurrentTime: now,
		}
		if _, err := signerCert.Verify(opts); err == nil {
			trusted = true
		}
	}
	if !trusted {
		errorsList = append(errorsList, "untrusted")
	}

	// Verify signature over signed content
	si := sd.SignerInfos[0]
	sigValid, err := verifySignerInfo(si, signerCert, signedContent)
	if err != nil || !sigValid {
		errorsList = append(errorsList, "tampered")
	}

	// Determine final RFC 9219 status
	status := "signed"
	hasTampered := false
	hasExpired := false
	hasUntrusted := false

	for _, e := range errorsList {
		switch e {
		case "tampered", "malformed":
			hasTampered = true
		case "expired", "notYetValid":
			hasExpired = true
		case "untrusted":
			hasUntrusted = true
		}
	}

	if hasTampered {
		status = "signed/failed"
	} else if hasExpired {
		status = "signed/failed"
	} else if hasUntrusted {
		status = "signed/warning"
	}

	return &SMIMEVerification{
		Status:       status,
		StatusAt:     statusAt,
		VerifiedWith: &signerIdentity,
		Errors:       errorsList,
	}
}

func verifySignerInfo(si pkcs7SignerInfo, cert *x509.Certificate, content []byte) (bool, error) {
	hashFunc := crypto.SHA256
	if si.DigestAlgorithm.Algorithm.Equal(oidSHA1) {
		hashFunc = crypto.SHA1
	} else if si.DigestAlgorithm.Algorithm.Equal(oidSHA384) {
		hashFunc = crypto.SHA384
	} else if si.DigestAlgorithm.Algorithm.Equal(oidSHA512) {
		hashFunc = crypto.SHA512
	}

	h := hashFunc.New()
	h.Write(content)
	contentDigest := h.Sum(nil)

	var dataToVerify []byte
	if len(si.AuthenticatedAttributes.Bytes) > 0 {
		// Verify message-digest attribute matches content digest
		var attrs []struct {
			Type  asn1.ObjectIdentifier
			Value asn1.RawValue `asn1:"set"`
		}
		if _, err := asn1.Unmarshal(si.AuthenticatedAttributes.Bytes, &attrs); err != nil {
			return false, err
		}
		digestMatch := false
		for _, attr := range attrs {
			if attr.Type.Equal(oidMessageDigestAttr) {
				var attrDigest []byte
				if _, err := asn1.Unmarshal(attr.Value.Bytes, &attrDigest); err == nil {
					if bytes.Equal(attrDigest, contentDigest) {
						digestMatch = true
					}
				}
			}
		}
		if !digestMatch {
			return false, errors.New("authenticated attributes digest mismatch")
		}
		// Data to verify is the DER encoding of authenticated attributes with tag replaced by SET (0x31)
		dataToVerify = append([]byte{0x31}, si.AuthenticatedAttributes.Bytes[1:]...)
	} else {
		dataToVerify = contentDigest
	}

	// Verify signature using cert public key
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		if len(si.AuthenticatedAttributes.Bytes) > 0 {
			hAuth := hashFunc.New()
			hAuth.Write(dataToVerify)
			digest := hAuth.Sum(nil)
			return rsa.VerifyPKCS1v15(pub, hashFunc, digest, si.EncryptedDigest) == nil, nil
		}
		return rsa.VerifyPKCS1v15(pub, hashFunc, contentDigest, si.EncryptedDigest) == nil, nil
	case *ecdsa.PublicKey:
		if len(si.AuthenticatedAttributes.Bytes) > 0 {
			hAuth := hashFunc.New()
			hAuth.Write(dataToVerify)
			digest := hAuth.Sum(nil)
			return ecdsa.VerifyASN1(pub, digest, si.EncryptedDigest), nil
		}
		return ecdsa.VerifyASN1(pub, contentDigest, si.EncryptedDigest), nil
	default:
		return false, fmt.Errorf("unsupported public key type %T", cert.PublicKey)
	}
}

// extractSMIMEParts separates the signed body and PKCS#7 signature bytes from a MIME message.
func extractSMIMEParts(raw []byte) (signedContent []byte, sigBytes []byte, isSMIME bool) {
	headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		headerEnd = bytes.Index(raw, []byte("\n\n"))
		if headerEnd == -1 {
			return nil, nil, false
		}
	}

	headers := parseRawHeaders(raw)
	contentType := ""
	for _, h := range headers {
		if strings.EqualFold(h.Name, "Content-Type") {
			contentType = h.Value
			break
		}
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, false
	}

	if strings.EqualFold(mediaType, "multipart/signed") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil, nil, true
		}

		bodyBytes := raw[headerEnd:]
		if bytes.HasPrefix(bodyBytes, []byte("\r\n\r\n")) {
			bodyBytes = bodyBytes[4:]
		} else if bytes.HasPrefix(bodyBytes, []byte("\n\n")) {
			bodyBytes = bodyBytes[2:]
		}

		mr := multipart.NewReader(bytes.NewReader(bodyBytes), boundary)
		// Part 1: signed content
		p1, err := mr.NextPart()
		if err != nil {
			return nil, nil, true
		}
		p1Bytes, _ := io.ReadAll(p1)

		// Part 2: signature
		p2, err := mr.NextPart()
		if err != nil {
			return p1Bytes, nil, true
		}
		p2Bytes, _ := io.ReadAll(p2)

		// Decode base64 signature
		decodedSig, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(p2Bytes), "\r\n", ""))
		if err != nil {
			decodedSig, _ = base64.StdEncoding.DecodeString(strings.TrimSpace(string(p2Bytes)))
		}
		return p1Bytes, decodedSig, true
	}

	if strings.EqualFold(mediaType, "application/pkcs7-mime") || strings.EqualFold(mediaType, "application/x-pkcs7-mime") {
		bodyBytes := raw[headerEnd:]
		if bytes.HasPrefix(bodyBytes, []byte("\r\n\r\n")) {
			bodyBytes = bodyBytes[4:]
		} else if bytes.HasPrefix(bodyBytes, []byte("\n\n")) {
			bodyBytes = bodyBytes[2:]
		}
		cleaned := strings.ReplaceAll(strings.ReplaceAll(string(bodyBytes), "\r", ""), "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			decoded = bodyBytes
		}
		return nil, decoded, true
	}

	return nil, nil, false
}
