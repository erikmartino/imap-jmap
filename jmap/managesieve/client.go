package managesieve

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"
)

// ScriptInfo represents a Sieve script listed by ManageSieve.
type ScriptInfo struct {
	Name   string
	Active bool
}

// Client is an RFC 5804 ManageSieve protocol client.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

// Dial connects to a ManageSieve server at addr and reads the capability greeting.
func Dial(addr string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
	// Read initial capabilities until OK banner
	if err := c.readUntilOK(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ManageSieve greeting failed: %w", err)
	}
	return c, nil
}

// Close closes the underlying network connection.
func (c *Client) Close() error {
	_, _ = fmt.Fprintf(c.w, "LOGOUT\r\n")
	_ = c.w.Flush()
	return c.conn.Close()
}

func (c *Client) readUntilOK() error {
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "OK") {
			return nil
		}
		if strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "BYE") {
			return fmt.Errorf("server error: %s", line)
		}
	}
}

// Authenticate performs SASL PLAIN authentication.
func (c *Client) Authenticate(user, pass string) error {
	payload := "\x00" + user + "\x00" + pass
	b64 := base64.StdEncoding.EncodeToString([]byte(payload))
	cmd := fmt.Sprintf("AUTHENTICATE \"PLAIN\" \"%s\"\r\n", b64)
	if _, err := c.w.WriteString(cmd); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return c.readUntilOK()
}

// ListScripts retrieves the list of scripts and active status.
func (c *Client) ListScripts() ([]ScriptInfo, error) {
	if _, err := fmt.Fprintf(c.w, "LISTSCRIPTS\r\n"); err != nil {
		return nil, err
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}

	var scripts []ScriptInfo
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "OK") {
			return scripts, nil
		}
		if strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "BYE") {
			return nil, fmt.Errorf("LISTSCRIPTS failed: %s", line)
		}

		parts := parseTokens(line)
		if len(parts) >= 1 {
			name := unquote(parts[0])
			active := len(parts) >= 2 && strings.ToUpper(parts[1]) == "ACTIVE"
			scripts = append(scripts, ScriptInfo{Name: name, Active: active})
		}
	}
}

// GetScript downloads the content of the named script.
func (c *Client) GetScript(name string) (string, error) {
	if _, err := fmt.Fprintf(c.w, "GETSCRIPT \"%s\"\r\n", escapeString(name)); err != nil {
		return "", err
	}
	if err := c.w.Flush(); err != nil {
		return "", err
	}

	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(line, "NO") || strings.HasPrefix(line, "BYE") {
		return "", fmt.Errorf("GETSCRIPT failed: %s", line)
	}

	if !strings.HasPrefix(line, "{") {
		return "", fmt.Errorf("unexpected GETSCRIPT response header: %s", line)
	}

	content, err := readLiteral(line, c.r)
	if err != nil {
		return "", err
	}

	if err := c.readUntilOK(); err != nil {
		return "", err
	}

	return content, nil
}

// PutScript uploads a Sieve script to the server.
func (c *Client) PutScript(name, content string) error {
	header := fmt.Sprintf("PUTSCRIPT \"%s\" {%d+}\r\n", escapeString(name), len(content))
	if _, err := c.w.WriteString(header); err != nil {
		return err
	}
	if _, err := c.w.WriteString(content + "\r\n"); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return c.readUntilOK()
}

// SetActive marks a script as active, or deactivates all scripts if name is empty.
func (c *Client) SetActive(name string) error {
	cmd := fmt.Sprintf("SETACTIVE \"%s\"\r\n", escapeString(name))
	if _, err := c.w.WriteString(cmd); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return c.readUntilOK()
}

// DeleteScript deletes a script from the server.
func (c *Client) DeleteScript(name string) error {
	cmd := fmt.Sprintf("DELETESCRIPT \"%s\"\r\n", escapeString(name))
	if _, err := c.w.WriteString(cmd); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return c.readUntilOK()
}

// CheckScript validates a script without saving it.
func (c *Client) CheckScript(content string) error {
	header := fmt.Sprintf("CHECKSCRIPT {%d+}\r\n", len(content))
	if _, err := c.w.WriteString(header); err != nil {
		return err
	}
	if _, err := c.w.WriteString(content + "\r\n"); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return c.readUntilOK()
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}
