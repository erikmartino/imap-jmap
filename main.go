package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"imap-jmap/jmap"
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

	publicURL := os.Getenv("PUBLIC_URL")

	port := flag.String("port", defaultPort, "HTTP server listening port")
	host := flag.String("host", defaultHost, "HTTP server listening host")
	flag.Parse()

	addr := fmt.Sprintf("%s:%s", *host, *port)
	if publicURL == "" {
		publicURL = fmt.Sprintf("http://%s", addr)
	}

	session := jmap.DefaultSession(publicURL)
	server := jmap.NewServer(session)

	log.Printf("Starting JMAP server on http://%s (public URL: %s)", addr, publicURL)
	log.Printf("Discovery endpoint: %s/.well-known/jmap", publicURL)

	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
