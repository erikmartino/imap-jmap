package jmapcore

import (
	"errors"
	"regexp"
)

// ErrNotFound is returned when a referenced object id does not exist.
// Handlers MUST map it to a "notFound" SetError per RFC 8620 Section 5.3.
// @spec RFC8620#5.3-p1-MUST
var ErrNotFound = errors.New("not found")

// Id represents a JMAP Id as defined in RFC 8620 Section 1.6 & 1.7.5.
// @spec RFC8620#1.6-p1-MUST
type Id string

var idRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

// Validate checks if the Id matches RFC 8620 Section 1.6 rules.
// @spec RFC8620#1.6-p1-MUST
func (id Id) Validate() bool {
	return idRegexp.MatchString(string(id))
}
