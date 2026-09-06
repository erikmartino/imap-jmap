package nextcloud

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// PrincipalsBackend implements jmap.PrincipalsBackend backed by Nextcloud OCS Provisioning API.
type PrincipalsBackend struct {
	client          *Client
	calBackend      jmap.CalendarsBackend
	mu              sync.RWMutex
	principalsCache map[jmap.Id]*jmap.Principal
	tracker         *jmap.ChangeTracker
	broadcaster     *jmap.Broadcaster
}

var _ jmap.PrincipalsBackend = (*PrincipalsBackend)(nil)

// NewPrincipalsBackend initializes a Nextcloud PrincipalsBackend and seeds default groups and users in Nextcloud.
func NewPrincipalsBackend(client *Client, calBackend jmap.CalendarsBackend) *PrincipalsBackend {
	b := &PrincipalsBackend{
		client:          client,
		calBackend:      calBackend,
		principalsCache: make(map[jmap.Id]*jmap.Principal),
		tracker:         jmap.NewChangeTracker(1000),
	}

	initialUsers := []struct {
		userid, email, displayname string
	}{
		{"primary", "user@example.com", "User Example"},
		{"alice", "alice@example.com", "Alice Smith"},
		{"bob", "bob@example.com", "Bob Jones"},
		{"carol", "carol@example.com", "Carol Danvers"},
	}
	for _, u := range initialUsers {
		pid := jmap.Id("p-" + u.userid)
		b.principalsCache[pid] = &jmap.Principal{
			ID:                 pid,
			Type:               "individual",
			Name:               u.displayname,
			Email:              u.email,
			CalendarAddress:    "mailto:" + u.email,
			MayGetAvailability: true,
			MayShareWith:       true,
			AccountIDs:         map[string]bool{jmap.AccountIDForSubject(u.email): true},
		}
	}
	b.principalsCache["p-team"] = &jmap.Principal{
		ID:                 "p-team",
		Type:               "group",
		Name:               "Engineering Team",
		Email:              "team@example.com",
		CalendarAddress:    "mailto:team@example.com",
		MayGetAvailability: false,
		MayShareWith:       true,
		Members:            map[string]bool{"p-alice": true, "p-bob": true, "p-carol": true, "p-primary": true},
	}
	b.principalsCache["p-all"] = &jmap.Principal{
		ID:                 "p-all",
		Type:               "group",
		Name:               "All Staff",
		Email:              "all@example.com",
		CalendarAddress:    "mailto:all@example.com",
		MayGetAvailability: false,
		MayShareWith:       true,
		Members:            map[string]bool{"p-alice": true, "p-bob": true, "p-carol": true, "p-primary": true},
	}
	b.principalsCache["p-marketing"] = &jmap.Principal{
		ID:                 "p-marketing",
		Type:               "group",
		Name:               "Marketing",
		Email:              "marketing@example.com",
		CalendarAddress:    "mailto:marketing@example.com",
		MayGetAvailability: false,
		MayShareWith:       true,
		Members:            map[string]bool{"p-carol": true},
	}

	if !client.HasAdminAuth() {
		// Run without admin provisioning or seeding
		return b
	}

	// Asynchronously seed default Nextcloud groups and team members when admin credentials are provided
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Ensure groups exist in Nextcloud
		_ = client.EnsureGroup(ctx, "team")
		_ = client.EnsureGroup(ctx, "all")
		_ = client.EnsureGroup(ctx, "marketing")

		for _, u := range initialUsers {
			_ = client.EnsureUserInTeam(ctx, u.userid, u.email, u.email, u.displayname)
		}

		b.mu.Lock()
		b.refreshCacheLocked(ctx)
		b.mu.Unlock()
	}()

	return b
}

// SetCalendarsBackend sets the CalendarsBackend used for free/busy availability computation.
func (b *PrincipalsBackend) SetCalendarsBackend(cb jmap.CalendarsBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calBackend = cb
}

// SetBroadcaster sets the event broadcaster for state change notifications.
func (b *PrincipalsBackend) SetBroadcaster(broadcaster *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = broadcaster
}

func (b *PrincipalsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil && u != "" {
		accID := jmap.AccountIDForSubject(u)
		bc.PublishStateChange(accID, typeName, newState)
	}
}

