package jmap

import (
	"sync"
)

// StateChange represents an RFC 8620 Section 7.1 StateChange event payload.
type StateChange struct {
	Type    string                       `json:"@type"`
	Changed map[string]map[string]string `json:"changed"` // accountID -> typeName -> stateToken
}

// StateChangeListener is called whenever a StateChange is published.
type StateChangeListener func(accountID, typeName, newState string)

// Broadcaster manages active SSE subscriber channels per RFC 8620 Section 7.1 and push dispatchers.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan *StateChange]struct{}
	listeners   []StateChangeListener
}

// NewBroadcaster initializes a new Broadcaster instance.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[chan *StateChange]struct{}),
	}
}

// AddListener registers a callback invoked whenever a StateChange is published.
func (b *Broadcaster) AddListener(l StateChangeListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, l)
}

// Subscribe registers a new subscriber channel.
func (b *Broadcaster) Subscribe() chan *StateChange {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan *StateChange, 10)
	b.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel.
func (b *Broadcaster) Unsubscribe(ch chan *StateChange) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
	}
}

// PublishStateChange broadcasts a StateChange event to all active subscribers.
func (b *Broadcaster) PublishStateChange(accountID string, typeName string, newState string) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	event := &StateChange{
		Type: "StateChange",
		Changed: map[string]map[string]string{
			accountID: {
				typeName: newState,
			},
		},
	}

	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			// Buffer full, drop non-blocking
		}
	}

	for _, l := range b.listeners {
		go l(accountID, typeName, newState)
	}
}
