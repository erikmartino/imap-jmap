package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lor00x/goldap/message"
	"github.com/vjeantet/ldapserver"
)

func main() {
	port := os.Getenv("LDAP_LISTEN_PORT")
	if port == "" {
		envPort := os.Getenv("LDAP_PORT")
		if strings.Contains(envPort, ":") {
			parts := strings.Split(envPort, ":")
			port = parts[len(parts)-1]
		} else {
			port = envPort
		}
	}
	if port == "" {
		port = "389"
	}

	httpPort := os.Getenv("HTTP_LISTEN_PORT")
	if httpPort == "" {
		httpPort = os.Getenv("PORT")
	}
	if httpPort == "" {
		httpPort = "8080"
	}

	issuer := os.Getenv("OIDC_ISSUER")
	domain := os.Getenv("OIDC_DOMAIN")
	if domain == "" {
		domain = "profundo.dk"
	}

	server := ldapserver.NewServer()
	// No read deadline: closing an idle connection here makes Dovecot's LDAP
	// client pay a reconnect penalty on its next auth request. Instead, apply a
	// per-write deadline so a client that stops reading (wedged auth worker)
	// cannot stall the connection forever — the server closes it and Dovecot
	// reconnects fast, exactly like a real LDAP server.
	server.WriteTimeout = 5 * time.Second
	routes := ldapserver.NewRouteMux()

	routes.Bind(handleBind)
	routes.Search(handleSearch)

	server.Handle(routes)

	listenAddr := "0.0.0.0:" + port
	log.Printf("Starting Mock LDAP server on %s ...", listenAddr)

	go func() {
		if err := server.ListenAndServe(listenAddr); err != nil {
			log.Fatalf("LDAP server error: %v", err)
		}
	}()

	oidcSrv, err := NewOIDCServer(issuer, domain)
	if err != nil {
		log.Fatalf("Failed to initialize OIDC server: %v", err)
	}

	httpAddr := "0.0.0.0:" + httpPort
	log.Printf("Starting Mock LDAP OIDC server on http://%s (Issuer: %s) ...", httpAddr, issuer)

	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: oidcSrv.Handler(),
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP OIDC server error: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down Mock LDAP and OIDC servers.")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	server.Stop()
}

func handleBind(w ldapserver.ResponseWriter, m *ldapserver.Message) {
	r := m.GetBindRequest()
	bindDN := string(r.Name())
	password := string(r.AuthenticationSimple())

	log.Printf("[MockLDAP] Bind request: DN=%s", bindDN)
	username := extractUsername(bindDN)

	// Accept admin bind or strict username == password authentication
	if strings.HasPrefix(strings.ToLower(bindDN), "cn=admin") || (username != "" && password == username) {
		log.Printf("[MockLDAP] Bind SUCCESS for %s", username)
		w.Write(ldapserver.NewBindResponse(ldapserver.LDAPResultSuccess))
		return
	}

	log.Printf("[MockLDAP] Bind FAILED for %s", username)
	w.Write(ldapserver.NewBindResponse(ldapserver.LDAPResultInvalidCredentials))
}

func handleSearch(w ldapserver.ResponseWriter, m *ldapserver.Message) {
	r := m.GetSearchRequest()
	filterStr := fmt.Sprintf("%v", r.Filter())
	log.Printf("[MockLDAP] Search BaseDN=%s Filter=%s", r.BaseObject(), filterStr)

	username := extractUsernameFromFilter(filterStr)
	if username == "" || username == "%u" {
		username = extractUsername(string(r.BaseObject()))
	}
	if username == "" || username == "%u" || strings.HasPrefix(username, "ou=") || strings.HasPrefix(username, "dc=") {
		username = "user@example.com"
	}

	baseDN := string(r.BaseObject())
	if baseDN == "" || !strings.Contains(baseDN, "dc=") {
		baseDN = "ou=users,dc=profundo,dc=dk"
	}
	entryDN := fmt.Sprintf("uid=%s,%s", username, baseDN)
	if !strings.Contains(entryDN, "ou=") {
		entryDN = fmt.Sprintf("uid=%s,ou=users,dc=profundo,dc=dk", username)
	}
	e := ldapserver.NewSearchResultEntry(entryDN)
	e.AddAttribute("objectClass", message.AttributeValue("inetOrgPerson"), message.AttributeValue("posixAccount"), message.AttributeValue("top"))
	e.AddAttribute("uid", message.AttributeValue(username))
	e.AddAttribute("mail", message.AttributeValue(username))
	e.AddAttribute("cn", message.AttributeValue(username))
	e.AddAttribute("sn", message.AttributeValue(username))
	e.AddAttribute("displayName", message.AttributeValue(username))
	e.AddAttribute("userPassword", message.AttributeValue(username))
	e.AddAttribute("entryUUID", message.AttributeValue(username))
	e.AddAttribute("nsUniqueId", message.AttributeValue(username))
	e.AddAttribute("objectGUID", message.AttributeValue(username))
	e.AddAttribute("ipaUniqueID", message.AttributeValue(username))
	e.AddAttribute("uidNumber", message.AttributeValue("1000"))
	e.AddAttribute("gidNumber", message.AttributeValue("1000"))
	e.AddAttribute("homeDirectory", message.AttributeValue("/home/"+username))

	w.Write(e)
	w.Write(ldapserver.NewSearchResultDoneResponse(ldapserver.LDAPResultSuccess))
}

func extractUsername(dn string) string {
	parts := strings.Split(dn, ",")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && (strings.EqualFold(kv[0], "uid") || strings.EqualFold(kv[0], "mail") || strings.EqualFold(kv[0], "cn")) {
			return kv[1]
		}
	}
	if !strings.Contains(dn, "=") {
		return dn
	}
	return ""
}

func extractUsernameFromFilter(filter string) string {
	parts := strings.FieldsFunc(filter, func(r rune) bool {
		return r == ' ' || r == '{' || r == '}' || r == '[' || r == ']' || r == '(' || r == ')' || r == '=' || r == '|' || r == '&'
	})
	for i, part := range parts {
		if (part == "uid" || part == "mail" || part == "cn") && i+1 < len(parts) {
			val := parts[i+1]
			if val != "%u" && val != "" && !strings.Contains(val, "(") {
				return val
			}
		}
	}
	return ""
}