// EnsureUser ensures a newly authenticated user exists in Nextcloud and is added to the "team" group.
func (b *PrincipalsBackend) EnsureUser(ctx context.Context, subject, password string) error {
	if subject == "" {
		return nil
	}
	email := subject
	userid := subject
	if parts := strings.Split(subject, "@"); len(parts) > 0 {
		userid = parts[0]
	}
	displayName := strings.Title(strings.ReplaceAll(userid, ".", " "))
	accID := jmap.AccountIDForSubject(email)
	pid := jmap.Id("p-" + userid)

	b.mu.Lock()
	_, existed := b.principalsCache[pid]
	b.principalsCache[pid] = &jmap.Principal{
		ID:                 pid,
		Type:               "individual",
		Name:               displayName,
		Email:              email,
		CalendarAddress:    "mailto:" + email,
		MayGetAvailability: true,
		MayShareWith:       true,
		AccountIDs:         map[string]bool{accID: true},
	}
	if teamGroup, ok := b.principalsCache["p-team"]; ok && teamGroup.Members != nil {
		teamGroup.Members[string(pid)] = true
	}
	if allGroup, ok := b.principalsCache["p-all"]; ok && allGroup.Members != nil {
		allGroup.Members[string(pid)] = true
	}
	var st string
	if existed {
		st = b.tracker.Record(pid, "update")
	} else {
		st = b.tracker.Record(pid, "create")
	}
	b.mu.Unlock()

	b.emitStateChange(subject, "Principal", st)

	if b.client.HasAdminAuth() {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = b.client.EnsureUserInTeam(bgCtx, userid, password, email, displayName)
		}()
	}
	return nil
}

func (b *PrincipalsBackend) refreshCacheLocked(ctx context.Context) {
	userIDs, err := b.client.GetUsers(ctx)
	if err != nil {
		return
	}

	newCache := make(map[jmap.Id]*jmap.Principal)

	// Users
	for _, uid := range userIDs {
		if uid == "admin" || uid == "cn" {
			continue
		}
		displayName := strings.Title(strings.ReplaceAll(uid, ".", " "))
		email := uid + "@example.com"
		if strings.Contains(uid, "@") {
			email = uid
		}
		details, dErr := b.client.GetUserDetails(ctx, uid)
		if dErr == nil && details != nil {
			if details.DisplayName != "" {
				displayName = details.DisplayName
			}
			if details.Email != "" {
				email = details.Email
			}
		}
		accID := jmap.AccountIDForSubject(email)
		pid := jmap.Id("p-" + uid)

		newCache[pid] = &jmap.Principal{
			ID:                 pid,
			Type:               "individual",
			Name:               displayName,
			Email:              email,
			CalendarAddress:    "mailto:" + email,
			MayGetAvailability: true,
			MayShareWith:       true,
			AccountIDs:         map[string]bool{accID: true},
		}
	}

	// Groups
	groupIDs, gErr := b.client.GetGroups(ctx)
	if gErr == nil {
		for _, gid := range groupIDs {
			if gid == "admin" {
				continue
			}
			members, _ := b.client.GetGroupMembers(ctx, gid)
			membersMap := make(map[string]bool)
			for _, m := range members {
				membersMap["p-"+m] = true
			}

			groupName := gid
			switch strings.ToLower(gid) {
			case "team":
				groupName = "Engineering Team"
			case "all":
				groupName = "All Staff"
			case "marketing":
				groupName = "Marketing Group"
			default:
				groupName = strings.Title(strings.ReplaceAll(gid, "-", " "))
			}

			pid := jmap.Id("p-" + gid)
			newCache[pid] = &jmap.Principal{
				ID:                 pid,
				Type:               "group",
				Name:               groupName,
				Email:              gid + "@example.com",
				Description:        groupName + " in Nextcloud",
				CalendarAddress:    "mailto:" + gid + "@example.com",
				Members:            membersMap,
				MayGetAvailability: true,
				MayShareWith:       true,
			}
		}
	}

	if _, ok := newCache["p-team"]; !ok {
		newCache["p-team"] = &jmap.Principal{
			ID:                 "p-team",
			Type:               "group",
			Name:               "Engineering Team",
			Email:              "team@example.com",
			CalendarAddress:    "mailto:team@example.com",
			MayGetAvailability: false,
			MayShareWith:       true,
			Members:            map[string]bool{"p-alice": true, "p-bob": true, "p-carol": true, "p-primary": true},
		}
	}
	if _, ok := newCache["p-all"]; !ok {
		newCache["p-all"] = &jmap.Principal{
			ID:                 "p-all",
			Type:               "group",
			Name:               "All Staff",
			Email:              "all@example.com",
			CalendarAddress:    "mailto:all@example.com",
			MayGetAvailability: false,
			MayShareWith:       true,
			Members:            map[string]bool{"p-alice": true, "p-bob": true, "p-carol": true, "p-primary": true},
		}
	}
	if _, ok := newCache["p-marketing"]; !ok {
		newCache["p-marketing"] = &jmap.Principal{
			ID:                 "p-marketing",
			Type:               "group",
			Name:               "Marketing Group",
			Email:              "marketing@example.com",
			CalendarAddress:    "mailto:marketing@example.com",
			MayGetAvailability: false,
			MayShareWith:       true,
			Members:            map[string]bool{"p-carol": true},
		}
	}

	b.principalsCache = newCache
}

