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

// Record registers a change for the given id/action and returns the new state
// token to be assigned to the affected data type.
func (t *ChangeTracker) Record(id Id, action string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.counter++
	t.history = append(t.history, ChangeEntry{ID: id, Action: action, State: t.counter})
	if len(t.history) > t.maxKeep {
		t.history = append(t.history[:0], t.history[len(t.history)-t.maxKeep:]...)
	}
	return fmt.Sprintf("~%d", t.counter)
}

// Changes resolves mutations since the given state token into created, updated,
// and destroyed id lists per RFC 8620 Section 5.2. If the client state is older
// than the retained history, or if maxChanges is exceeded, hasMoreChanges is true.
func (t *ChangeTracker) Changes(sinceState string, maxChanges ...*uint64) (created, updated, destroyed []Id, newState string, hasMore bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	newState = fmt.Sprintf("~%d", t.counter)

	since, ok := ParseNumericStateToken(sinceState)
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
	for start < len(t.history) && t.history[start].State <= since {
		start++
	}
	if start == len(t.history) {
		return nil, nil, nil, newState, false
	}

	// SinceState is older than the oldest retained entry, so some mutations
	// between sinceState and the retained window cannot be reconstructed.
	if len(t.history) > 0 {
		earliest := t.history[0].State
		if since == 0 {
			hasMore = earliest > 1
		} else if earliest > since+1 {
			hasMore = true
		}
	}

	end := len(t.history)
	if len(maxChanges) > 0 && maxChanges[0] != nil {
		limit := int(*maxChanges[0])
		if end-start > limit {
			end = start + limit
			hasMore = true
			if end > 0 {
				newState = fmt.Sprintf("~%d", t.history[end-1].State)
			}
		}
	}

	// Resolve each id's final action within the window [start, end).
	first := make(map[Id]bool) // true if the id was created within the window
	last := make(map[Id]string)
	for i := start; i < end; i++ {
		e := t.history[i]
		if _, seen := last[e.ID]; !seen {
			first[e.ID] = e.Action == "create"
		}
		last[e.ID] = e.Action
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
