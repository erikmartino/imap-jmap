package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

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

	publicURL := os.Getenv("PUBLIC_URL")

	port := flag.String("port", defaultPort, "HTTP server listening port")
	host := flag.String("host", defaultHost, "HTTP server listening host")
	smtpPort := flag.String("smtp-port", defaultSMTPPort, "SMTP receiver listening port")
	smtpHost := flag.String("smtp-host", defaultSMTPHost, "SMTP receiver listening host")
	flag.Parse()

	addr := fmt.Sprintf("%s:%s", *host, *port)
	smtpAddr := fmt.Sprintf("%s:%s", *smtpHost, *smtpPort)
	if publicURL == "" {
		publicURL = fmt.Sprintf("http://%s", addr)
	}

	session := jmap.DefaultSession(publicURL)
	memBackend := memory.NewMemoryBackend()
	memBlobBackend := memory.NewMemoryBlobBackend()
	memCalBackend := memory.NewMemoryCalendarsBackend()
	memContactsBackend := memory.NewMemoryContactsBackend()
	memSieveBackend := memory.NewMemorySieveBackend()
	memIMAPBackend := memory.NewMemoryIMAPAccessBackend()
	authBackend := memory.NewMemoryAuthBackend()

	// Seed realistic sample emails and calendars for server runtime execution
	memory.SeedSampleData(memBackend, memCalBackend)

	server := jmap.NewServer(
		session,
		jmap.WithMailBackend(memBackend),
		jmap.WithBlobBackend(memBlobBackend),
		jmap.WithCalendarsBackend(memCalBackend),
		jmap.WithContactsBackend(memContactsBackend),
		jmap.WithSieveBackend(memSieveBackend),
		jmap.WithIMAPAccessBackend(memIMAPBackend),
		jmap.WithAuthBackend(authBackend),
	)
	memBackend.SetBroadcaster(server.Broadcaster)

	smtpServer := smtp.NewServer(smtpAddr, memBackend, memBlobBackend, memCalBackend)
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

	log.Printf("Starting JMAP & WebDAV server on http://%s (public URL: %s)", addr, publicURL)
	log.Printf("Discovery endpoint: %s/.well-known/jmap", publicURL)
	log.Printf("CalDAV endpoint: %s/caldav/", publicURL)
	log.Printf("CardDAV endpoint: %s/carddav/", publicURL)

	if err := http.ListenAndServe(addr, httpMux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
