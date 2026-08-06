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
	primaryDomain := flag.String("primary-domain", defaultPrimaryDomain, "Primary email domain for local account resolution")
	allowedRecipientsStr := flag.String("allowed-recipients", defaultAllowedRecipients, "Comma-separated list of allowed external recipient email addresses")
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
	publicURL := os.Getenv("PUBLIC_URL")
	if publicURL == "" {
		publicURL = fmt.Sprintf("http://%s", addr)
	}

	session := jmap.DefaultSession(publicURL, "user@example.com")
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	memCalBackend := memory.NewMemoryCalendarsBackend()
	memContactsBackend := memory.NewMemoryContactsBackend()
	memSieveBackend := memory.NewMemorySieveBackend()
	memIMAPBackend := memory.NewMemoryIMAPAccessBackend()
	memFileNodeBackend := memory.NewMemoryFileNodeBackend()
	authBackend := memory.NewMemoryAuthBackend()
	authBackend.SetBackends(memBackend, memCalBackend, memContactsBackend, memFileNodeBackend)
	accountResolver := jmap.PrimaryDomainResolver{PrimaryDomain: *primaryDomain}

	// Seed realistic sample emails, calendars, contacts, and filenodes for server execution
	memory.SeedSampleData(memBackend, memCalBackend)
	memory.SeedAccountSampleData(context.Background(), "primary", memBackend, memCalBackend, memContactsBackend, memFileNodeBackend)

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
	)
	memBackend.SetBroadcaster(server.Broadcaster)
	memCalBackend.SetBroadcaster(server.Broadcaster)
	memContactsBackend.SetBroadcaster(server.Broadcaster)
	memSieveBackend.SetBroadcaster(server.Broadcaster)
	memFileNodeBackend.SetBroadcaster(server.Broadcaster)

	smtpServer := smtp.NewServer(smtpAddr, memBackend, memBlobBackend, memCalBackend, smtp.WithAccountResolver(accountResolver))
	go func() {
		log.Printf("Starting SMTP receiver server on %s", smtpAddr)
		if err := smtpServer.ListenAndServe(); err != nil {
			log.Printf("SMTP server stopped: %v", err)
		}
	}()

	davServer := dav.NewServer(memCalBackend, memContactsBackend)
	httpMux := http.NewServeMux()
	httpMux.Handle("/caldav/", davServer.CalDAVHandler)
	httpMux.Handle("/carddav/", davServer.CardDAVHandler)
	httpMux.Handle("/", server.Handler())

	// Start HTTPS TLS Listener for browser clients enforcing HTTPS connect-src CSP
	if tlsCert, err := generateSelfSignedCert(); err == nil {
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
	}

	log.Printf("Starting JMAP & WebDAV server on http://%s (public URL: %s)", addr, publicURL)
	log.Printf("Discovery endpoint: %s/.well-known/jmap", publicURL)
	log.Printf("CalDAV endpoint: %s/caldav/", publicURL)
	log.Printf("CardDAV endpoint: %s/carddav/", publicURL)

	if err := http.ListenAndServe(addr, httpMux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
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
