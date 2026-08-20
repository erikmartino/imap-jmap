package memory

import (
	"context"
	"fmt"
	"imap-jmap/jmap"
	"sort"
	"strings"
	"time"
)

func (b *MemoryCalendarsBackend) ParticipantIdentityState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.identityState.State()
}

// ParticipantIdentityChanges returns created/updated/destroyed ParticipantIdentities since the given state.
func (b *MemoryCalendarsBackend) ParticipantIdentityChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.identityState.Changes(sinceState)
}

func (b *MemoryCalendarsBackend) GetParticipantIdentities(ctx context.Context, ids []jmap.Id) ([]*jmap.ParticipantIdentity, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.ParticipantIdentity
	var notFound []jmap.Id
	if len(ids) == 0 {
		for _, pi := range us.identities {
			list = append(list, pi)
		}
		return list, nil, nil
	}
	for _, id := range ids {
		if pi, ok := us.identities[id]; ok {
			list = append(list, pi)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllParticipantIdentities(ctx context.Context) ([]*jmap.ParticipantIdentity, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.ParticipantIdentity
	for _, pi := range us.identities {
		list = append(list, pi)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateParticipantIdentity(ctx context.Context, pi *jmap.ParticipantIdentity) (*jmap.ParticipantIdentity, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if pi.ID == "" {
		b.nextID++
		pi.ID = jmap.Id(fmt.Sprintf("pi-%d", b.nextID))
	}
	if pi.SendTo == nil {
		pi.SendTo = make(map[string]string)
	}
	if pi.IsDefault {
		for _, other := range us.identities {
			other.IsDefault = false
		}
	}
	us.identities[pi.ID] = pi
	b.recordChange(ctx, us.identityState, pi.ID, "create", "ParticipantIdentity")
	return pi, nil
}

func (b *MemoryCalendarsBackend) UpdateParticipantIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.ParticipantIdentity, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	pi, ok := us.identities[id]
	if !ok {
		return nil, fmt.Errorf("participant identity not found: %s", id)
	}
	if name, ok := patch["name"].(string); ok {
		pi.Name = name
	}
	if calAddr, ok := patch["calendarAddress"].(string); ok {
		pi.CalendarAddress = calAddr
	}
	if sendTo, ok := patch["sendTo"].(map[string]any); ok {
		converted := make(map[string]string, len(sendTo))
		for k, v := range sendTo {
			if s, ok := v.(string); ok {
				converted[k] = s
			}
		}
		pi.SendTo = converted
	}
	if isDefault, ok := patch["isDefault"].(bool); ok && isDefault {
		for _, other := range us.identities {
			if other.ID != pi.ID {
				other.IsDefault = false
			}
		}
		pi.IsDefault = true
	}
	b.recordChange(ctx, us.identityState, id, "update", "ParticipantIdentity")
	return pi, nil
}

func (b *MemoryCalendarsBackend) DeleteParticipantIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	pi, ok := us.identities[id]
	if !ok {
		return false, nil
	}
	wasDefault := pi.IsDefault
	delete(us.identities, id)
	// Keep the "SHOULD be exactly one default" invariant: if the default identity was
	// destroyed, promote another identity to be the default.
	if wasDefault {
		for _, other := range us.identities {
			other.IsDefault = true
			b.recordChange(ctx, us.identityState, other.ID, "update", "ParticipantIdentity")
			break
		}
	}
	b.recordChange(ctx, us.identityState, id, "destroy", "ParticipantIdentity")
	return true, nil
}

func (b *MemoryCalendarsBackend) SetDefaultParticipantIdentity(ctx context.Context, id jmap.Id) error {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.identities[id]; !ok {
		return fmt.Errorf("participant identity not found: %s", id)
	}
	for _, pi := range us.identities {
		want := pi.ID == id
		if pi.IsDefault != want {
			pi.IsDefault = want
			b.recordChange(ctx, us.identityState, pi.ID, "update", "ParticipantIdentity")
		}
	}
	return nil
}

// --- CalendarEventNotifications (draft-ietf-jmap-calendars Section 7) ---

// CalendarEventNotificationState returns the current change state token for notifications.
func (b *MemoryCalendarsBackend) CalendarEventNotificationState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.notificationState.State()
}

// CalendarEventNotificationChanges returns created/updated/destroyed notifications since the given state.
func (b *MemoryCalendarsBackend) CalendarEventNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	us := b.getStoreLocked(ctx)
	return us.notificationState.Changes(sinceState)
}

func (b *MemoryCalendarsBackend) GetCalendarEventNotifications(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEventNotification, []jmap.Id, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEventNotification
	var notFound []jmap.Id
	if len(ids) == 0 {
		for _, n := range us.notifications {
			list = append(list, n)
		}
		return list, nil, nil
	}
	for _, id := range ids {
		if n, ok := us.notifications[id]; ok {
			list = append(list, n)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryCalendarsBackend) GetAllCalendarEventNotifications(ctx context.Context) ([]*jmap.CalendarEventNotification, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEventNotification
	for _, n := range us.notifications {
		list = append(list, n)
	}
	return list, nil
}

func (b *MemoryCalendarsBackend) CreateCalendarEventNotification(ctx context.Context, n *jmap.CalendarEventNotification) (*jmap.CalendarEventNotification, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if n.ID == "" {
		b.nextID++
		n.ID = jmap.Id(fmt.Sprintf("not-%d", b.nextID))
	}
	if n.Created == "" {
		n.Created = time.Now().UTC().Format(time.RFC3339)
	}
	us.notifications[n.ID] = n
	us.notificationSeq[n.ID] = b.nextID
	b.recordChange(ctx, us.notificationState, n.ID, "create", "CalendarEventNotification")
	return n, nil
}

func (b *MemoryCalendarsBackend) DeleteCalendarEventNotification(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	us := b.getStoreLocked(ctx)
	defer b.mu.Unlock()

	if _, ok := us.notifications[id]; !ok {
		return false, nil
	}
	delete(us.notifications, id)
	delete(us.notificationSeq, id)
	b.recordChange(ctx, us.notificationState, id, "destroy", "CalendarEventNotification")
	return true, nil
}

// MatchCalendarEventNotification reports whether a notification satisfies a
// CalendarEventNotification filter condition per draft-ietf-jmap-calendars Section 7.6.1.
func MatchCalendarEventNotification(n *jmap.CalendarEventNotification, filter map[string]any) bool {
	for k, v := range filter {
		switch k {
		case "after":
			s, _ := v.(string)
			if s != "" && n.Created < s {
				return false
			}
		case "before":
			s, _ := v.(string)
			if s != "" && n.Created >= s {
				return false
			}
		case "type":
			s, _ := v.(string)
			if n.Type != s {
				return false
			}
		case "calendarEventIds":
			raw, _ := v.([]any)
			matched := false
			for _, item := range raw {
				if idStr, ok := item.(string); ok && jmap.Id(idStr) == n.CalendarEventID {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}

func (b *MemoryCalendarsBackend) QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, comparators []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	us := b.getStoreLocked(ctx)
	defer b.mu.RUnlock()

	var matched []*jmap.CalendarEventNotification
	for _, n := range us.notifications {
		if MatchCalendarEventNotification(n, filter) {
			matched = append(matched, n)
		}
	}

	// The "created" property is the only supported sort per Section 7.6.2; the default
	// order is newest first so clients always see the latest change at the top. When
	// created timestamps tie (second precision), the monotonic notification sequence
	// breaks the tie so the default order remains deterministic.
	sort.SliceStable(matched, func(i, j int) bool {
		for _, comp := range comparators {
			if comp.Property != "created" {
				continue
			}
			cmp := strings.Compare(matched[i].Created, matched[j].Created)
			if cmp != 0 {
				if comp.IsAscending {
					return cmp < 0
				}
				return cmp > 0
			}
		}
		if matched[i].Created != matched[j].Created {
			return matched[i].Created > matched[j].Created
		}
		return us.notificationSeq[matched[i].ID] > us.notificationSeq[matched[j].ID]
	})

	resultIDs := make([]jmap.Id, 0, len(matched))
	for _, n := range matched {
		resultIDs = append(resultIDs, n.ID)
	}
	total := len(resultIDs)
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
		ids = append(ids, resultIDs[i])
	}
	return ids, total, nil
}
