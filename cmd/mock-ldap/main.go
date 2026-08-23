package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lor00x/goldap/message"
	"github.com/vjeantet/ldapserver"
)

func main() {
	port := os.Getenv("LDAP_LISTEN_PORT")
	if port == "" {
		port = os.Getenv("LDAP_PORT")
	}
	if strings.Contains(port, "://") {
		parts := strings.Split(port, ":")
		port = parts[len(parts)-1]
	}
	if port == "" {
		port = "389"
	}


	server := ldapserver.NewServer()
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("Shutting down Mock LDAP server.")
	server.Stop()
}

func handleBind(w ldapserver.ResponseWriter, m *ldapserver.Message) {
	r := m.GetBindRequest()
	bindDN := string(r.Name())
	password := string(r.AuthenticationSimple())

	log.Printf("[MockLDAP] Bind request: DN=%s", bindDN)
	username := extractUsername(bindDN)

	// Accept admin bind or strict username == password authentication
	if bindDN == "cn=admin,dc=example,dc=org" || (username != "" && password == username) {
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
	if username == "" {
		username = extractUsername(string(r.BaseObject()))
	}
	if username == "" || strings.HasPrefix(username, "ou=") || strings.HasPrefix(username, "dc=") {
		username = "user@example.com"
	}

	entryDN := fmt.Sprintf("uid=%s,ou=users,dc=example,dc=org", username)
	e := ldapserver.NewSearchResultEntry(entryDN)
	e.AddAttribute("objectClass", message.AttributeValue("inetOrgPerson"), message.AttributeValue("posixAccount"), message.AttributeValue("top"))
	e.AddAttribute("uid", message.AttributeValue(username))
	e.AddAttribute("mail", message.AttributeValue(username))
	e.AddAttribute("cn", message.AttributeValue(username))
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
		return r == ' ' || r == '{' || r == '}' || r == '[' || r == ']' || r == '(' || r == ')'
	})
	for i, part := range parts {
		if (part == "uid" || part == "mail" || part == "cn") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
