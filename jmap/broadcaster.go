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

// SubscriptionListener is notified when the subscriber count for an account changes.
type SubscriptionListener interface {
	OnSubscribe(accountID string)
	OnUnsubscribe(accountID string)
}

// Broadcaster manages active SSE subscriber channels per RFC 8620 Section 7.1 and push dispatchers.
type Broadcaster struct {
	mu            sync.RWMutex
	subscribers   map[chan *StateChange]string // ch -> accountID
	accountCounts map[string]int               // accountID -> count
	listeners     []StateChangeListener
	subListeners  []SubscriptionListener
}

// NewBroadcaster initializes a new Broadcaster instance.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers:   make(map[chan *StateChange]string),
		accountCounts: make(map[string]int),
	}
}

// AddListener registers a callback invoked whenever a StateChange is published.
func (b *Broadcaster) AddListener(l StateChangeListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, l)
}

// AddSubscriptionListener registers a listener for account subscribe/unsubscribe events.
func (b *Broadcaster) AddSubscriptionListener(sl SubscriptionListener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subListeners = append(b.subListeners, sl)
}

// HasSubscribersForAccount returns true if there is at least one active subscriber or listener.
func (b *Broadcaster) HasSubscribersForAccount(accountID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.listeners) > 0 {
		return true
	}
	if accountID == "" {
		return len(b.subscribers) > 0
	}
	return b.accountCounts[accountID] > 0 || b.accountCounts[""] > 0
}

// Subscribe registers a new subscriber channel, optionally associated with an account ID.
func (b *Broadcaster) Subscribe(accountID ...string) chan *StateChange {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan *StateChange, 10)
	var acct string
	if len(accountID) > 0 {
		acct = accountID[0]
	}
	b.subscribers[ch] = acct
	b.accountCounts[acct]++
	if b.accountCounts[acct] == 1 {
		for _, sl := range b.subListeners {
			go sl.OnSubscribe(acct)
		}
	}
	return ch
}

// Unsubscribe removes a subscriber channel.
func (b *Broadcaster) Unsubscribe(ch chan *StateChange) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if acct, ok := b.subscribers[ch]; ok {
		delete(b.subscribers, ch)
		close(ch)
		b.accountCounts[acct]--
		if b.accountCounts[acct] <= 0 {
			delete(b.accountCounts, acct)
			for _, sl := range b.subListeners {
				go sl.OnUnsubscribe(acct)
			}
		}
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
