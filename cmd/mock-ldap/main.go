package main

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lor00x/goldap/message"
	"github.com/vjeantet/ldapserver"
)

func main() {
	port := os.Getenv("LDAP_PORT")
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

	if username != "" && (password == username || password != "") {
		log.Printf("[MockLDAP] Bind SUCCESS for %s", username)
		w.Write(ldapserver.NewBindResponse(ldapserver.LDAPResultSuccess))
		return
	}

	log.Printf("[MockLDAP] Bind FAILED for %s", username)
	w.Write(ldapserver.NewBindResponse(ldapserver.LDAPResultInvalidCredentials))
}

func handleSearch(w ldapserver.ResponseWriter, m *ldapserver.Message) {
	r := m.GetSearchRequest()
	log.Printf("[MockLDAP] Search BaseDN=%s Filter=%s", r.BaseObject(), r.Filter())

	baseDN := string(r.BaseObject())
	username := extractUsername(baseDN)
	if username == "" {
		username = "user@example.com"
	}

	e := ldapserver.NewSearchResultEntry(baseDN)
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
	return dn
}
