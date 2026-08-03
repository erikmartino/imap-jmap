package memory

import (
	"fmt"
	"strings"
	"sync"

	"imap-jmap/jmap"
)

// changeEntry records a single state mutation so that /changes requests
// (RFC 8620 Section 5.2) can be answered.
type changeEntry struct {
	action string // "create", "update", "destroy"
	id     jmap.Id
	state  uint64 // the state token produced by this mutation
}

// changeTracker maintains a monotonically increasing state token and a bounded
// history of changes. Old history entries beyond maxKeep are discarded to keep
// memory bounded; /changes requests older than the retained window report
// hasMoreChanges so the client can re-fetch the full state.
type changeTracker struct {
	mu      sync.RWMutex
	counter uint64
	history []changeEntry
	maxKeep int
}

// newChangeTracker initializes a change tracker retaining up to maxKeep entries
// before discarding the oldest.
func newChangeTracker(maxKeep int) *changeTracker {
	if maxKeep <= 0 {
		maxKeep = 1000
	}
	return &changeTracker{
		history: make([]changeEntry, 0, 16),
		maxKeep: maxKeep,
	}
}

// State returns the current opaque state token.
func (t *changeTracker) State() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("~%d", t.counter)
}

// parseStateToken parses the numeric component of an opaque state token.
// Missing state tokens (empty or "0") are treated as the initial state.
func parseStateToken(s string) (uint64, bool) {
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

// record registers a change for the given id/action and returns the new state
// token to be assigned to the affected data type.
func (t *changeTracker) record(id jmap.Id, action string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counter++
	t.history = append(t.history, changeEntry{id: id, action: action, state: t.counter})
	if len(t.history) > t.maxKeep {
		t.history = append(t.history[:0], t.history[len(t.history)-t.maxKeep:]...)
	}
	return fmt.Sprintf("~%d", t.counter)
}

// Changes resolves mutations since the given state token into created, updated,
// and destroyed id lists per RFC 8620 Section 5.2. If the client state is older
// than the retained history, hasMoreChanges is true so the client can refetch.
func (t *changeTracker) Changes(sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	newState = fmt.Sprintf("~%d", t.counter)

	since, ok := parseStateToken(sinceState)
	if !ok {
		return nil, nil, nil, newState, true
	}
	if t.counter == 0 {
		if since == 0 {
			return nil, nil, nil, newState, false
		}
		return nil, nil, nil, newState, true
	}
	if since >= t.counter {
		return nil, nil, nil, newState, false
	}

	// Start from the first retained entry that is newer than sinceState.
	start := 0
	for start < len(t.history) && t.history[start].state <= since {
		start++
	}
	if start == len(t.history) {
		return nil, nil, nil, newState, false
	}

	// SinceState is older than the oldest retained entry, so some mutations
	// between sinceState and the retained window cannot be reconstructed.
	if len(t.history) > 0 {
		earliest := t.history[0].state
		if since == 0 {
			hasMore = earliest > 1
		} else if earliest > since+1 {
			hasMore = true
		}
	}

	// Resolve each id's final action within the window.
	first := make(map[jmap.Id]bool) // true if the id was created within the window
	last := make(map[jmap.Id]string)
	for i := start; i < len(t.history); i++ {
		e := t.history[i]
		if _, seen := last[e.id]; !seen {
			first[e.id] = e.action == "create"
		}
		last[e.id] = e.action
	}

	for id, action := range last {
		switch action {
		case "create", "update":
			if first[id] {
				created = append(created, id)
			} else {
				updated = append(updated, id)
			}
		case "destroy":
			if !first[id] {
				destroyed = append(destroyed, id)
			}
		}
	}

	return created, updated, destroyed, newState, hasMore
}
