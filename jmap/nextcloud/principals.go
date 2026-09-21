package nextcloud

import (
	"context"
	"fmt"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcalendar"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapprincipals"
	"imap-jmap/jmap/jmappush"
)

// PrincipalsBackend implements jmapprincipals.PrincipalsBackend backed by Nextcloud OCS Provisioning API.
type PrincipalsBackend struct {
	client          *Client
	calBackend      jmapcalendar.CalendarsBackend
	mu              sync.RWMutex
	refreshMu       sync.Mutex
	principalsCache map[jmapcore.Id]*jmapprincipals.Principal
	tracker         *jmappush.ChangeTracker
	broadcaster     *jmappush.Broadcaster
	// directoryCache holds per-user, on-demand principal discovery results (from the
	// Nextcloud sharee search API, using that user's own credentials). It is keyed by
	// subject because visibility is per-user; it is never populated from a background
	// job and never from another user's session.
	directoryCache map[string]*principalDirectory
}

// principalDirectory is a short-lived, per-user view of the principals a user may
// share with, discovered on demand from the upstream sharee search API.
type principalDirectory struct {
	syncedAt   time.Time
	principals map[jmapcore.Id]*jmapprincipals.Principal
}

// principalsDirectoryTTL bounds how long a per-user directory is reused before it is
// refreshed with the user's own credentials.
const principalsDirectoryTTL = 5 * time.Minute

var _ jmapprincipals.PrincipalsBackend = (*PrincipalsBackend)(nil)

// NewPrincipalsBackend initializes a Nextcloud PrincipalsBackend without hardcoded accounts.
func NewPrincipalsBackend(client *Client, calBackend jmapcalendar.CalendarsBackend) *PrincipalsBackend {
	return &PrincipalsBackend{
		client:          client,
		calBackend:      calBackend,
		principalsCache: make(map[jmapcore.Id]*jmapprincipals.Principal),
		tracker:         jmappush.NewChangeTracker(1000),
		directoryCache:  make(map[string]*principalDirectory),
	}
}

// SetCalendarsBackend sets the CalendarsBackend used for free/busy availability computation.
func (b *PrincipalsBackend) SetCalendarsBackend(cb jmapcalendar.CalendarsBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calBackend = cb
}

// SeedUser seeds an individual user principal into the cache.
func (b *PrincipalsBackend) SeedUser(id jmapcore.Id, email, displayName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	acctID := jmapauth.AccountIDForSubject(email)
	if id == "" {
		id = jmapcore.Id("p-" + email)
	}
	for existingID, p := range b.principalsCache {
		if p != nil && strings.EqualFold(p.Email, email) {
			if displayName != "" {
				p.Name = displayName
			}
			if existingID != id {
				delete(b.principalsCache, existingID)
				p.ID = id
				b.principalsCache[id] = p
			}
			return
		}
	}
	b.principalsCache[id] = &jmapprincipals.Principal{
		ID:                 id,
		Type:               "individual",
		Name:               displayName,
		Email:              email,
		CalendarAddress:    "mailto:" + email,
		MayGetAvailability: true,
		MayShareWith:       true,
		AccountIDs:         map[string]bool{acctID: true},
	}
}

// SeedPrincipal seeds a principal directly into the cache.
func (b *PrincipalsBackend) SeedPrincipal(p *jmapprincipals.Principal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p != nil && p.ID != "" {
		b.principalsCache[p.ID] = p
	}
}

// SetBroadcaster sets the event broadcaster for state change notifications.
func (b *PrincipalsBackend) SetBroadcaster(broadcaster *jmappush.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = broadcaster
}

