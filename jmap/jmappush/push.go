package jmappush

import (
	"fmt"
	"strings"
	"sync"

	"imap-jmap/jmap/jmapcore"
)

type Id = jmapcore.Id

// StateChange represents an RFC 8620 Section 7.1 StateChange event payload.
// @spec RFC8620#7.1-p1-MUST
type StateChange struct {
	Type    string                       `json:"@type"`
	Changed map[string]map[string]string `json:"changed"` // accountID -> typeName -> stateToken
}

// ChangeEntry records a single state mutation per RFC 8620 Section 5.2.
// @spec RFC8620#5.2-p1-MUST
type ChangeEntry struct {
	Action string // "create", "update", "destroy"
	ID     Id
	State  uint64 // the state token produced by this mutation
}

// ChangeTracker maintains a monotonically increasing state token and a bounded history of changes.
// @spec RFC8620#5.2-p2-MUST
type ChangeTracker struct {
	mu      sync.RWMutex
	counter uint64
	history []ChangeEntry
	maxKeep int
}

// NewChangeTracker initializes a change tracker retaining up to maxKeep entries before discarding the oldest.
func NewChangeTracker(maxKeep int) *ChangeTracker {
	if maxKeep <= 0 {
		maxKeep = 1000
	}
	return &ChangeTracker{
		history: make([]ChangeEntry, 0, 16),
		maxKeep: maxKeep,
	}
}

// State returns the current opaque state token.
func (t *ChangeTracker) State() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("~%d", t.counter)
}

// ParseNumericStateToken parses the numeric component of an opaque state token.
func ParseNumericStateToken(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, true
	}
	s = strings.TrimPrefix(s, "~")
	s = strings.TrimPrefix(s, "state-")
	var n uint64
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}
