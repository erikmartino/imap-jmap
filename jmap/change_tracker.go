package jmap

import (
	"imap-jmap/jmap/jmappush"
)

// ChangeEntry records a single state mutation per RFC 8620 Section 5.2.
type ChangeEntry = jmappush.ChangeEntry

// ChangeTracker maintains a monotonically increasing state token and bounded history.
type ChangeTracker = jmappush.ChangeTracker

func NewChangeTracker(maxKeep int) *ChangeTracker {
	return jmappush.NewChangeTracker(maxKeep)
}

func ParseNumericStateToken(s string) (uint64, bool) {
	return jmappush.ParseNumericStateToken(s)
}