func (b *PrincipalsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil && u != "" {
		accID := jmapauth.AccountIDForSubject(u)
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
	displayName := subject
	accID := jmapauth.AccountIDForSubject(email)
	pid := jmapcore.Id("p-" + userid)

	b.mu.Lock()
	_, existed := b.principalsCache[pid]
	b.principalsCache[pid] = &jmapprincipals.Principal{
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

	if b.client != nil {
		_ = b.client.EnsureUserInTeam(ctx, userid, password, email, displayName)
	}
	return nil
}

func (b *PrincipalsBackend) ensureCurrentPrincipal(ctx context.Context) {
	subj, ok := jmapauth.SubjectFromContext(ctx)
	if !ok || subj == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	email := subj
	userid := subj
	accID := jmapauth.AccountIDForSubject(email)
	for _, existing := range b.principalsCache {
		if existing.Email == email || existing.AccountIDs[accID] {
			return
		}
	}
	pid := jmapcore.Id("p-" + userid)
	if _, exists := b.principalsCache[pid]; !exists {
		displayName := userid
		b.principalsCache[pid] = &jmapprincipals.Principal{
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
}

// domainFromContext extracts the domain name from the authenticated user subject,
// account ID, or Nextcloud client base URL. Returns empty string if no domain is known.
func domainFromContext(ctx context.Context, client *Client) string {
	if ctx != nil {
		if subj, ok := jmapauth.SubjectFromContext(ctx); ok && subj != "" {
			if parts := strings.Split(subj, "@"); len(parts) == 2 && parts[1] != "" {
				return strings.ToLower(parts[1])
			}
		}
		if accID, ok := jmapauth.AccountIDFromContext(ctx); ok && accID != "" {
			if subj, okSub := jmapauth.SubjectForAccountID(accID); okSub && strings.Contains(subj, "@") {
				parts := strings.Split(subj, "@")
				return strings.ToLower(parts[len(parts)-1])
			}
		}
		if creds, okCreds := jmapauth.CredentialsFromContext(ctx); okCreds && strings.Contains(creds.Username, "@") {
			parts := strings.Split(creds.Username, "@")
			return strings.ToLower(parts[len(parts)-1])
		}
	}
	if client != nil && client.BaseURL != "" {
		if u, err := url.Parse(client.BaseURL); err == nil {
			host := u.Hostname()
			if host != "" && host != "localhost" && host != "127.0.0.1" {
				return strings.ToLower(host)
			}
		}
	}
	return ""
}

func safeGroupEmailAndCalendarAddress(ctx context.Context, client *Client, gid string) (string, string) {
	// If gid is already an email address, validate it
	if strings.Contains(gid, "@") {
		if addr, err := mail.ParseAddress(gid); err == nil && addr != nil {
			cleanAddr := strings.ReplaceAll(strings.ReplaceAll(addr.Address, "\r", ""), "\n", "")
			return cleanAddr, "mailto:" + cleanAddr
		}
	}
	// Discover domain dynamically from authenticated user / server context
	domain := domainFromContext(ctx, client)
	if domain == "" {
		// Do not fabricate a fake domain when none is known; in JMAP draft-ietf-jmap-principals,
		// email and calendarAddress are optional (omitted/null) when the group has no email address.
		return "", ""
	}
	// Sanitize local part: only alphanumeric, hyphen, underscore, dot
	safeLocal := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, gid)
	safeLocal = strings.Trim(safeLocal, ".-")
	if safeLocal == "" {
		safeLocal = "group"
	}
	email := safeLocal + "@" + domain
	return email, "mailto:" + email
}

func sanitizeDisplayName(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", " ")
	return strings.TrimSpace(name)
}

// collectGroupMembership queries Nextcloud for groups and their members using the user's credentials.
// It is a fallback used only when the sharee search API is unavailable (e.g. the embedded
// reference server); callers already hold refreshMu.
// It reports whether the group listing was successfully retrieved.
func (b *PrincipalsBackend) collectGroupMembership(ctx context.Context) bool {
	if b.client == nil {
		return false
	}

	groups, err := b.client.GetGroups(ctx)
	if err != nil {
		return false
	}

	for _, gid := range groups {
		if gid == "admin" || !IsValidGroupID(gid) {
			continue
		}
		members, err := b.client.GetGroupMembers(ctx, gid)
		if err != nil {
			continue
		}
		membersMap := make(map[string]bool, len(members))
		for _, m := range members {
			if IsValidUserID(m) {
				membersMap["p-"+m] = true
			}
		}

		pid := jmapcore.Id("p-" + gid)
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
		groupName = sanitizeDisplayName(groupName)
		email, calAddr := safeGroupEmailAndCalendarAddress(ctx, b.client, gid)

		b.mu.Lock()
		if existing, ok := b.principalsCache[pid]; ok {
			if existing.Members == nil {
				existing.Members = make(map[string]bool)
			}
			for m := range membersMap {
				existing.Members[m] = true
			}
		} else {
			b.principalsCache[pid] = &jmapprincipals.Principal{
				ID:                 pid,
				Type:               "group",
				Name:               groupName,
				Email:              email,
				Description:        groupName + " in Nextcloud",
				CalendarAddress:    calAddr,
				Members:            membersMap,
				MayGetAvailability: true,
				MayShareWith:       true,
			}
		}
		b.mu.Unlock()
	}
	return true
}

// ensureUserDirectory returns the on-demand principal directory for the user in the
// request context, refreshing it from the upstream sharee search API at most once per
// TTL. There is deliberately no background/global directory: without admin credentials
// the only way to enumerate share targets is with the caller's own session, exactly as
// the Nextcloud web UI does.
func (b *PrincipalsBackend) ensureUserDirectory(ctx context.Context) map[jmapcore.Id]*jmapprincipals.Principal {
	subj, ok := jmapauth.SubjectFromContext(ctx)
	if !ok || subj == "" || b.client == nil {
		return nil
	}

	b.mu.RLock()
	dir := b.directoryCache[subj]
	b.mu.RUnlock()
	if dir != nil && time.Since(dir.syncedAt) < principalsDirectoryTTL {
		return dir.principals
	}

	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	b.mu.RLock()
	dir = b.directoryCache[subj]
	b.mu.RUnlock()
	if dir != nil && time.Since(dir.syncedAt) < principalsDirectoryTTL {
		return dir.principals
	}

	principals := b.buildUserDirectory(ctx, subj)
	b.mu.Lock()
	b.directoryCache[subj] = &principalDirectory{syncedAt: time.Now(), principals: principals}
	b.mu.Unlock()
	return principals
}

// buildUserDirectory maps the caller's visible Nextcloud share targets to JMAP
// principals. If the sharee search API is unavailable (e.g. the embedded test server),
// it falls back to the legacy OCS group collection, which populates the global cache.
func (b *PrincipalsBackend) buildUserDirectory(ctx context.Context, subj string) map[jmapcore.Id]*jmapprincipals.Principal {
	sharees, err := b.client.GetSharees(ctx, "")
	if err != nil {
		b.collectGroupMembership(ctx)
		return nil
	}
	domain := domainFromContext(ctx, b.client)
	out := make(map[jmapcore.Id]*jmapprincipals.Principal, len(sharees))
	for _, s := range sharees {
		if s.ShareType == 1 {
			if strings.EqualFold(s.ID, "admin") || !IsValidGroupID(s.ID) {
				continue
			}
			pid := jmapcore.Id("p-" + s.ID)
			name := groupDisplayName(s.ID, s.Label)
			email, calAddr := safeGroupEmailAndCalendarAddress(ctx, b.client, s.ID)
			out[pid] = &jmapprincipals.Principal{
				ID:                 pid,
				Type:               "group",
				Name:               name,
				Email:              email,
				Description:        name + " in Nextcloud",
				CalendarAddress:    calAddr,
				MayGetAvailability: true,
				MayShareWith:       true,
			}
			continue
		}
		pid := jmapcore.Id("p-" + s.ID)
		email := s.ID
		if !strings.Contains(email, "@") && domain != "" {
			email = s.ID + "@" + domain
		}
		name := sanitizeDisplayName(s.Label)
		if name == "" {
			name = s.ID
		}
		out[pid] = &jmapprincipals.Principal{
			ID:                 pid,
			Type:               "individual",
			Name:               name,
			Email:              email,
			CalendarAddress:    "mailto:" + email,
			MayGetAvailability: true,
			MayShareWith:       true,
			AccountIDs:         map[string]bool{jmapauth.AccountIDForSubject(email): true},
		}
	}
	return out
}

// groupDisplayName maps a Nextcloud group id to its user-facing name, preferring the
// sharee label when present.
func groupDisplayName(gid, label string) string {
	switch strings.ToLower(gid) {
	case "team":
		return "Engineering Team"
	case "all":
		return "All Staff"
	case "marketing":
		return "Marketing Group"
	}
	if label != "" {
		return sanitizeDisplayName(label)
	}
	return sanitizeDisplayName(strings.Title(strings.ReplaceAll(gid, "-", " ")))
}

func (b *PrincipalsBackend) PrincipalState(ctx context.Context) string {
	return b.tracker.State()
}

func (b *PrincipalsBackend) PrincipalChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool) {
	return b.tracker.Changes(sinceState)
}

func (b *PrincipalsBackend) GetPrincipals(ctx context.Context, ids []jmapcore.Id) ([]*jmapprincipals.Principal, []jmapcore.Id, error) {
	if len(ids) == 0 {
		list, err := b.GetAllPrincipals(ctx)
		return list, []jmapcore.Id{}, err
	}

	b.ensureCurrentPrincipal(ctx)
	merged := b.mergedPrincipals(ctx)

	var list []*jmapprincipals.Principal
	var notFound []jmapcore.Id
	for _, id := range ids {
		p, ok := merged[id]
		if !ok {
			// Try matching without p- prefix or by email
			for _, item := range merged {
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

// mergedPrincipals returns the principals visible to the requesting user: the global
// known-principals cache (self and users who have authenticated) plus that user's
// on-demand directory discovered with their own credentials.
func (b *PrincipalsBackend) mergedPrincipals(ctx context.Context) map[jmapcore.Id]*jmapprincipals.Principal {
	directory := b.ensureUserDirectory(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()
	merged := make(map[jmapcore.Id]*jmapprincipals.Principal, len(b.principalsCache)+len(directory))
	for id, p := range b.principalsCache {
		merged[id] = p
	}
	for id, p := range directory {
		if _, ok := merged[id]; !ok {
			merged[id] = p
		}
	}
	return merged
}

func (b *PrincipalsBackend) GetAllPrincipals(ctx context.Context) ([]*jmapprincipals.Principal, error) {
	b.ensureCurrentPrincipal(ctx)
	merged := b.mergedPrincipals(ctx)
	list := make([]*jmapprincipals.Principal, 0, len(merged))
	for _, p := range merged {
		list = append(list, p)
	}
	return list, nil
}

func (b *PrincipalsBackend) QueryPrincipals(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	b.ensureCurrentPrincipal(ctx)
	merged := b.mergedPrincipals(ctx)

	var matched []jmapcore.Id
	for id, p := range merged {
		if !jmapprincipals.MatchPrincipal(p, filter) {
			continue
		}
		matched = append(matched, id)
	}

	sort.Slice(matched, func(i, j int) bool {
		return string(matched[i]) < string(matched[j])
	})

	total := len(matched)
	if position >= total {
		return []jmapcore.Id{}, total, nil
	}
	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}
	return matched[position:end], total, nil
}

func (b *PrincipalsBackend) CreatePrincipal(ctx context.Context, p *jmapprincipals.Principal) (*jmapprincipals.Principal, error) {
	if p == nil {
		return nil, fmt.Errorf("principal is nil")
	}
	if p.ID == "" {
		p.ID = jmapcore.Id(fmt.Sprintf("p-%d", time.Now().UnixNano()))
	}
	if p.Type == "" {
		p.Type = "individual"
	}
	if p.Type == "group" {
		groupName := p.Name
		if groupName == "" {
			groupName = string(p.ID)
		}
		if !IsValidGroupID(groupName) {
			return nil, fmt.Errorf("invalid group name %q: contains prohibited characters or injection sequence", groupName)
		}
		p.Name = sanitizeDisplayName(groupName)
		if p.Email == "" {
			email, calAddr := safeGroupEmailAndCalendarAddress(ctx, b.client, groupName)
			p.Email = email
			p.CalendarAddress = calAddr
		}
	}
	if b.client != nil {
		if p.Type == "group" {
			_ = b.client.CreateGroup(ctx, p.Name)
		} else {
			_ = b.client.CreateUser(ctx, string(p.ID), p.Email, p.Email, p.Name)
		}
	}

	b.mu.Lock()
	if b.principalsCache == nil {
		b.principalsCache = make(map[jmapcore.Id]*jmapprincipals.Principal)
	}
	b.principalsCache[p.ID] = p
	st := b.tracker.Record(p.ID, "create")
	u := p.Email
	b.mu.Unlock()

	b.emitStateChange(u, "Principal", st)
	return p, nil
}

func (b *PrincipalsBackend) UpdatePrincipal(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapprincipals.Principal, error) {
	b.mu.Lock()

	p, ok := b.principalsCache[id]
	if !ok {
		b.mu.Unlock()
		return nil, fmt.Errorf("principal %q not found", id)
	}

	if name, ok := patch["name"].(string); ok {
		if p.Type == "group" && !IsValidGroupID(name) {
			b.mu.Unlock()
			return nil, fmt.Errorf("invalid group name %q: contains prohibited characters or injection sequence", name)
		}
		p.Name = sanitizeDisplayName(name)
	}
	if desc, ok := patch["description"].(string); ok {
		p.Description = sanitizeDisplayName(desc)
	}
	if email, ok := patch["email"].(string); ok {
		if strings.ContainsAny(email, "\r\n") {
			b.mu.Unlock()
			return nil, fmt.Errorf("invalid email address: contains CRLF")
		}
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

func (b *PrincipalsBackend) DeletePrincipal(ctx context.Context, id jmapcore.Id) (bool, error) {
	b.mu.Lock()
	delete(b.principalsCache, id)
	b.tracker.Record(id, "destroy")
	b.mu.Unlock()
	return true, nil
}

func (b *PrincipalsBackend) GetAvailability(ctx context.Context, principalID jmapcore.Id, utcStart, utcEnd string) ([]*jmapprincipals.AvailabilityWindow, error) {
	b.mu.RLock()
	cb := b.calBackend
	p := b.principalsCache[principalID]
	b.mu.RUnlock()

	windows := make([]*jmapprincipals.AvailabilityWindow, 0)
	if cb == nil {
		return windows, nil
	}

	winStart, hasStart := jmapcalendar.ParseRFC3339(utcStart)
	winEnd, hasEnd := jmapcalendar.ParseRFC3339(utcEnd)

	var contexts []context.Context
	if p != nil && len(p.AccountIDs) > 0 {
		for accID := range p.AccountIDs {
			pCtx := jmapauth.ContextWithAccountID(ctx, accID)
			pCtx = jmapauth.ContextWithPrincipalAccountID(pCtx, accID)
			contexts = append(contexts, pCtx)
		}
	} else {
		contexts = append(contexts, ctx)
	}

	for _, pCtx := range contexts {
		cals, err := cb.GetAllCalendars(pCtx)
		if err != nil {
			continue
		}
		calInAvail := make(map[jmapcore.Id]string, len(cals))
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

			for _, inst := range jmapcalendar.ExpandRecurrenceInstances(ev, winEnd) {
				if hasEnd && !inst.Start.Before(winEnd) {
					continue
				}
				if hasStart && !inst.End.After(winStart) {
					continue
				}
				windows = append(windows, &jmapprincipals.AvailabilityWindow{
					UTCStart:       inst.Start.UTC().Format(time.RFC3339),
					UTCEnd:         inst.End.UTC().Format(time.RFC3339),
					FreeBusyStatus: fb,
				})
			}
		}
	}

	return windows, nil
}

func isPrincipalAttending(ev *jmapcalendar.CalendarEvent, p *jmapprincipals.Principal) bool {
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