func (b *PrincipalsBackend) ensureCurrentPrincipal(ctx context.Context) {
	subj, ok := jmap.SubjectFromContext(ctx)
	if !ok || subj == "" {
		return
	}
	if !b.client.HasAdminAuth() {
		b.mu.Lock()
		defer b.mu.Unlock()
		email := subj
		userid := subj
		if parts := strings.Split(subj, "@"); len(parts) > 0 {
			userid = parts[0]
		}
		accID := jmap.AccountIDForSubject(email)
		for _, existing := range b.principalsCache {
			if existing.Email == email || existing.AccountIDs[accID] {
				return
			}
		}
		pid := jmap.Id("p-" + userid)
		if userid == "user" {
			pid = "p-primary"
		}
		if _, exists := b.principalsCache[pid]; !exists {
			displayName := strings.Title(strings.ReplaceAll(userid, ".", " "))
			b.principalsCache[pid] = &jmap.Principal{
				ID:                 pid,
				Type:               "individual",
				Name:               displayName,
				Email:              email,
				CalendarAddress:    "mailto:" + email,
				MayGetAvailability: true,
				MayShareWith:       true,
				AccountIDs:         map[string]bool{accID: true},
			}
		}
		return
	}
	creds, _ := jmap.CredentialsFromContext(ctx)
	pass := creds.Password
	if pass == "" {
		pass = subj
	}
	_ = b.EnsureUser(ctx, subj, pass)
}

func (b *PrincipalsBackend) PrincipalState(ctx context.Context) string {
	return b.tracker.State()
}

func (b *PrincipalsBackend) PrincipalChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	return b.tracker.Changes(sinceState)
}

