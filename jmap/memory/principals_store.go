package memory

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"imap-jmap/jmap"
)

// MemoryPrincipalsBackend manages in-memory storage for JMAP Principals & Availability per draft-ietf-jmap-principals.
type MemoryPrincipalsBackend struct {
	mu               sync.RWMutex
	principals       map[jmap.Id]*jmap.Principal
	state            uint64
	calendarsBackend jmap.CalendarsBackend
}

var _ jmap.PrincipalsBackend = (*MemoryPrincipalsBackend)(nil)

// NewMemoryPrincipalsBackend creates a new MemoryPrincipalsBackend seeded with default principal data.
func NewMemoryPrincipalsBackend() *MemoryPrincipalsBackend {
	b := &MemoryPrincipalsBackend{
		principals: make(map[jmap.Id]*jmap.Principal),
		state:      1,
	}

	// Seed default user principal
	defaultAccID := jmap.AccountIDForSubject("user@example.com")
	defaultPrincipal := &jmap.Principal{
		ID:                 "p-primary",
		Type:               "individual",
		Name:               "User Example",
		Description:        "Primary user principal",
		Email:              "user@example.com",
		CalendarAddress:    "mailto:user@example.com",
		MayGetAvailability: true,
		MayShareWith:       true,
		AccountIDs:         map[string]bool{defaultAccID: true},
	}
	b.principals[defaultPrincipal.ID] = defaultPrincipal

	return b
}

// SetCalendarsBackend sets the CalendarsBackend used to compute free-busy availability from calendar events.
func (b *MemoryPrincipalsBackend) SetCalendarsBackend(cb jmap.CalendarsBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calendarsBackend = cb
}

func (b *MemoryPrincipalsBackend) PrincipalState(ctx context.Context) string {
	return fmt.Sprintf("%d", atomic.LoadUint64(&b.state))
}

func (b *MemoryPrincipalsBackend) PrincipalChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	newState = b.PrincipalState(ctx)
	if sinceState == newState {
		return []jmap.Id{}, []jmap.Id{}, []jmap.Id{}, newState, false
	}
	// Return all principal IDs as created for simplicity on state mismatch
	for id := range b.principals {
		created = append(created, id)
	}
	return created, []jmap.Id{}, []jmap.Id{}, newState, false
}

func (b *MemoryPrincipalsBackend) GetPrincipals(ctx context.Context, ids []jmap.Id) (list []*jmap.Principal, notFound []jmap.Id, err error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, id := range ids {
		p, ok := b.principals[id]
		if ok {
			list = append(list, p)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryPrincipalsBackend) GetAllPrincipals(ctx context.Context) ([]*jmap.Principal, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	list := make([]*jmap.Principal, 0, len(b.principals))
	for _, p := range b.principals {
		list = append(list, p)
	}
	return list, nil
}

func (b *MemoryPrincipalsBackend) CreatePrincipal(ctx context.Context, p *jmap.Principal) (*jmap.Principal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if p.ID == "" {
		p.ID = jmap.Id(fmt.Sprintf("p-%d", len(b.principals)+1))
	}
	if p.Type == "" {
		p.Type = "individual"
	}

	b.principals[p.ID] = p
	atomic.AddUint64(&b.state, 1)
	return p, nil
}

func (b *MemoryPrincipalsBackend) UpdatePrincipal(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Principal, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	p, ok := b.principals[id]
	if !ok {
		return nil, fmt.Errorf("principal %q not found", id)
	}

	if name, ok := patch["name"].(string); ok {
		p.Name = name
	}
	if desc, ok := patch["description"].(string); ok {
		p.Description = desc
	}
	if email, ok := patch["email"].(string); ok {
		p.Email = email
	}
	if mayGet, ok := patch["mayGetAvailability"].(bool); ok {
		p.MayGetAvailability = mayGet
	}
	if mayShare, ok := patch["mayShareWith"].(bool); ok {
		p.MayShareWith = mayShare
	}

	atomic.AddUint64(&b.state, 1)
	return p, nil
}

func (b *MemoryPrincipalsBackend) DeletePrincipal(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.principals[id]; !ok {
		return false, nil
	}
	delete(b.principals, id)
	atomic.AddUint64(&b.state, 1)
	return true, nil
}

func (b *MemoryPrincipalsBackend) QueryPrincipals(ctx context.Context, filter map[string]any, position int, limit *uint64) (ids []jmap.Id, total int, err error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched []jmap.Id
	emailFilter, _ := filter["email"].(string)

	for id, p := range b.principals {
		if emailFilter != "" && p.Email != emailFilter {
			continue
		}
		matched = append(matched, id)
	}

	total = len(matched)
	if position >= total {
		return []jmap.Id{}, total, nil
	}
	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	return matched[position:end], total, nil
}

func (b *MemoryPrincipalsBackend) GetAvailability(ctx context.Context, principalID jmap.Id, utcStart, utcEnd string) ([]*jmap.AvailabilityWindow, error) {
	b.mu.RLock()
	cb := b.calendarsBackend
	b.mu.RUnlock()

	windows := make([]*jmap.AvailabilityWindow, 0)
	if cb == nil {
		return windows, nil
	}

	// Parse the query window; an absent bound is treated as open on that side.
	winStart, hasStart := parseRFC3339(utcStart)
	winEnd, hasEnd := parseRFC3339(utcEnd)

	events, err := cb.GetAllCalendarEvents(ctx)
	if err != nil {
		return windows, nil
	}
	for _, ev := range events {
		if ev == nil || ev.Start == "" {
			continue
		}
		// Secret events must behave as though they do not exist to other principals, so
		// they never contribute to shared free-busy. Cancelled events and events explicitly
		// marked "free" are not busy time either (draft-ietf-jmap-calendars availability).
		if ev.Privacy == "secret" || ev.Status == "cancelled" {
			continue
		}
		fb := ev.FreeBusyStatus
		if fb == "" {
			fb = "busy"
		}
		if fb == "free" {
			continue
		}
		// Expand recurrences and emit one busy window per instance that overlaps the query
		// window. End is start+duration (previously collapsed to start, yielding zero-length
		// windows), and recurring events now contribute every occurrence in range.
		for _, inst := range ExpandRecurrenceInstances(ev, winEnd) {
			if hasEnd && !inst.Start.Before(winEnd) {
				continue // occurrence starts at/after the window end
			}
			if hasStart && !inst.End.After(winStart) {
				continue // occurrence ends at/before the window start
			}
			windows = append(windows, &jmap.AvailabilityWindow{
				UTCStart:       inst.Start.UTC().Format(time.RFC3339),
				UTCEnd:         inst.End.UTC().Format(time.RFC3339),
				FreeBusyStatus: fb,
			})
		}
	}

	return windows, nil
}
