package main

import (
	"flag"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	defaultServer := os.Getenv("SMTP_SERVER")
	if defaultServer == "" {
		defaultServer = "127.0.0.1"
	}

	defaultPort := os.Getenv("SMTP_PORT")
	if defaultPort == "" {
		defaultPort = "1025"
	}

	defaultFrom := os.Getenv("SMTP_FROM")
	if defaultFrom == "" {
		defaultFrom = "test-sender@example.com"
	}

	defaultTo := os.Getenv("SMTP_TO")
	if defaultTo == "" {
		defaultTo = "recipient@example.com"
	}

	defaultIntervalStr := os.Getenv("INTERVAL")
	if defaultIntervalStr == "" {
		defaultIntervalStr = "20s"
	}

	server := flag.String("server", defaultServer, "SMTP server host")
	port := flag.String("port", defaultPort, "SMTP server port")
	from := flag.String("from", defaultFrom, "Sender email address")
	to := flag.String("to", defaultTo, "Recipient email address")
	intervalStr := flag.String("interval", defaultIntervalStr, "Sending interval (e.g., 20s, 1m)")
	once := flag.Bool("once", false, "Send a single email and exit")
	maxCount := flag.Int("count", 0, "Maximum number of emails to send (0 = infinite)")
	flag.Parse()

	interval, err := time.ParseDuration(*intervalStr)
	if err != nil {
		log.Fatalf("Invalid interval duration %q: %v", *intervalStr, err)
	}

	addr := fmt.Sprintf("%s:%s", *server, *port)
	log.Printf("SMTP Test Sender starting...")
	log.Printf("Target SMTP server: %s", addr)
	log.Printf("From: %s -> To: %s", *from, *to)
	if *once {
		log.Printf("Mode: single send")
	} else {
		log.Printf("Mode: continuous loop every %v", interval)
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	counter := 0
	sendOne := func() bool {
		counter++
		now := time.Now().UTC()
		subject := fmt.Sprintf("Bulwark Test Push #%d - %s", counter, now.Format("15:04:05 UTC"))
		msgID := fmt.Sprintf("<test-push-%d-%d@example.com>", counter, now.Unix())

		body := fmt.Sprintf("From: %s\r\n"+
			"To: %s\r\n"+
			"Subject: %s\r\n"+
			"Message-ID: %s\r\n"+
			"Date: %s\r\n"+
			"Content-Type: text/plain; charset=utf-8\r\n"+
			"\r\n"+
			"This is test push notification email #%d sent at %s to verify real-time updates in Bulwark / JMAP clients.\r\n",
			*from, *to, subject, msgID, now.Format(time.RFC1123Z), counter, now.Format(time.RFC3339))

		err := smtp.SendMail(addr, nil, *from, []string{*to}, []byte(body))
		if err != nil {
			log.Printf("Failed to send email #%d: %v", counter, err)
			return false
		}

		log.Printf("Successfully sent email #%d: %q", counter, subject)
		return true
	}

	// First send immediately
	sendOne()
	if *once || (*maxCount > 0 && counter >= *maxCount) {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			log.Printf("Received termination signal, shutting down sender.")
			return
		case <-ticker.C:
			sendOne()
			if *maxCount > 0 && counter >= *maxCount {
				log.Printf("Reached maximum count (%d), exiting.", *maxCount)
				return
			}
		}
	}
}