func (b *PrincipalsBackend) GetPrincipals(ctx context.Context, ids []jmap.Id) ([]*jmap.Principal, []jmap.Id, error) {
	if len(ids) == 0 {
		list, err := b.GetAllPrincipals(ctx)
		return list, []jmap.Id{}, err
	}

	b.mu.Lock()
	if len(b.principalsCache) == 0 {
		b.refreshCacheLocked(ctx)
	}
	b.mu.Unlock()

	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.Principal
	var notFound []jmap.Id
	for _, id := range ids {
		p, ok := b.principalsCache[id]
		if !ok {
			// Try matching without p- prefix or by email
			for _, item := range b.principalsCache {
				if item.Email == string(id) || item.ID == "p-"+id {
					p = item
					ok = true
					break
				}
			}
		}
		if ok {
			list = append(list, p)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *PrincipalsBackend) GetAllPrincipals(ctx context.Context) ([]*jmap.Principal, error) {
	b.ensureCurrentPrincipal(ctx)

	b.mu.Lock()
	if len(b.principalsCache) == 0 {
		b.refreshCacheLocked(ctx)
	}
	b.mu.Unlock()

	b.mu.RLock()
	defer b.mu.RUnlock()

	list := make([]*jmap.Principal, 0, len(b.principalsCache))
	for _, p := range b.principalsCache {
		list = append(list, p)
	}
	return list, nil
}

func (b *PrincipalsBackend) QueryPrincipals(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.ensureCurrentPrincipal(ctx)

	b.mu.Lock()
	if len(b.principalsCache) == 0 {
		b.refreshCacheLocked(ctx)
	}
	b.mu.Unlock()

	b.mu.RLock()
	defer b.mu.RUnlock()

	var matched []jmap.Id
	for id, p := range b.principalsCache {
		if !jmap.MatchPrincipal(p, filter) {
			continue
		}
		matched = append(matched, id)
	}

	sort.Slice(matched, func(i, j int) bool {
		return string(matched[i]) < string(matched[j])
	})

	total := len(matched)
	if position >= total {
		return []jmap.Id{}, total, nil
	}
	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}
	return matched[position:end], total, nil
}

func (b *PrincipalsBackend) CreatePrincipal(ctx context.Context, p *jmap.Principal) (*jmap.Principal, error) {
	if p == nil {
		return nil, fmt.Errorf("principal is nil")
	}
	if p.ID == "" {
		p.ID = jmap.Id(fmt.Sprintf("p-%d", time.Now().UnixNano()))
	}
	if p.Type == "" {
		p.Type = "individual"
	}
	if p.Type == "group" {
		_ = b.client.CreateGroup(ctx, p.Name)
	} else {
		_ = b.client.CreateUser(ctx, string(p.ID), p.Email, p.Email, p.Name)
	}

	b.mu.Lock()
	if b.principalsCache == nil {
		b.principalsCache = make(map[jmap.Id]*jmap.Principal)
	}
	b.principalsCache[p.ID] = p
	st := b.tracker.Record(p.ID, "create")
	u := p.Email
	b.mu.Unlock()

	b.emitStateChange(u, "Principal", st)
	return p, nil
}

func (b *PrincipalsBackend) UpdatePrincipal(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Principal, error) {
	b.mu.Lock()

	p, ok := b.principalsCache[id]
	if !ok {
		b.mu.Unlock()
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

	st := b.tracker.Record(id, "update")
	u := p.Email
	b.mu.Unlock()

	b.emitStateChange(u, "Principal", st)
	return p, nil
}

func (b *PrincipalsBackend) DeletePrincipal(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	delete(b.principalsCache, id)
	b.tracker.Record(id, "destroy")
	b.mu.Unlock()
	return true, nil
}

func (b *PrincipalsBackend) GetAvailability(ctx context.Context, principalID jmap.Id, utcStart, utcEnd string) ([]*jmap.AvailabilityWindow, error) {
	b.mu.RLock()
	cb := b.calBackend
	p := b.principalsCache[principalID]
	b.mu.RUnlock()

	windows := make([]*jmap.AvailabilityWindow, 0)
	if cb == nil {
		return windows, nil
	}

	winStart, hasStart := jmap.ParseRFC3339(utcStart)
	winEnd, hasEnd := jmap.ParseRFC3339(utcEnd)

	var contexts []context.Context
	if p != nil && len(p.AccountIDs) > 0 {
		for accID := range p.AccountIDs {
			contexts = append(contexts, jmap.ContextWithAccountID(ctx, accID))
		}
	} else {
		contexts = append(contexts, ctx)
	}

	for _, pCtx := range contexts {
		cals, err := cb.GetAllCalendars(pCtx)
		if err != nil {
			continue
		}
		calInAvail := make(map[jmap.Id]string, len(cals))
		for _, cal := range cals {
			calInAvail[cal.ID] = cal.IncludeInAvailability
		}

		events, err := cb.GetAllCalendarEvents(pCtx)
		if err != nil {
			continue
		}
		for _, ev := range events {
			if ev == nil || ev.Start == "" {
				continue
			}
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

			if len(ev.CalendarIDs) > 0 {
				included := false
				for calID := range ev.CalendarIDs {
					incSetting := calInAvail[calID]
					if incSetting == "none" {
						continue
					}
					if incSetting == "attending" {
						if p != nil && isPrincipalAttending(ev, p) {
							included = true
							break
						}
						continue
					}
					included = true
					break
				}
				if !included {
					continue
				}
			}

			for _, inst := range jmap.ExpandRecurrenceInstances(ev, winEnd) {
				if hasEnd && !inst.Start.Before(winEnd) {
					continue
				}
				if hasStart && !inst.End.After(winStart) {
					continue
				}
				windows = append(windows, &jmap.AvailabilityWindow{
					UTCStart:       inst.Start.UTC().Format(time.RFC3339),
					UTCEnd:         inst.End.UTC().Format(time.RFC3339),
					FreeBusyStatus: fb,
				})
			}
		}
	}

	return windows, nil
}

func isPrincipalAttending(ev *jmap.CalendarEvent, p *jmap.Principal) bool {
	if ev == nil || p == nil {
		return false
	}
	for _, part := range ev.Participants {
		if part == nil {
			continue
		}
		matches := false
		if p.Email != "" && strings.EqualFold(part.Email, p.Email) {
			matches = true
		}
		if p.CalendarAddress != "" {
			if strings.EqualFold(part.SendTo["imip"], p.CalendarAddress) || strings.EqualFold(part.Email, strings.TrimPrefix(p.CalendarAddress, "mailto:")) {
				matches = true
			}
		}
		if p.Name != "" && strings.EqualFold(part.Name, p.Name) {
			matches = true
		}
		if matches {
			if part.ParticipationStatus == "accepted" || part.ParticipationStatus == "attending" {
				return true
			}
			if part.Roles != nil && (part.Roles["chair"] || part.Roles["organizer"]) {
				return true
			}
		}
	}
	return false
}
