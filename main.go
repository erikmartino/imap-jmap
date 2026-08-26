package main

import (
	"context"
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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"imap-jmap/dav"
	"imap-jmap/jmap"
	"imap-jmap/jmap/imapsmtp"
	"imap-jmap/jmap/memory"
	"imap-jmap/jmap/nextcloud"
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

	backendType := os.Getenv("BACKEND_TYPE")
	if backendType == "" {
		backendType = "memory"
	}
	imapServer := os.Getenv("IMAP_SERVER")
	if imapServer == "" {
		imapServer = "dovecot:143"
	}
	smtpTargetServer := os.Getenv("SMTP_SERVER")
	if smtpTargetServer == "" {
		smtpTargetServer = "smtp:25"
	}

	session := jmap.DefaultSession(publicURL, "user@example.com")

	var mailBackend jmap.MailBackend
	var blobBackend jmap.BlobBackend

	if backendType == "imapsmtp" {
		log.Printf("Initializing IMAP/SMTP Gateway Backend (IMAP: %s, SMTP: %s)", imapServer, smtpTargetServer)
		gwBackend := imapsmtp.New(imapServer, smtpTargetServer)
		mailBackend = gwBackend
		blobBackend = gwBackend
	} else {
		log.Printf("Initializing Memory Backend")
		memBackend := memory.NewMemoryBackend()
		memBlobBackend := memory.NewMemoryBlobBackend()
		mailBackend = memBackend
		blobBackend = memBlobBackend
	}

	nextcloudURL := os.Getenv("NEXTCLOUD_URL")
	if nextcloudURL == "" && backendType == "imapsmtp" {
		nextcloudURL = "http://nextcloud:80"
	}

	var calBackend jmap.CalendarsBackend
	var contactsBackend jmap.ContactsBackend
	var fileNodeBackend jmap.FileNodeBackend

	if nextcloudURL != "" {
		log.Printf("Initializing Nextcloud Backend at %s (CalDAV, CardDAV, WebDAV)", nextcloudURL)
		ncClient := nextcloud.NewClient(nextcloudURL)
		calBackend = nextcloud.NewCalendarsBackend(ncClient)
		contactsBackend = nextcloud.NewContactsBackend(ncClient)
		fileNodeBackend = nextcloud.NewFileNodeBackend(ncClient)
	} else {
		calBackend = memory.NewMemoryCalendarsBackend()
		contactsBackend = memory.NewMemoryContactsBackend()
		fileNodeBackend = memory.NewMemoryFileNodeBackend()
	}

	memSieveBackend := memory.NewMemorySieveBackend()
	memIMAPBackend := memory.NewMemoryIMAPAccessBackend()
	devAuthBackend := memory.NewMemoryAuthBackend()
	devAuthBackend.SetBackends(mailBackend, blobBackend, calBackend, contactsBackend, fileNodeBackend)

	var authBackend jmap.AuthBackend = devAuthBackend
	if backendType == "imapsmtp" {
		if gw, ok := mailBackend.(*imapsmtp.IMAPSMTPBackend); ok {
			secretKey := os.Getenv("SESSION_SECRET")
			if secretKey == "" {
				secretKey = "imap-jmap-secret-session-key-32b!"
			}
			authBackend = imapsmtp.NewAuthBackend(gw.Pool(), secretKey)
		}
	}
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
	// For the IMAP/SMTP gateway deployment the memory auth backend is not in the auth path, so
	// seed through the live backends on first successful authentication instead.
	if backendType == "imapsmtp" {
		authBackend = &seedingAuthBackend{
			inner:  authBackend,
			seeded: make(map[string]bool),
			seedFn: func(ctx context.Context, accountID, subject string) {
				accountCtx := jmap.ContextWithAccountID(context.Background(), accountID)
				accountCtx = jmap.ContextWithSubject(accountCtx, subject)
				accountCtx = jmap.ContextWithCredentials(accountCtx, subject, subject)
				memory.SeedAccountSampleData(accountCtx, accountID, mailBackend, blobBackend, calBackend, contactsBackend, fileNodeBackend)
			},
		}
	}

	outboundSender := smtp.NewMXOutboundSender()
	if *primaryDomain != "" {
		outboundSender.LocalName = "mail." + *primaryDomain
	}
	if sn := os.Getenv("SERVER_NAME"); sn != "" {
		outboundSender.LocalName = sn
	}

	server := jmap.NewServer(
		session,
		jmap.WithMailBackend(mailBackend),
		jmap.WithBlobBackend(blobBackend),
		jmap.WithCalendarsBackend(calBackend),
		jmap.WithContactsBackend(contactsBackend),
		jmap.WithSieveBackend(memSieveBackend),
		jmap.WithIMAPAccessBackend(memIMAPBackend),
		jmap.WithFileNodeBackend(fileNodeBackend),
		jmap.WithAuthBackend(authBackend),
		jmap.WithAccountResolver(accountResolver),
		jmap.WithAllowedRecipients(allowedSlice),
		jmap.WithOutboundSender(outboundSender),
		jmap.WithPublicBaseURL(publicURL),
	)
	if mb, ok := mailBackend.(interface{ SetBroadcaster(*jmap.Broadcaster) }); ok {
		mb.SetBroadcaster(server.Broadcaster)
	}

	if cb, ok := calBackend.(interface{ SetBroadcaster(*jmap.Broadcaster) }); ok {
		cb.SetBroadcaster(server.Broadcaster)
	}
	if cb, ok := contactsBackend.(interface{ SetBroadcaster(*jmap.Broadcaster) }); ok {
		cb.SetBroadcaster(server.Broadcaster)
	}
	memSieveBackend.SetBroadcaster(server.Broadcaster)
	if fb, ok := fileNodeBackend.(interface{ SetBroadcaster(*jmap.Broadcaster) }); ok {
		fb.SetBroadcaster(server.Broadcaster)
	}

	smtpServer := smtp.NewServer(smtpAddr, mailBackend, blobBackend, calBackend,
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
	// 587) requires SMTP-AUTH (RFC 6409 Section 7) and binds the envelope
	// sender to the authenticated identity (RFC 6409 Section 6.1). The submission
	// boundary is trusted (SEC-4) so iTIP scheduling messages from an
	// authenticated client are applied without DNS sender authentication.
	submissionServer := smtp.NewServer(submissionAddr, mailBackend, blobBackend, calBackend,
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

	davServer := dav.NewServer(calBackend, contactsBackend)
	httpMux := http.NewServeMux()
	httpMux.Handle("/caldav/", davServer.CalDAVHandler)
	httpMux.Handle("/carddav/", davServer.CardDAVHandler)
	httpMux.Handle("/", server.Handler())

	// Start HTTPS TLS Listener for browser clients enforcing HTTPS connect-src CSP.
	// Prefer a caller-supplied certificate (e.g. mkcert, which is trusted by the
	// browser so there is no warning); fall back to reusing or generating a persistent
	// self-signed certificate so certificate trust is preserved across restarts.
	tlsCert, certErr := getOrGenerateCertificate(*tlsCertFile, *tlsKeyFile)
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
			log.Printf("Starting HTTPS TLS server on https://%s", httpsAddr)
			if err := httpsServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Printf("HTTPS TLS server error: %v", err)
			}
		}()
	} else {
		log.Printf("HTTPS TLS disabled (failed to obtain certificate: %v)", certErr)
	}

	log.Printf("Starting JMAP & WebDAV server on http://%s (public URL: %s)", addr, publicURL)
	log.Printf("Discovery endpoint: %s/.well-known/jmap", publicURL)
	log.Printf("CalDAV endpoint: %s/caldav/", publicURL)
	log.Printf("CardDAV endpoint: %s/carddav/", publicURL)

	if err := http.ListenAndServe(addr, httpMux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// seedingAuthBackend wraps a jmap.AuthBackend and runs a one-time per-account
// callback after the first successful credential authentication. Used by the
// IMAP/SMTP gateway deployment to lazily seed sample data (the memory auth
// backend does this internally, but it is not in the auth path there).
type seedingAuthBackend struct {
	inner  jmap.AuthBackend
	seedFn func(ctx context.Context, accountID, subject string)
	mu     sync.Mutex
	seeded map[string]bool
}

var _ jmap.AuthBackend = (*seedingAuthBackend)(nil)
var _ jmap.TokenCredentialsExtractor = (*seedingAuthBackend)(nil)

func (s *seedingAuthBackend) Authenticate(ctx context.Context, username, password string) (string, error) {
	token, err := s.inner.Authenticate(ctx, username, password)
	if err == nil {
		s.maybeSeed(jmap.AccountIDForSubject(username), username)
	}
	return token, err
}

func (s *seedingAuthBackend) ValidateCredentials(ctx context.Context, username, password string) (string, error) {
	accountID, err := s.inner.ValidateCredentials(ctx, username, password)
	if err == nil {
		s.maybeSeed(accountID, username)
	}
	return accountID, err
}

func (s *seedingAuthBackend) ValidateToken(ctx context.Context, token string) (string, string, error) {
	return s.inner.ValidateToken(ctx, token)
}

func (s *seedingAuthBackend) ExtractCredentials(ctx context.Context, token string) (string, string, bool) {
	if ex, ok := s.inner.(jmap.TokenCredentialsExtractor); ok {
		return ex.ExtractCredentials(ctx, token)
	}
	return "", "", false
}

func (s *seedingAuthBackend) maybeSeed(accountID, subject string) {
	if accountID == "" || s.seedFn == nil {
		return
	}
	s.mu.Lock()
	if s.seeded[accountID] {
		s.mu.Unlock()
		return
	}
	s.seeded[accountID] = true
	s.mu.Unlock()
	s.seedFn(context.Background(), accountID, subject)
}

// defaultCacheDir returns a platform-appropriate directory for persistent cache files.
func defaultCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "imap-jmap")
	}
	return filepath.Join(os.TempDir(), "imap-jmap-certs")
}

