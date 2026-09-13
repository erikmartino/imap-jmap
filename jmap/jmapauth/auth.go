package jmapauth

import (
	"encoding/base64"
	"errors"
	"strings"
)

// DecodeBase64OrRaw decodes standard base64 strings with or without padding.
func DecodeBase64OrRaw(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Basic ")
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, errors.New("invalid base64")
}
