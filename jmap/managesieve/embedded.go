package managesieve

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

// EmbeddedServer is an in-process RFC 5804 ManageSieve server for hermetic testing.
type EmbeddedServer struct {
	listener net.Listener
	addr     string
	mu       sync.Mutex
	scripts  map[string]map[string]string // user -> scriptName -> content
	active   map[string]string            // user -> activeScriptName
	closed   bool
}

// NewEmbeddedServer starts an in-process RFC 5804 ManageSieve server on an ephemeral port.
func NewEmbeddedServer() (*EmbeddedServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &EmbeddedServer{
		listener: ln,
		addr:     ln.Addr().String(),
		scripts:  make(map[string]map[string]string),
		active:   make(map[string]string),
	}
	go s.serve()
	return s, nil
}

// Addr returns the host:port address the server is listening on.
func (s *EmbeddedServer) Addr() string {
	return s.addr
}

// Close stops the server listener and closes all connections.
func (s *EmbeddedServer) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.listener.Close()
}

func (s *EmbeddedServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *EmbeddedServer) handleConn(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	// Send RFC 5804 Capabilities Banner
	_, _ = fmt.Fprintf(w, "\"IMPLEMENTATION\" \"InProcess Pigeonhole\"\r\n")
	_, _ = fmt.Fprintf(w, "\"SASL\" \"PLAIN\"\r\n")
	_, _ = fmt.Fprintf(w, "\"SIEVE\" \"fileinto reject vacation envelope date imap4flags subaddress\"\r\n")
	_, _ = fmt.Fprintf(w, "\"STARTTLS\"\r\n")
	_, _ = fmt.Fprintf(w, "OK \"ManageSieve ready.\"\r\n")
	_ = w.Flush()

	var currentUser string

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		parts := parseTokens(line)
		if len(parts) == 0 {
			continue
		}

		cmd := strings.ToUpper(parts[0])

		switch cmd {
		case "CAPABILITY":
			_, _ = fmt.Fprintf(w, "\"IMPLEMENTATION\" \"InProcess Pigeonhole\"\r\n")
			_, _ = fmt.Fprintf(w, "\"SASL\" \"PLAIN\"\r\n")
			_, _ = fmt.Fprintf(w, "\"SIEVE\" \"fileinto reject vacation envelope date imap4flags subaddress\"\r\n")
			_, _ = fmt.Fprintf(w, "OK \"Capability completed.\"\r\n")
			_ = w.Flush()

		case "NOOP":
			_, _ = fmt.Fprintf(w, "OK \"Noop completed.\"\r\n")
			_ = w.Flush()

		case "LOGOUT":
			_, _ = fmt.Fprintf(w, "BYE \"Logout completed.\"\r\n")
			_, _ = fmt.Fprintf(w, "OK \"Logout completed.\"\r\n")
			_ = w.Flush()
			return

		case "AUTHENTICATE":
			// AUTHENTICATE "PLAIN" [base64]
			if len(parts) < 2 {
				_, _ = fmt.Fprintf(w, "NO \"Missing mechanism.\"\r\n")
				_ = w.Flush()
				continue
			}
			mech := strings.Trim(parts[1], "\"")
			if strings.ToUpper(mech) != "PLAIN" {
				_, _ = fmt.Fprintf(w, "NO \"Unsupported SASL mechanism.\"\r\n")
				_ = w.Flush()
				continue
			}
			var authPayload string
			if len(parts) >= 3 {
				authPayload = parts[2]
			} else {
				// Server sends empty challenge
				_, _ = fmt.Fprintf(w, "\"\"\r\n")
				_ = w.Flush()
				authPayload, _ = r.ReadString('\n')
				authPayload = strings.TrimRight(authPayload, "\r\n")
			}
			data, decErr := base64.StdEncoding.DecodeString(strings.Trim(authPayload, "\""))
			if decErr != nil {
				_, _ = fmt.Fprintf(w, "NO \"Invalid SASL encoding.\"\r\n")
				_ = w.Flush()
				continue
			}
			// SASL PLAIN: authzid\0authcid\0passwd
			tokens := strings.Split(string(data), "\x00")
			if len(tokens) < 3 {
				_, _ = fmt.Fprintf(w, "NO \"Invalid SASL PLAIN format.\"\r\n")
				_ = w.Flush()
				continue
			}
			user := tokens[1]
			if user == "" {
				user = tokens[0]
			}
			currentUser = user
			_, _ = fmt.Fprintf(w, "OK \"Logged in as %s\"\r\n", currentUser)
			_ = w.Flush()

		case "LISTSCRIPTS":
			if currentUser == "" {
				_, _ = fmt.Fprintf(w, "NO \"Not authenticated.\"\r\n")
				_ = w.Flush()
				continue
			}
			s.mu.Lock()
			userScripts := s.scripts[currentUser]
			activeScript := s.active[currentUser]
			for name := range userScripts {
				if name == activeScript {
					_, _ = fmt.Fprintf(w, "\"%s\" ACTIVE\r\n", name)
				} else {
					_, _ = fmt.Fprintf(w, "\"%s\"\r\n", name)
				}
			}
			s.mu.Unlock()
			_, _ = fmt.Fprintf(w, "OK \"Listscripts completed.\"\r\n")
			_ = w.Flush()

		case "GETSCRIPT":
			if currentUser == "" {
				_, _ = fmt.Fprintf(w, "NO \"Not authenticated.\"\r\n")
				_ = w.Flush()
				continue
			}
			if len(parts) < 2 {
				_, _ = fmt.Fprintf(w, "NO \"Missing script name.\"\r\n")
				_ = w.Flush()
				continue
			}
			name := unquote(parts[1])
			s.mu.Lock()
			content, exists := s.scripts[currentUser][name]
			s.mu.Unlock()
			if !exists {
				_, _ = fmt.Fprintf(w, "NO (NONEXISTENT) \"Script does not exist.\"\r\n")
				_ = w.Flush()
				continue
			}
			_, _ = fmt.Fprintf(w, "{%d+}\r\n%s\r\n", len(content), content)
			_, _ = fmt.Fprintf(w, "OK \"Getscript completed.\"\r\n")
			_ = w.Flush()

		case "PUTSCRIPT":
			if currentUser == "" {
				_, _ = fmt.Fprintf(w, "NO \"Not authenticated.\"\r\n")
				_ = w.Flush()
				continue
			}
			if len(parts) < 3 {
				_, _ = fmt.Fprintf(w, "NO \"Invalid PUTSCRIPT arguments.\"\r\n")
				_ = w.Flush()
				continue
			}
			name := unquote(parts[1])
			litHeader := parts[2]
			content, err := readLiteral(litHeader, r)
			if err != nil {
				_, _ = fmt.Fprintf(w, "NO \"Failed to read script literal: %v\"\r\n", err)
				_ = w.Flush()
				continue
			}
			s.mu.Lock()
			if s.scripts[currentUser] == nil {
				s.scripts[currentUser] = make(map[string]string)
			}
			s.scripts[currentUser][name] = content
			s.mu.Unlock()
			_, _ = fmt.Fprintf(w, "OK \"Putscript completed.\"\r\n")
			_ = w.Flush()

		case "SETACTIVE":
			if currentUser == "" {
				_, _ = fmt.Fprintf(w, "NO \"Not authenticated.\"\r\n")
				_ = w.Flush()
				continue
			}
			if len(parts) < 2 {
				_, _ = fmt.Fprintf(w, "NO \"Missing script name.\"\r\n")
				_ = w.Flush()
				continue
			}
			name := unquote(parts[1])
			s.mu.Lock()
			if name == "" {
				delete(s.active, currentUser)
			} else if _, exists := s.scripts[currentUser][name]; !exists {
				s.mu.Unlock()
				_, _ = fmt.Fprintf(w, "NO (NONEXISTENT) \"Script does not exist.\"\r\n")
				_ = w.Flush()
				continue
			} else {
				s.active[currentUser] = name
			}
			s.mu.Unlock()
			_, _ = fmt.Fprintf(w, "OK \"Setactive completed.\"\r\n")
			_ = w.Flush()

		case "DELETESCRIPT":
			if currentUser == "" {
				_, _ = fmt.Fprintf(w, "NO \"Not authenticated.\"\r\n")
				_ = w.Flush()
				continue
			}
			if len(parts) < 2 {
				_, _ = fmt.Fprintf(w, "NO \"Missing script name.\"\r\n")
				_ = w.Flush()
				continue
			}
			name := unquote(parts[1])
			s.mu.Lock()
			if s.active[currentUser] == name {
				s.mu.Unlock()
				_, _ = fmt.Fprintf(w, "NO (ACTIVE) \"Cannot delete active script.\"\r\n")
				_ = w.Flush()
				continue
			}
			if s.scripts[currentUser] == nil {
				s.mu.Unlock()
				_, _ = fmt.Fprintf(w, "NO (NONEXISTENT) \"Script does not exist.\"\r\n")
				_ = w.Flush()
				continue
			}
			if _, exists := s.scripts[currentUser][name]; !exists {
				s.mu.Unlock()
				_, _ = fmt.Fprintf(w, "NO (NONEXISTENT) \"Script does not exist.\"\r\n")
				_ = w.Flush()
				continue
			}
			delete(s.scripts[currentUser], name)
			s.mu.Unlock()
			_, _ = fmt.Fprintf(w, "OK \"Deletescript completed.\"\r\n")
			_ = w.Flush()

		case "CHECKSCRIPT":
			if len(parts) < 2 {
				_, _ = fmt.Fprintf(w, "NO \"Missing script literal.\"\r\n")
				_ = w.Flush()
				continue
			}
			_, err := readLiteral(parts[1], r)
			if err != nil {
				_, _ = fmt.Fprintf(w, "NO \"Failed to read script literal: %v\"\r\n", err)
				_ = w.Flush()
				continue
			}
			_, _ = fmt.Fprintf(w, "OK \"Checkscript completed.\"\r\n")
			_ = w.Flush()

		default:
			_, _ = fmt.Fprintf(w, "NO \"Unknown command %s\"\r\n", cmd)
			_ = w.Flush()
		}
	}
}

func readLiteral(header string, r *bufio.Reader) (string, error) {
	header = strings.Trim(header, "{}")
	header = strings.TrimSuffix(header, "+")
	size, err := strconv.Atoi(header)
	if err != nil {
		return "", fmt.Errorf("invalid literal length %q: %w", header, err)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	// Consume trailing CRLF
	_, _ = r.ReadString('\n')
	return string(buf), nil
}
