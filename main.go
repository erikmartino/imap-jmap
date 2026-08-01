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

	port := flag.String("port", defaultPort, "HTTP server listening port")
	host := flag.String("host", "localhost", "HTTP server listening host")
	flag.Parse()

	addr := fmt.Sprintf("%s:%s", *host, *port)
	baseURL := fmt.Sprintf("http://%s", addr)

	session := jmap.DefaultSession(baseURL)
	server := jmap.NewServer(session)

	log.Printf("Starting JMAP server on http://%s", addr)
	log.Printf("Discovery endpoint: %s/.well-known/jmap", baseURL)

	if err := http.ListenAndServe(addr, server.Handler()); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
