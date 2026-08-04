package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/foxcpp/go-sieve"

	"imap-jmap/jmap"
)

// MemorySieveBackend provides an in-memory implementation of jmap.SieveBackend per RFC 9610 / RFC 9661.
type userSieveStore struct {
	scripts map[jmap.Id]*jmap.SieveScript
	state   *changeTracker
}

type MemorySieveBackend struct {
	mu          sync.RWMutex
	users       map[string]*userSieveStore
	nextID      uint64
	broadcaster *jmap.Broadcaster
}

func (b *MemorySieveBackend) getStoreLocked(ctx context.Context) *userSieveStore {
	accountID := "primary"
	if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
		accountID = ctxID
	}

	us, ok := b.users[accountID]
	if !ok {
		us = &userSieveStore{
			scripts: make(map[jmap.Id]*jmap.SieveScript),
			state:   newChangeTracker(1000),
		}
		b.users[accountID] = us
	}
	return us
}

// Ensure MemorySieveBackend implements jmap.SieveBackend interface.
var _ jmap.SieveBackend = (*MemorySieveBackend)(nil)

// SetBroadcaster connects a Broadcaster so SieveScript mutations emit RFC 8620 Section 7.1
// StateChange push events.
func (b *MemorySieveBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

// NewMemorySieveBackend initializes a new MemorySieveBackend instance.
func NewMemorySieveBackend() *MemorySieveBackend {
	b := &MemorySieveBackend{
		users:  make(map[string]*userSieveStore),
		nextID: 0,
	}
	_ = b.getStoreLocked(context.Background())
	return b
}

// SieveScriptState returns the current change state token per RFC 8620.
func (b *MemorySieveBackend) SieveScriptState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.state.State()
}

// SieveScriptChanges returns created/updated/destroyed scripts since the given state per RFC 8620 Section 5.2.
func (b *MemorySieveBackend) SieveScriptChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.state.Changes(sinceState)
}

// recordChange records a mutation and publishes the new state token to push subscribers.
func (b *MemorySieveBackend) recordChange(ctx context.Context, tracker *changeTracker, id jmap.Id, action string) string {
	newState := tracker.record(id, action)
	if b.broadcaster != nil {
		accountID := "primary"
		if ctxID, ok := jmap.AccountIDFromContext(ctx); ok && ctxID != "" {
			accountID = ctxID
		}
		b.broadcaster.PublishStateChange(accountID, "SieveScript", newState)
	}
	return newState
}

func (b *MemorySieveBackend) ValidateSieveScript(ctx context.Context, content string) (bool, string) {
	if strings.TrimSpace(content) == "" {
		return false, "sieve script content is empty"
	}
	_, err := sieve.Load(strings.NewReader(content), sieve.DefaultOptions())
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (b *MemorySieveBackend) GetSieveScripts(ctx context.Context, ids []jmap.Id) ([]*jmap.SieveScript, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.SieveScript
	var notFound []jmap.Id

	if len(ids) == 0 {
		for _, s := range us.scripts {
			list = append(list, s)
		}
		return list, nil, nil
	}

	for _, id := range ids {
		if s, ok := us.scripts[id]; ok {
			list = append(list, s)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemorySieveBackend) GetAllSieveScripts(ctx context.Context) ([]*jmap.SieveScript, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.SieveScript
	for _, s := range us.scripts {
		list = append(list, s)
	}
	return list, nil
}

func (b *MemorySieveBackend) CreateSieveScript(ctx context.Context, script *jmap.SieveScript) (*jmap.SieveScript, error) {
	isValid, errDetail := b.ValidateSieveScript(ctx, script.Content)
	if !isValid {
		return nil, fmt.Errorf("invalid sieve script: %s", errDetail)
	}
	script.IsValid = true

	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if script.ID == "" {
		b.nextID++
		script.ID = jmap.Id(fmt.Sprintf("sieve-%d", b.nextID))
	}

	// If this script is active, deactivate all other scripts
	if script.IsActive {
		for _, s := range us.scripts {
			s.IsActive = false
		}
	}

	us.scripts[script.ID] = script
	b.recordChange(ctx, us.state, script.ID, "create")
	return script, nil
}

func (b *MemorySieveBackend) UpdateSieveScript(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.SieveScript, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	script, ok := us.scripts[id]
	if !ok {
		return nil, fmt.Errorf("sieve script %s: %w", id, jmap.ErrNotFound)
	}

	if content, ok := patch["content"].(string); ok {
		isValid, errDetail := b.ValidateSieveScript(ctx, content)
		if !isValid {
			return nil, fmt.Errorf("invalid sieve script: %s", errDetail)
		}
		script.Content = content
		script.IsValid = true
	}

	if name, ok := patch["name"].(string); ok {
		script.Name = name
	}

	if isActive, ok := patch["isActive"].(bool); ok {
		if isActive {
			for _, s := range us.scripts {
				s.IsActive = false
			}
		}
		script.IsActive = isActive
	}

	b.recordChange(ctx, us.state, id, "update")
	return script, nil
}

func (b *MemorySieveBackend) DeleteSieveScript(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.scripts[id]; !ok {
		return false, nil
	}
	delete(us.scripts, id)
	b.recordChange(ctx, us.state, id, "destroy")
	return true, nil
}

func (b *MemorySieveBackend) QuerySieveScripts(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.SieveScript

	nameFilter, _ := filter["name"].(string)
	isActiveFilter, hasActiveFilter := filter["isActive"].(bool)
	isValidFilter, hasValidFilter := filter["isValid"].(bool)

	for _, s := range us.scripts {
		if nameFilter != "" && !strings.Contains(strings.ToLower(s.Name), strings.ToLower(nameFilter)) {
			continue
		}
		if hasActiveFilter && s.IsActive != isActiveFilter {
			continue
		}
		if hasValidFilter && s.IsValid != isValidFilter {
			continue
		}
		matched = append(matched, s)
	}

	// Stable server-determined order (RFC 8620 Section 5.5: sort order MUST be stable
	// between calls to /query): name ascending, tie-broken by id. This keeps the indices
	// returned by /queryChanges deterministic.
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Name != matched[j].Name {
			return matched[i].Name < matched[j].Name
		}
		return matched[i].ID < matched[j].ID
	})

	total := len(matched)
	position = jmap.NormalizePosition(position, total)
	if position >= total {
		return []jmap.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmap.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, matched[i].ID)
	}

	return ids, total, nil
}