// saveCertAndKey writes the certificate and private key PEM data to disk with appropriate permissions.
func saveCertAndKey(certFile, keyFile string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(filepath.Dir(certFile), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return err
	}
	return nil
}

// loadTLSCertificate loads a PEM certificate/key pair from disk (e.g. an mkcert cert).
// It returns an error when either path is empty or the files cannot be loaded.
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

// getOrGenerateCertificate resolves a TLS certificate for the HTTPS listener:
// 1. If explicit certFile and keyFile exist on disk, loads and returns them.
// 2. If certFile and keyFile are specified but don't exist yet, generates a self-signed
//    certificate and saves it to those paths (if writable) so subsequent starts reuse it.
// 3. If certFile and keyFile are not specified, looks for an existing cached self-signed
//    certificate in default search locations (./certs, user cache dir). If found, reuses it.
// 4. Otherwise, generates a self-signed certificate, persists it to the first writable default
//    location, and returns it.
func getOrGenerateCertificate(certFile, keyFile string) (tls.Certificate, error) {
	if certFile != "" && keyFile != "" {
		cert, err := loadTLSCertificate(certFile, keyFile)
		if err == nil {
			log.Printf("HTTPS TLS: loaded certificate from %s (key %s)", certFile, keyFile)
			return cert, nil
		}
		log.Printf("HTTPS TLS: configured cert/key (%s, %s) not loaded: %v; generating persistent self-signed cert", certFile, keyFile, err)

		certPEM, keyPEM, tlsCert, genErr := generateSelfSignedCertBytes()
		if genErr != nil {
			return tls.Certificate{}, genErr
		}
		if saveErr := saveCertAndKey(certFile, keyFile, certPEM, keyPEM); saveErr == nil {
			log.Printf("HTTPS TLS: saved self-signed certificate to %s (key %s) for reuse across restarts", certFile, keyFile)
		} else {
			log.Printf("HTTPS TLS: could not save self-signed cert to %s/%s (%v); falling back to cache directory", certFile, keyFile, saveErr)
			fallbackDir := defaultCacheDir()
			_ = saveCertAndKey(filepath.Join(fallbackDir, "cert.pem"), filepath.Join(fallbackDir, "key.pem"), certPEM, keyPEM)
		}
		return tlsCert, nil
	}

	// No explicit paths provided: check default persistent cache candidates
	candidates := []struct{ cert, key string }{
		{cert: "certs/cert.pem", key: "certs/key.pem"},
		{cert: filepath.Join(defaultCacheDir(), "cert.pem"), key: filepath.Join(defaultCacheDir(), "key.pem")},
	}

	for _, c := range candidates {
		if c.cert == "" || c.key == "" {
			continue
		}
		if cert, err := loadTLSCertificate(c.cert, c.key); err == nil {
			log.Printf("HTTPS TLS: reusing persistent self-signed certificate from %s (key %s)", c.cert, c.key)
			return cert, nil
		}
	}

	// Generate fresh cert and try persisting to the first writable candidate
	certPEM, keyPEM, tlsCert, err := generateSelfSignedCertBytes()
	if err != nil {
		return tls.Certificate{}, err
	}
	for _, c := range candidates {
		if c.cert == "" || c.key == "" {
			continue
		}
		if saveErr := saveCertAndKey(c.cert, c.key, certPEM, keyPEM); saveErr == nil {
			log.Printf("HTTPS TLS: saved persistent self-signed certificate to %s (key %s) for reuse across restarts", c.cert, c.key)
			break
		}
	}
	return tlsCert, nil
}

func generateSelfSignedCert() (tls.Certificate, error) {
	_, _, cert, err := generateSelfSignedCertBytes()
	return cert, err
}

func generateSelfSignedCertBytes() ([]byte, []byte, tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, tls.Certificate{}, err
	}

	notBefore := time.Now().Add(-1 * time.Hour)
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, tls.Certificate{}, err
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
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("0.0.0.0"), net.IPv6loopback},
		DNSNames:              []string{"localhost", "127.0.0.1", "imap-jmap"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, tls.Certificate{}, err
	}

	return certPEM, keyPEM, tlsCert, nil
}
