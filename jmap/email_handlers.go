package jmap

import (
	"imap-jmap/jmap/jmapmail"
)

func handleEmailGet(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailGet(backend)
}

func handleEmailChanges(backend MailBackend) MethodHandler {
	return jmapmail.HandleEmailChanges(backend)
}

func parsePropertiesBody(args map[string]any) []string {
	return jmapmail.ParsePropertiesBody(args)
}

func formatEmailGet(em *Email, props []string, parsedHeaderProps []*ParsedHeaderProperty, bodyProps []string, fetchText, fetchHTML, fetchAll bool, maxBytes uint64) any {
	return jmapmail.FormatEmailGet(em, props, parsedHeaderProps, bodyProps, fetchText, fetchHTML, fetchAll, maxBytes)
}

func applyMaxBodyValueBytes(bv EmailBodyValue, maxBytes uint64) EmailBodyValue {
	return jmapmail.ApplyMaxBodyValueBytes(bv, maxBytes)
}
