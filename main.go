package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"imap-jmap/dav"
	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
	"imap-jmap/smtp"
)

func main() {
	defaultPort := os.Getenv("PORT")
	if defaultPort == "" {
		defaultPort = "8080"
	}

	defaultHost := os.Getenv("HOST")
	if defaultHost == "" {
		defaultHost = "0.0.0.0"
	}

	defaultSMTPPort := os.Getenv("SMTP_PORT")
	if defaultSMTPPort == "" {
		defaultSMTPPort = "1025"
	}

	defaultSMTPHost := os.Getenv("SMTP_HOST")
	if defaultSMTPHost == "" {
		defaultSMTPHost = "0.0.0.0"
	}

	defaultHTTPSPort := os.Getenv("HTTPS_PORT")
	if defaultHTTPSPort == "" {
		defaultHTTPSPort = "8443"
	}

	defaultSubmissionPort := os.Getenv("SUBMISSION_PORT")
	if defaultSubmissionPort == "" {
		defaultSubmissionPort = "587"
	}

	defaultPrimaryDomain := os.Getenv("PRIMARY_DOMAIN")
	if defaultPrimaryDomain == "" {
		defaultPrimaryDomain = "example.com"
	}

	defaultAllowedRecipients := os.Getenv("ALLOWED_RECIPIENTS")

	port := flag.String("port", defaultPort, "HTTP server listening port")
	httpsPort := flag.String("https-port", defaultHTTPSPort, "HTTPS TLS server listening port")
	host := flag.String("host", defaultHost, "HTTP server listening host")
	smtpPort := flag.String("smtp-port", defaultSMTPPort, "SMTP receiver listening port")
	smtpHost := flag.String("smtp-host", defaultSMTPHost, "SMTP receiver listening host")
	submissionPort := flag.String("submission-port", defaultSubmissionPort, "SMTP message submission (RFC 6409) listening port (default 587)")
	submissionHost := flag.String("submission-host", os.Getenv("SUBMISSION_HOST"), "SMTP message submission listening host (default same as SMTP host)")
	primaryDomain := flag.String("primary-domain", defaultPrimaryDomain, "Primary email domain for local account resolution")
	allowedRecipientsStr := flag.String("allowed-recipients", defaultAllowedRecipients, "Comma-separated list of allowed external recipient email addresses")
	// TLS cert/key files for the HTTPS server. When provided (e.g. an mkcert cert),
	// they are used instead of the built-in self-signed certificate, so browsers
	tlsCertFile := flag.String("tls-cert", os.Getenv("TLS_CERT_FILE"), "Path to a PEM TLS certificate for the HTTPS server (default: self-signed)")
	tlsKeyFile := flag.String("tls-key", os.Getenv("TLS_KEY_FILE"), "Path to the PEM TLS private key for the HTTPS server (default: self-signed)")
	oidcIssuer := flag.String("oidc-issuer", os.Getenv("OIDC_ISSUER"), "OIDC Issuer URL (e.g. https://auth.profundo.dk/realms/master)")
	oidcJWKSURL := flag.String("oidc-jwks-url", os.Getenv("OIDC_JWKS_URL"), "OIDC JWKS URL (optional, auto-discovered if empty)")
	flag.Parse()

	var allowedSlice []string
	if *allowedRecipientsStr != "" {
		for _, part := range strings.Split(*allowedRecipientsStr, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				allowedSlice = append(allowedSlice, part)
			}
		}
	}

	addr := fmt.Sprintf("%s:%s", *host, *port)
	httpsAddr := fmt.Sprintf("%s:%s", *host, *httpsPort)
	smtpAddr := fmt.Sprintf("%s:%s", *smtpHost, *smtpPort)
	submissionHostStr := *submissionHost
	if submissionHostStr == "" {
		submissionHostStr = *smtpHost
	}
	submissionAddr := fmt.Sprintf("%s:%s", submissionHostStr, *submissionPort)
	publicURL := os.Getenv("PUBLIC_URL")

	session := jmap.DefaultSession(publicURL, "user@example.com")
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	memCalBackend := memory.NewMemoryCalendarsBackend()
	memContactsBackend := memory.NewMemoryContactsBackend()
	memSieveBackend := memory.NewMemorySieveBackend()
	memIMAPBackend := memory.NewMemoryIMAPAccessBackend()
	memFileNodeBackend := memory.NewMemoryFileNodeBackend()
	devAuthBackend := memory.NewMemoryAuthBackend()
	devAuthBackend.SetBackends(memBackend, memBlobBackend, memCalBackend, memContactsBackend, memFileNodeBackend)

	var authBackend jmap.AuthBackend = devAuthBackend
	if *oidcIssuer != "" {
		var oidcFallback jmap.AuthBackend
		// AUTH-2: the in-memory "password == email" credential path MUST NOT leak
		// into a production OIDC deployment. It is only attached as the OIDC
		// credential fallback when explicitly requested via AUTH_DEV_FALLBACK=true;
		// by default the OIDC backend accepts tokens only and rejects every
		// username/password attempt (failing closed).
		if strings.EqualFold(os.Getenv("AUTH_DEV_FALLBACK"), "true") {
			oidcFallback = devAuthBackend
			log.Printf("WARNING: AUTH_DEV_FALLBACK=true — development username==password credentials are enabled alongside OIDC. Do not use in production.")
		}
		oidcBackend, err := jmap.NewOIDCAuthBackend(jmap.OIDCConfig{
			Issuer:          *oidcIssuer,
			JWKSURL:         *oidcJWKSURL,
			FallbackBackend: oidcFallback,
		})
		if err != nil {
			log.Fatalf("Failed to initialize OIDCAuthBackend: %v", err)
		}
		log.Printf("OIDC authentication enabled with issuer %s (dev credential fallback: %t)", *oidcIssuer, oidcFallback != nil)
		authBackend = oidcBackend
	}

	accountResolver := jmap.PrimaryDomainResolver{PrimaryDomain: *primaryDomain}

	// Sample data is dynamically auto-seeded per account upon first login in MemoryAuthBackend.

	server := jmap.NewServer(
		session,
		jmap.WithMailBackend(memBackend),
		jmap.WithBlobBackend(memBlobBackend),
		jmap.WithCalendarsBackend(memCalBackend),
		jmap.WithContactsBackend(memContactsBackend),
		jmap.WithSieveBackend(memSieveBackend),
		jmap.WithIMAPAccessBackend(memIMAPBackend),
		jmap.WithFileNodeBackend(memFileNodeBackend),
		jmap.WithAuthBackend(authBackend),
		jmap.WithAccountResolver(accountResolver),
		jmap.WithAllowedRecipients(allowedSlice),
		jmap.WithOutboundSender(smtp.NewMXOutboundSender()),
		jmap.WithPublicBaseURL(publicURL),
	)
	memBackend.SetBroadcaster(server.Broadcaster)
	memCalBackend.SetBroadcaster(server.Broadcaster)
	memContactsBackend.SetBroadcaster(server.Broadcaster)
	memSieveBackend.SetBroadcaster(server.Broadcaster)
	memFileNodeBackend.SetBroadcaster(server.Broadcaster)

	smtpServer := smtp.NewServer(smtpAddr, memBackend, memBlobBackend, memCalBackend,
		smtp.WithAccountResolver(accountResolver),
		smtp.WithSenderVerifier(smtp.NewSPFDKIMDMARCVerifier()),
	)
	go func() {
		log.Printf("Starting SMTP receiver server on %s", smtpAddr)
		if err := smtpServer.ListenAndServe(); err != nil {
			log.Printf("SMTP server stopped: %v", err)
		}
	}()

	// RFC 6409 Section 3.1: message submission is a distinct transport (port
	// 587) that requires SMTP-AUTH (RFC 6409 Section 7) and binds the envelope
	// sender to the authenticated identity (RFC 6409 Section 6.1). The submission
	// boundary is trusted (SEC-4) so iTIP scheduling messages from an
	// authenticated client are applied without DNS sender authentication.
	submissionServer := smtp.NewServer(submissionAddr, memBackend, memBlobBackend, memCalBackend,
		smtp.WithAccountResolver(accountResolver),
		smtp.WithTransportMode(smtp.TransportModeSubmission),
		smtp.WithAuthenticator(smtp.NewAuthBackendAuthenticator(authBackend)),
		smtp.WithSenderVerifier(smtp.NewSPFDKIMDMARCVerifier()),
	)
	go func() {
		log.Printf("Starting SMTP submission server on %s (AUTH required)", submissionAddr)
		if err := submissionServer.ListenAndServe(); err != nil {
			log.Printf("SMTP submission server stopped: %v", err)
		}
	}()

	davServer := dav.NewServer(memCalBackend, memContactsBackend)
	httpMux := http.NewServeMux()
	httpMux.Handle("/caldav/", davServer.CalDAVHandler)
	httpMux.Handle("/carddav/", davServer.CardDAVHandler)
	httpMux.Handle("/", server.Handler())

	// Start HTTPS TLS Listener for browser clients enforcing HTTPS connect-src CSP.
	// Prefer a caller-supplied certificate (e.g. mkcert, which is trusted by the
	// browser so there is no warning); fall back to a self-signed certificate.
	tlsCert, certErr := loadTLSCertificate(*tlsCertFile, *tlsKeyFile)
	if certErr != nil {
		log.Printf("HTTPS TLS: %v; falling back to a self-signed certificate", certErr)
		tlsCert, certErr = generateSelfSignedCert()
	}
	if certErr == nil {
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
		}
		httpsServer := &http.Server{
			Addr:      httpsAddr,
			Handler:   httpMux,
			TLSConfig: tlsConfig,
		}
		go func() {
			if *tlsCertFile != "" && *tlsKeyFile != "" {
				log.Printf("Starting HTTPS TLS server on https://%s (cert %s)", httpsAddr, *tlsCertFile)
			} else {
				log.Printf("Starting HTTPS TLS server on https://%s (self-signed)", httpsAddr)
			}
			if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Printf("HTTPS TLS server error: %v", err)
			}
		}()
	}

	log.Printf("Starting JMAP & WebDAV server on http://%s (public URL: %s)", addr, publicURL)
	log.Printf("Discovery endpoint: %s/.well-known/jmap", publicURL)
	log.Printf("CalDAV endpoint: %s/caldav/", publicURL)
	log.Printf("CardDAV endpoint: %s/carddav/", publicURL)

	if err := http.ListenAndServe(addr, httpMux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// loadTLSCertificate loads a PEM certificate/key pair from disk (e.g. an mkcert cert).
// It returns an error when either path is empty or the files cannot be loaded, so the
// caller can fall back to a self-signed certificate.
func loadTLSCertificate(certFile, keyFile string) (tls.Certificate, error) {
	if certFile == "" || keyFile == "" {
		return tls.Certificate{}, fmt.Errorf("no TLS cert/key configured")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("loading TLS cert %q / key %q: %w", certFile, keyFile, err)
	}
	return cert, nil
}

func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"JMAP Server Test"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("0.0.0.0")},
		DNSNames:              []string{"localhost", "127.0.0.1", "imap-jmap"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	return tls.X509KeyPair(certPEM, keyPEM)
}
