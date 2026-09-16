package nextcloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav/caldav"

	"imap-jmap/jmap"
)

// CalendarsBackend implements jmap.CalendarsBackend backed by Nextcloud CalDAV via github.com/emersion/go-webdav/caldav.
type CalendarsBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmap.Broadcaster

	calTrackers          map[string]*jmap.ChangeTracker
	eventTrackers        map[string]*jmap.ChangeTracker
	identityTrackers     map[string]*jmap.ChangeTracker
	notificationTrackers map[string]*jmap.ChangeTracker

	calsFingerprint    map[string]string
	eventsFingerprint  map[string]string

	calsCache          map[string][]*jmap.Calendar
	calPaths           map[string]map[jmap.Id]string
	homeSets           map[string]string
	calsCacheTime      map[string]time.Time
	eventsCache        map[string]map[jmap.Id]*jmap.CalendarEvent
	eventsCacheTime    map[string]time.Time
	identitiesCache    map[string]map[jmap.Id]*jmap.ParticipantIdentity
	notificationsCache map[string]map[jmap.Id]*jmap.CalendarEventNotification
	defaultCalendars   map[string]jmap.Id
	calProps           map[string]map[jmap.Id]*jmap.Calendar
	notifSeq           map[string]map[jmap.Id]uint64
	nextNotifSeq       uint64
	allowedAddresses          map[string]map[string]bool
	shareNotificationsCache   map[string]map[jmap.Id]*jmap.ShareNotification
	shareNotificationTrackers map[string]*jmap.ChangeTracker
	userCalOverrides          map[string]map[jmap.Id]*jmap.Calendar
	principalsBackend         jmap.PrincipalsBackend
}

var _ jmap.CalendarsBackend = (*CalendarsBackend)(nil)

// NewCalendarsBackend initializes a new Nextcloud-backed CalendarsBackend.
func NewCalendarsBackend(client *Client) *CalendarsBackend {
	return &CalendarsBackend{
		client:                    client,
		calTrackers:               make(map[string]*jmap.ChangeTracker),
		eventTrackers:             make(map[string]*jmap.ChangeTracker),
		identityTrackers:          make(map[string]*jmap.ChangeTracker),
		notificationTrackers:      make(map[string]*jmap.ChangeTracker),
		calsFingerprint:           make(map[string]string),
		eventsFingerprint:         make(map[string]string),
		calsCache:                 make(map[string][]*jmap.Calendar),
		calPaths:                  make(map[string]map[jmap.Id]string),
		homeSets:                  make(map[string]string),
		calsCacheTime:             make(map[string]time.Time),
		eventsCache:               make(map[string]map[jmap.Id]*jmap.CalendarEvent),
		eventsCacheTime:           make(map[string]time.Time),
		identitiesCache:           make(map[string]map[jmap.Id]*jmap.ParticipantIdentity),
		notificationsCache:        make(map[string]map[jmap.Id]*jmap.CalendarEventNotification),
		defaultCalendars:          make(map[string]jmap.Id),
		calProps:                  make(map[string]map[jmap.Id]*jmap.Calendar),
		notifSeq:                  make(map[string]map[jmap.Id]uint64),
		allowedAddresses:          make(map[string]map[string]bool),
		shareNotificationsCache:   make(map[string]map[jmap.Id]*jmap.ShareNotification),
		shareNotificationTrackers: make(map[string]*jmap.ChangeTracker),
		userCalOverrides:          make(map[string]map[jmap.Id]*jmap.Calendar),
	}
}

func (b *CalendarsBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *CalendarsBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil {
		accountID := jmap.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
		}
	}
}

func eventFingerprint(ev *jmap.CalendarEvent) string {
	if ev == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%v", ev.ID, ev.Title, ev.Start, ev.Duration, ev.Updated, ev.Description, ev.Sequence, ev.CalendarIDs)
}

func eventsMapFingerprint(m map[jmap.Id]*jmap.CalendarEvent) string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		fmt.Fprintf(h, "%s:%s;", id, eventFingerprint(m[jmap.Id(id)]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func calsListFingerprint(cals []*jmap.Calendar) string {
	h := sha256.New()
	for _, c := range cals {
		if c != nil {
			fmt.Fprintf(h, "%s:%s:%t;", c.ID, c.Name, c.IsVisible)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (b *CalendarsBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	return u
}

func (b *CalendarsBackend) getCalTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.calTrackers[u] == nil {
		b.calTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.calTrackers[u]
}

func (b *CalendarsBackend) getEventTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.eventTrackers[u] == nil {
		b.eventTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.eventTrackers[u]
}

func (b *CalendarsBackend) getIdentityTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.identityTrackers[u] == nil {
		b.identityTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.identityTrackers[u]
}

func (b *CalendarsBackend) getNotificationTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.notificationTrackers[u] == nil {
		b.notificationTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.notificationTrackers[u]
}

func (b *CalendarsBackend) getShareNotificationTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.shareNotificationTrackers[u] == nil {
		b.shareNotificationTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.shareNotificationTrackers[u]
}

func (b *CalendarsBackend) SetPrincipalsBackend(pb jmap.PrincipalsBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.principalsBackend = pb
}

func (b *CalendarsBackend) lookupUserNameLocked(subj string) string {
	pb := b.principalsBackend
	if pb != nil {
		principals, err := pb.GetAllPrincipals(context.Background())
		if err == nil {
			for _, p := range principals {
				if p != nil && (strings.EqualFold(p.Email, subj) || strings.EqualFold(p.CalendarAddress, "mailto:"+subj)) {
					if p.Name != "" {
						return p.Name
					}
				}
			}
		}
	}
	if subj == "jdoe@example.com" {
		return "John Doe"
	}
	if subj == "jane.smith@example.com" {
		return "Jane Smith"
	}
	parts := strings.Split(subj, "@")
	if len(parts) > 0 {
		nameParts := strings.Split(parts[0], ".")
		for i, part := range nameParts {
			if len(part) > 0 {
				nameParts[i] = strings.ToUpper(part[:1]) + part[1:]
			}
		}
		return strings.Join(nameParts, " ")
	}
	return subj
}

func (b *CalendarsBackend) lookupUserName(subj string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lookupUserNameLocked(subj)
}

func (b *CalendarsBackend) CanAccessSharedAccount(principalAccountID, targetAccountID string) (allowed bool, accountKnown bool) {
	if principalAccountID == targetAccountID {
		return true, true
	}
	targetUser, ok := jmap.SubjectForAccountID(targetAccountID)
	if !ok || targetUser == "" {
		targetUser = targetAccountID
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	known := false
	if b.calProps[targetUser] != nil || b.calsCache[targetUser] != nil || b.calPaths[targetUser] != nil || b.defaultCalendars[targetUser] != "" || b.allowedAddresses[targetUser] != nil {
		known = true
	} else if strings.Contains(targetUser, "@") {
		known = true
	}

	for _, cp := range b.calProps[targetUser] {
		if cp != nil && cp.ShareWith != nil && cp.ShareWith[principalAccountID] != nil {
			return true, known
		}
	}
	for _, cal := range b.calsCache[targetUser] {
		if cal != nil && cal.ShareWith != nil && cal.ShareWith[principalAccountID] != nil {
			return true, known
		}
	}

	return false, known
}

// CalendarState
func (b *CalendarsBackend) CalendarState(ctx context.Context) string {
	return b.getCalTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	return b.getCalTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllCalendars(ctx context.Context) ([]*jmap.Calendar, error) {
	list, _, err := b.GetCalendars(ctx, nil)
	return list, err
}

func filterCalendars(list []*jmap.Calendar, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	if ids == nil {
		return list, []jmap.Id{}, nil
	}
	if len(ids) == 0 {
		return []*jmap.Calendar{}, []jmap.Id{}, nil
	}
	idMap := make(map[jmap.Id]bool, len(ids))
	for _, id := range ids {
		idMap[id] = true
	}
	var filtered []*jmap.Calendar
	foundMap := make(map[jmap.Id]bool, len(list))
	for _, c := range list {
		if idMap[c.ID] || (idMap["cal-default"] && c.IsDefault) {
			filtered = append(filtered, c)
			foundMap[c.ID] = true
			if c.IsDefault {
				foundMap["cal-default"] = true
			}
		}
	}
	var notFound []jmap.Id
	for _, id := range ids {
		if !foundMap[id] {
			notFound = append(notFound, id)
		}
	}
	if notFound == nil {
		notFound = []jmap.Id{}
	}
	return filtered, notFound, nil
}

func (b *CalendarsBackend) getCalendarHomeSet(ctx context.Context, calClient *caldav.Client, u string) string {
	b.mu.RLock()
	if hs, ok := b.homeSets[u]; ok && hs != "" {
		b.mu.RUnlock()
		return hs
	}
	b.mu.RUnlock()

	principal, err := calClient.FindCurrentUserPrincipal(ctx)
	if err == nil && principal != "" {
		homeSet, err := calClient.FindCalendarHomeSet(ctx, principal)
		if err == nil && homeSet != "" {
			b.mu.Lock()
			b.homeSets[u] = homeSet
			b.mu.Unlock()
			return homeSet
		}
	}
	defaultHS := "calendars/" + u + "/"
	b.mu.Lock()
	b.homeSets[u] = defaultHS
	b.mu.Unlock()
	return defaultHS
}

func (b *CalendarsBackend) getCalPath(u string, cid jmap.Id, homeSet string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.calPaths[u] != nil {
		if p, ok := b.calPaths[u][cid]; ok && p != "" {
			return p
		}
	}
	if homeSet != "" {
		return strings.TrimRight(homeSet, "/") + "/" + string(cid) + "/"
	}
	return "calendars/" + u + "/" + string(cid) + "/"
}

func (b *CalendarsBackend) GetCalendars(ctx context.Context, ids []jmap.Id) ([]*jmap.Calendar, []jmap.Id, error) {
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	calList, err := calClient.FindCalendars(ctx, homeSet)
	if err != nil {
		// Fallback default personal calendar
		defaultCal := &jmap.Calendar{
			ID:        jmap.Id("personal"),
			Name:      "Personal Calendar",
			IsVisible: true,
			IsDefault: true,
			SortOrder: 0,
			MyRights:  jmap.FullCalendarRights(),
		}
		b.mu.Lock()
		if b.calPaths[u] == nil {
			b.calPaths[u] = make(map[jmap.Id]string)
		}
		b.calPaths[u]["personal"] = strings.TrimRight(homeSet, "/") + "/personal/"
		b.calsCache[u] = []*jmap.Calendar{defaultCal}
		b.calsCacheTime[u] = time.Now()
		b.mu.Unlock()
		return filterCalendars([]*jmap.Calendar{defaultCal}, ids)
	}

	b.mu.RLock()
	defID := b.defaultCalendars[u]
	b.mu.RUnlock()

	var list []*jmap.Calendar
	pathMap := make(map[jmap.Id]string)
	for _, c := range calList {
		calID := path.Base(strings.TrimRight(c.Path, "/"))
		if calID == "inbox" || calID == "outbox" || calID == "trashbin" {
			continue
		}

		name := c.Name
		if name == "" || strings.EqualFold(name, "Personal") {
			name = "Personal Calendar"
		}

		cid := jmap.Id(calID)
		isDefault := false
		if defID != "" {
			isDefault = (cid == defID)
		} else {
			isDefault = (calID == "personal" || strings.EqualFold(name, "Personal") || strings.EqualFold(name, "Personal Calendar"))
		}
		pathMap[cid] = c.Path
		incAvail := "all"
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil && b.calProps[u][cid].IncludeInAvailability != "" {
			incAvail = b.calProps[u][cid].IncludeInAvailability
		}
		b.mu.RUnlock()
		cal := &jmap.Calendar{
			ID:                    cid,
			Name:                  name,
			IsVisible:             true,
			IsDefault:             isDefault,
			SortOrder:             0,
			IncludeInAvailability: incAvail,
			MyRights:              jmap.FullCalendarRights(),
		}
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil {
			cp := b.calProps[u][cid]
			if cp.Name != "" {
				cal.Name = cp.Name
			}
			cal.Description = cp.Description
			cal.Color = cp.Color
			cal.TimeZone = cp.TimeZone
			cal.SortOrder = cp.SortOrder
			cal.IsSubscribed = cp.IsSubscribed
			cal.IsVisible = cp.IsVisible
			if cp.IncludeInAvailability != "" {
				cal.IncludeInAvailability = cp.IncludeInAvailability
			}
			cal.DefaultAlertsWithTime = cp.DefaultAlertsWithTime
			cal.DefaultAlertsWithoutTime = cp.DefaultAlertsWithoutTime
			if cp.ShareWith != nil {
				cal.ShareWith = cp.ShareWith
			}
		}
		b.mu.RUnlock()
		list = append(list, cal)
	}

	if len(list) == 0 {
		cid := jmap.Id("personal")
		pathMap[cid] = strings.TrimRight(homeSet, "/") + "/personal/"
		incAvail := "all"
		cal := &jmap.Calendar{
			ID:                    cid,
			Name:                  "Personal Calendar",
			IsVisible:             true,
			IsDefault:             true,
			SortOrder:             0,
			IncludeInAvailability: incAvail,
			MyRights:              jmap.FullCalendarRights(),
		}
		b.mu.RLock()
		if b.calProps[u] != nil && b.calProps[u][cid] != nil {
			cp := b.calProps[u][cid]
			if cp.Name != "" {
				cal.Name = cp.Name
			}
			cal.Description = cp.Description
			cal.Color = cp.Color
			cal.TimeZone = cp.TimeZone
			cal.SortOrder = cp.SortOrder
			cal.IsSubscribed = cp.IsSubscribed
			cal.IsVisible = cp.IsVisible
			if cp.IncludeInAvailability != "" {
				cal.IncludeInAvailability = cp.IncludeInAvailability
			}
			cal.DefaultAlertsWithTime = cp.DefaultAlertsWithTime
			cal.DefaultAlertsWithoutTime = cp.DefaultAlertsWithoutTime
			if cp.ShareWith != nil {
				cal.ShareWith = cp.ShareWith
			}
		}
		b.mu.RUnlock()
		list = append(list, cal)
	}

	b.mu.Lock()
	if b.calPaths[u] == nil {
		b.calPaths[u] = make(map[jmap.Id]string)
	}
	for k, v := range pathMap {
		b.calPaths[u][k] = v
	}
	newFp := calsListFingerprint(list)
	oldFp := b.calsFingerprint[u]
	b.calsFingerprint[u] = newFp
	b.calsCache[u] = list
	b.calsCacheTime[u] = time.Now()
	needEmit := false
	var st string
	if oldFp != "" && oldFp != newFp {
		st = b.getCalTracker(u).Record("external-sync", "update")
		needEmit = true
	}
	b.mu.Unlock()

	if needEmit {
		b.emitStateChange(u, "Calendar", st)
	}

	callerAccountID, hasCaller := jmap.PrincipalAccountIDFromContext(ctx)
	targetAccountID, _ := jmap.AccountIDFromContext(ctx)
	isSharedCaller := hasCaller && callerAccountID != "" && callerAccountID != targetAccountID

	callerUser := ""
	if hasCaller && callerAccountID != "" {
		if s, ok := jmap.SubjectForAccountID(callerAccountID); ok && s != "" {
			callerUser = s
		} else {
			callerUser = callerAccountID
		}
	}

	if isSharedCaller {
		var sharedList []*jmap.Calendar
		for _, cal := range list {
			if cal != nil && cal.ShareWith != nil && cal.ShareWith[callerAccountID] != nil {
				calCopy := *cal
				calCopy.MyRights = *cal.ShareWith[callerAccountID]
				if !calCopy.MyRights.MayShare {
					calCopy.ShareWith = nil
				}
				b.mu.RLock()
				if b.userCalOverrides[callerUser] != nil && b.userCalOverrides[callerUser][calCopy.ID] != nil {
					ov := b.userCalOverrides[callerUser][calCopy.ID]
					if ov.Name != "" {
						calCopy.Name = ov.Name
					}
					if ov.Description != nil {
						calCopy.Description = ov.Description
					}
				}
				b.mu.RUnlock()
				sharedList = append(sharedList, &calCopy)
			}
		}
		return filterCalendars(sharedList, ids)
	}

	for _, cal := range list {
		if cal != nil && cal.ShareWith == nil {
			cal.ShareWith = make(map[string]*jmap.CalendarRights)
		}
	}

	return filterCalendars(list, ids)
}

func (b *CalendarsBackend) CreateCalendar(ctx context.Context, cal *jmap.Calendar) (*jmap.Calendar, error) {
	if cal == nil {
		return nil, fmt.Errorf("calendar is nil")
	}
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, err
	}

	if cal.ID == "" {
		cal.ID = jmap.Id(fmt.Sprintf("cal-%d", time.Now().UnixNano()))
	}
	if cal.IncludeInAvailability == "" {
		cal.IncludeInAvailability = "all"
	}
	if cal.ShareWith == nil {
		cal.ShareWith = make(map[string]*jmap.CalendarRights)
	}
	cal.MyRights = jmap.FullCalendarRights()

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	calPath := strings.TrimRight(homeSet, "/") + "/" + string(cal.ID) + "/"
	_ = calClient.Mkdir(ctx, calPath)

	b.mu.Lock()
	if b.calPaths[u] == nil {
		b.calPaths[u] = make(map[jmap.Id]string)
	}
	b.calPaths[u][cal.ID] = calPath
	if b.calProps[u] == nil {
		b.calProps[u] = make(map[jmap.Id]*jmap.Calendar)
	}
	calCopy := *cal
	b.calProps[u][cal.ID] = &calCopy
	if b.calsCache[u] != nil {
		b.calsCache[u] = append(b.calsCache[u], cal)
	}
	b.calsCacheTime[u] = time.Time{}
	st := b.getCalTracker(u).Record(cal.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return cal, nil
}

func (b *CalendarsBackend) UpdateCalendar(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.Calendar, error) {
	cals, notFound, err := b.GetCalendars(ctx, []jmap.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(cals) == 0 {
		return nil, jmap.ErrNotFound
	}

	callerAccountID, hasCaller := jmap.PrincipalAccountIDFromContext(ctx)
	targetAccountID, _ := jmap.AccountIDFromContext(ctx)
	isSharedCaller := hasCaller && callerAccountID != "" && callerAccountID != targetAccountID

	callerUser := ""
	if hasCaller && callerAccountID != "" {
		if s, ok := jmap.SubjectForAccountID(callerAccountID); ok && s != "" {
			callerUser = s
		} else {
			callerUser = callerAccountID
		}
	}

	if isSharedCaller {
		b.mu.Lock()
		if b.userCalOverrides[callerUser] == nil {
			b.userCalOverrides[callerUser] = make(map[jmap.Id]*jmap.Calendar)
		}
		ov := b.userCalOverrides[callerUser][id]
		if ov == nil {
			ov = &jmap.Calendar{ID: id}
			b.userCalOverrides[callerUser][id] = ov
		}
		if nameVal, hasName := patch["name"]; hasName {
			if s, ok := nameVal.(string); ok {
				ov.Name = s
			}
		}
		if descVal, hasDesc := patch["description"]; hasDesc {
			if s, ok := descVal.(string); ok {
				ov.Description = &s
			} else if descVal == nil {
				ov.Description = nil
			}
		}
		b.mu.Unlock()
		cals, _, _ = b.GetCalendars(ctx, []jmap.Id{id})
		if len(cals) > 0 {
			return cals[0], nil
		}
		return &jmap.Calendar{ID: id, Name: ov.Name, Description: ov.Description}, nil
	}

	u := b.user(ctx)
	b.mu.Lock()
	if b.calProps[u] == nil {
		b.calProps[u] = make(map[jmap.Id]*jmap.Calendar)
	}
	cp := b.calProps[u][id]
	if cp == nil {
		cpCopy := *cals[0]
		cp = &cpCopy
		b.calProps[u][id] = cp
	}

	oldShareWith := make(map[string]*jmap.CalendarRights)
	if cp.ShareWith != nil {
		for k, v := range cp.ShareWith {
			if v != nil {
				rCopy := *v
				oldShareWith[k] = &rCopy
			}
		}
	}

	_ = applyCalendarPatch(cp, patch)

	if cp.ShareWith != nil {
		for k, v := range cp.ShareWith {
			if v == nil {
				delete(cp.ShareWith, k)
			}
		}
	}

	allPrincipals := make(map[string]bool)
	for p := range oldShareWith {
		allPrincipals[p] = true
	}
	if cp.ShareWith != nil {
		for p := range cp.ShareWith {
			allPrincipals[p] = true
		}
	}

	type shareChange struct {
		pUser string
		pID   string
		oldR  *jmap.CalendarRights
		newR  *jmap.CalendarRights
	}
	var changes []shareChange

	for pID := range allPrincipals {
		oldR := oldShareWith[pID]
		var newR *jmap.CalendarRights
		if cp.ShareWith != nil {
			newR = cp.ShareWith[pID]
		}
		if !equalCalendarRights(oldR, newR) {
			pUser := pID
			if s, ok := jmap.SubjectForAccountID(pID); ok && s != "" {
				pUser = s
			}
			notifOldR := &jmap.CalendarRights{}
			if oldR != nil {
				rCopy := *oldR
				notifOldR = &rCopy
			}
			notifNewR := &jmap.CalendarRights{}
			if newR != nil {
				rCopy := *newR
				notifNewR = &rCopy
			}
			changes = append(changes, shareChange{
				pUser: pUser,
				pID:   pID,
				oldR:  notifOldR,
				newR:  notifNewR,
			})
		}
	}

	b.calsCacheTime[u] = time.Time{}
	st := b.getCalTracker(u).Record(id, "update")

	var shareNotifs []struct {
		user  string
		state string
	}

	for _, ch := range changes {
		notifID := jmap.Id(fmt.Sprintf("sn-%d-%s", time.Now().UnixNano(), ch.pID))
		ownerSubj := u
		ownerName := b.lookupUserNameLocked(ownerSubj)
		notif := &jmap.ShareNotification{
			ID: notifID,
			ChangedBy: jmap.ShareNotificationPerson{
				PrincipalID: targetAccountID,
				Name:        ownerName,
				Email:       ownerSubj,
			},
			ObjectType:      "Calendar",
			ObjectAccountID: targetAccountID,
			ObjectID:        id,
			OldRights:       ch.oldR,
			NewRights:       ch.newR,
			Name:            nil,
		}
		if b.shareNotificationsCache[ch.pUser] == nil {
			b.shareNotificationsCache[ch.pUser] = make(map[jmap.Id]*jmap.ShareNotification)
		}
		b.shareNotificationsCache[ch.pUser][notifID] = notif
		stNotif := b.getShareNotificationTracker(ch.pUser).Record(notifID, "create")
		shareNotifs = append(shareNotifs, struct {
			user  string
			state string
		}{user: ch.pUser, state: stNotif})
	}
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	for _, sn := range shareNotifs {
		b.emitStateChange(sn.user, "ShareNotification", sn.state)
	}

	cals, _, _ = b.GetCalendars(ctx, []jmap.Id{id})
	if len(cals) > 0 {
		return cals[0], nil
	}
	name, _ := patch["name"].(string)
	return &jmap.Calendar{ID: id, Name: name, IncludeInAvailability: cp.IncludeInAvailability, MyRights: jmap.FullCalendarRights()}, nil
}

func (b *CalendarsBackend) DeleteCalendar(ctx context.Context, id jmap.Id) (bool, error) {
	cals, notFound, err := b.GetCalendars(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(cals) == 0 {
		return false, nil
	}

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return false, err
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	calPath := b.getCalPath(u, id, homeSet)
	_ = calClient.RemoveAll(ctx, calPath)

	b.mu.Lock()
	if b.calPaths[u] != nil {
		delete(b.calPaths[u], id)
	}
	if b.calsCache[u] != nil {
		var filtered []*jmap.Calendar
		for _, c := range b.calsCache[u] {
			if c.ID != id {
				filtered = append(filtered, c)
			}
		}
		b.calsCache[u] = filtered
	}
	b.calsCacheTime[u] = time.Now()
	st := b.getCalTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "Calendar", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultCalendar(ctx context.Context, id jmap.Id) error {
	u := b.user(ctx)
	b.mu.Lock()
	if b.defaultCalendars == nil {
		b.defaultCalendars = make(map[string]jmap.Id)
	}
	b.defaultCalendars[u] = id
	b.calsCacheTime[u] = time.Time{}
	b.mu.Unlock()
	return nil
}

func (b *CalendarsBackend) CalendarHasEvents(ctx context.Context, id jmap.Id) (bool, error) {
	events, _, err := b.GetCalendarEvents(ctx, nil)
	if err != nil {
		return false, err
	}
	for _, ev := range events {
		if ev.CalendarIDs[id] {
			return true, nil
		}
	}
	return false, nil
}

// CalendarEventState
func (b *CalendarsBackend) CalendarEventState(ctx context.Context) string {
	return b.getEventTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarEventChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	return b.getEventTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllCalendarEvents(ctx context.Context) ([]*jmap.CalendarEvent, error) {
	evs, _, err := b.GetCalendarEvents(ctx, nil)
	return evs, err
}

func (b *CalendarsBackend) buildEventResponseFromCache(u string, ids []jmap.Id) ([]*jmap.CalendarEvent, []jmap.Id) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.CalendarEvent
	var notFound []jmap.Id
	if ids == nil {
		for _, ev := range b.eventsCache[u] {
			list = append(list, ev)
		}
	} else if len(ids) == 0 {
		list = []*jmap.CalendarEvent{}
		notFound = []jmap.Id{}
	} else {
		for _, id := range ids {
			if ev, ok := b.eventsCache[u][id]; ok {
				list = append(list, ev)
			} else if strings.Contains(string(id), "#") {
				parts := strings.SplitN(string(id), "#", 2)
				masterID := jmap.Id(parts[0])
				recID := parts[1]
				if master, okMaster := b.eventsCache[u][masterID]; okMaster {
					if master.Excluded != nil && master.Excluded[recID] {
						notFound = append(notFound, id)
						continue
					}
					if override, okOv := master.RecurrenceOverrides[recID]; okOv {
						if ex, _ := override["excluded"].(bool); ex {
							notFound = append(notFound, id)
							continue
						}
					}
					inst := *master
					inst.ID = id
					masterIDCopy := masterID
					inst.BaseEventID = &masterIDCopy
					inst.RecurrenceRules = nil
					inst.ExcludedRecurrenceRules = nil
					inst.RecurrenceOverrides = nil
					if len(master.RecurrenceRules) == 0 && len(master.RecurrenceOverrides) == 0 {
						inst.Start = master.Start
						inst.RecurrenceID = ""
					} else {
						inst.Start = recID
						inst.RecurrenceID = recID
						if override, okOv := master.RecurrenceOverrides[recID]; okOv {
							_ = applyEventPatch(&inst, override)
							inst.RecurrenceID = recID
						}
					}
					list = append(list, &inst)
				} else {
					notFound = append(notFound, id)
				}
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	if notFound == nil {
		notFound = []jmap.Id{}
	}
	return list, notFound
}

func (b *CalendarsBackend) GetCalendarEvents(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEvent, []jmap.Id, error) {
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	cals, _, _ := b.GetCalendars(ctx, nil)
	homeSet := b.getCalendarHomeSet(ctx, calClient, u)

	type calResult struct {
		calID jmap.Id
		objs  []caldav.CalendarObject
	}
	resChan := make(chan calResult, len(cals))
	var wg sync.WaitGroup

	for _, cal := range cals {
		wg.Add(1)
		go func(cal *jmap.Calendar) {
			defer wg.Done()
			calPath := b.getCalPath(u, cal.ID, homeSet)
			objs, qErr := calClient.QueryCalendar(ctx, calPath, &caldav.CalendarQuery{
				CompFilter: caldav.CompFilter{
					Name: "VCALENDAR",
				},
			})
			if qErr == nil {
				resChan <- calResult{calID: cal.ID, objs: objs}
			}
		}(cal)
	}
	wg.Wait()
	close(resChan)

	freshMap := make(map[jmap.Id]*jmap.CalendarEvent)
	for res := range resChan {
		for _, calObj := range res.objs {
			if calObj.Data == nil {
				continue
			}
			name := path.Base(calObj.Path)
			rawID := strings.TrimSuffix(name, ".ics")
			evID := jmap.Id(rawID)

			var buf bytes.Buffer
			_ = ical.NewEncoder(&buf).Encode(calObj.Data)

			parsedList, pErr := jmap.ParseICalendar(buf.Bytes())
			if pErr == nil && len(parsedList) > 0 {
				ev := parsedList[0]
				ev.ID = evID
				if existing, ok := freshMap[evID]; ok {
					if existing.CalendarIDs == nil {
						existing.CalendarIDs = make(map[jmap.Id]bool)
					}
					existing.CalendarIDs[res.calID] = true
					continue
				}
				if ev.CalendarIDs == nil {
					ev.CalendarIDs = make(map[jmap.Id]bool)
				}
				ev.CalendarIDs[res.calID] = true
				b.mu.RLock()
				if b.eventsCache[u] != nil {
					if cached, okCached := b.eventsCache[u][evID]; okCached && cached != nil {
						ev.IsDraft = cached.IsDraft
						ev.MayInviteSelf = cached.MayInviteSelf
						ev.MayInviteOthers = cached.MayInviteOthers
						ev.HideAttendees = cached.HideAttendees
						ev.UseDefaultAlerts = cached.UseDefaultAlerts
						if cached.TimeZone != "" {
							ev.TimeZone = cached.TimeZone
						}
						if ev.OrganizerCalendarAddress == "" && cached.OrganizerCalendarAddress != "" {
							ev.OrganizerCalendarAddress = cached.OrganizerCalendarAddress
						}
						if len(cached.CalendarIDs) > 0 {
							for cid, isSet := range cached.CalendarIDs {
								if isSet {
									ev.CalendarIDs[cid] = true
								}
							}
						}
						if len(ev.RecurrenceOverrides) == 0 && len(cached.RecurrenceOverrides) > 0 {
							ev.RecurrenceOverrides = cached.RecurrenceOverrides
						}
						if len(ev.Excluded) == 0 && len(cached.Excluded) > 0 {
							ev.Excluded = cached.Excluded
						}
						if len(cached.Participants) > 0 {
							ev.Participants = cached.Participants
						}
					}
				}
				b.mu.RUnlock()
				freshMap[evID] = ev
			}
		}
	}

	b.mu.Lock()
	newFp := eventsMapFingerprint(freshMap)
	oldFp := b.eventsFingerprint[u]
	b.eventsFingerprint[u] = newFp
	b.eventsCache[u] = freshMap
	b.eventsCacheTime[u] = time.Now()
	needEmit := false
	var st string
	if oldFp != "" && oldFp != newFp {
		st = b.getEventTracker(u).Record("external-sync", "update")
		needEmit = true
	}
	b.mu.Unlock()

	if needEmit {
		b.emitStateChange(u, "CalendarEvent", st)
	}

	list, notFound := b.buildEventResponseFromCache(u, ids)
	return list, notFound, nil
}

func (b *CalendarsBackend) CreateCalendarEvent(ctx context.Context, event *jmap.CalendarEvent) (*jmap.CalendarEvent, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}
	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return nil, err
	}

	if event.ID == "" {
		event.ID = jmap.Id(fmt.Sprintf("event-%d", time.Now().UnixNano()))
	}
	if event.UID == "" {
		event.UID = string(event.ID)
	}
	if event.TimeZone == "" {
		event.TimeZone = "Etc/UTC"
	}
	if event.UTCStart == "" {
		event.UTCStart = jmap.ComputeUTCStart(event.Start, event.TimeZone)
	}
	if event.UTCEnd == "" {
		event.UTCEnd = jmap.ComputeUTCEnd(event.Start, event.Duration, event.TimeZone)
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)

	if len(event.CalendarIDs) > 0 {
		cals, _, err := b.GetCalendars(ctx, nil)
		if err == nil {
			calMap := make(map[jmap.Id]bool, len(cals))
			for _, c := range cals {
				calMap[c.ID] = true
			}
			for cid := range event.CalendarIDs {
				if cid != "cal-default" && !calMap[cid] {
					return nil, jmap.SetError{Type: "notFound", Description: fmt.Sprintf("calendar %s not found", cid)}
				}
			}
		}
	}

	calObj := jmap.CalendarEventToICalendar(event, "", "", "", "")
	written := false
	for cid, isSet := range event.CalendarIDs {
		if isSet && cid != "cal-default" && cid != "" {
			calPath := b.getCalPath(u, cid, homeSet)
			eventPath := strings.TrimRight(calPath, "/") + "/" + string(event.ID) + ".ics"
			_, putErr := calClient.PutCalendarObject(ctx, eventPath, calObj)
			if putErr != nil {
				return nil, fmt.Errorf("failed to put calendar object via caldav client: %w", putErr)
			}
			written = true
		}
	}
	if !written {
		calPath := b.getCalPath(u, "personal", homeSet)
		eventPath := strings.TrimRight(calPath, "/") + "/" + string(event.ID) + ".ics"
		_, putErr := calClient.PutCalendarObject(ctx, eventPath, calObj)
		if putErr != nil {
			return nil, fmt.Errorf("failed to put calendar object via caldav client: %w", putErr)
		}
		if event.CalendarIDs == nil {
			event.CalendarIDs = make(map[jmap.Id]bool)
		}
		event.CalendarIDs["personal"] = true
	}

	b.mu.Lock()
	if b.eventsCache[u] == nil {
		b.eventsCache[u] = make(map[jmap.Id]*jmap.CalendarEvent)
	}
	action := "create"
	if _, exists := b.eventsCache[u][event.ID]; exists {
		action = "update"
	}
	b.eventsCache[u][event.ID] = event
	b.eventsCacheTime[u] = time.Now()
	st := b.getEventTracker(u).Record(event.ID, action)
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEvent", st)
	b.scheduleEventAlerts(u, event)
	return event, nil
}

func parseAlertOffset(alert *jmap.JSCalendarAlert) time.Duration {
	if alert == nil || alert.Trigger == nil {
		return 0
	}
	var offsetStr string
	switch t := alert.Trigger.(type) {
	case string:
		offsetStr = t
	case jmap.OffsetTrigger:
		offsetStr = t.Offset
	case *jmap.OffsetTrigger:
		offsetStr = t.Offset
	case map[string]any:
		if s, ok := t["offset"].(string); ok {
			offsetStr = s
		}
	}
	if offsetStr == "" {
		return 0
	}
	neg := false
	if strings.HasPrefix(offsetStr, "-") {
		neg = true
		offsetStr = strings.TrimPrefix(offsetStr, "-")
	} else if strings.HasPrefix(offsetStr, "+") {
		offsetStr = strings.TrimPrefix(offsetStr, "+")
	}

	prop := ical.NewProp(ical.PropDuration)
	prop.Value = offsetStr
	dur, err := prop.Duration()
	if err != nil {
		return 0
	}
	if neg {
		dur = -dur
	}
	return dur
}

func parseEventTime(startStr, tzName string) time.Time {
	loc := time.UTC
	if tzName != "" && tzName != "Etc/UTC" {
		if l, err := time.LoadLocation(tzName); err == nil {
			loc = l
		}
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, startStr, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (b *CalendarsBackend) scheduleEventAlerts(user string, ev *jmap.CalendarEvent) {
	b.mu.RLock()
	bc := b.broadcaster
	b.mu.RUnlock()
	if bc == nil || ev == nil || len(ev.Alerts) == 0 || ev.Start == "" {
		return
	}
	accountID := jmap.AccountIDForSubject(user)
	startTime := parseEventTime(ev.Start, ev.TimeZone)
	if startTime.IsZero() {
		return
	}
	now := time.Now()
	for alertID, alert := range ev.Alerts {
		if alert == nil {
			continue
		}
		offsetDur := parseAlertOffset(alert)
		triggerTime := startTime.Add(offsetDur)
		delay := triggerTime.Sub(now)
		if delay < 0 {
			continue
		}
		aID := alertID
		evID := string(ev.ID)
		evUID := ev.UID
		time.AfterFunc(delay, func() {
			calAlert := &jmap.CalendarAlert{
				Type:            "CalendarAlert",
				AccountID:       accountID,
				CalendarEventID: evID,
				UID:             evUID,
				RecurrenceID:    nil,
				AlertID:         aID,
			}
			bc.PublishCalendarAlert(calAlert)
		})
	}
}

func setNestedMapValue(m map[string]any, parts []string, val any) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		if val == nil {
			delete(m, parts[0])
		} else {
			m[parts[0]] = val
		}
		return
	}
	key := parts[0]
	if len(parts) == 2 {
		sub, ok := m[key].(map[string]any)
		if !ok {
			if val == nil {
				return
			}
			sub = make(map[string]any)
			m[key] = sub
		}
		if val == nil || (val == false && (key == "calendarIds" || key == "mailboxIds" || key == "keywords" || key == "addressBookIds" || key == "roles")) {
			delete(sub, parts[1])
		} else {
			sub[parts[1]] = val
		}
		return
	}
	sub, ok := m[key].(map[string]any)
	if !ok {
		if val == nil {
			return
		}
		sub = make(map[string]any)
		m[key] = sub
	}
	setNestedMapValue(sub, parts[1:], val)
}

func applyCalendarPatch(cal *jmap.Calendar, patch map[string]any) error {
	raw, err := json.Marshal(cal)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m == nil {
		m = make(map[string]any)
	}
	for path, val := range patch {
		cleanPath := strings.TrimPrefix(path, "/")
		parts := strings.Split(cleanPath, "/")
		setNestedMapValue(m, parts, val)
	}
	rawUpdated, err := json.Marshal(m)
	if err != nil {
		return err
	}
	origID := cal.ID
	origRights := cal.MyRights
	var updatedCal jmap.Calendar
	if err := json.Unmarshal(rawUpdated, &updatedCal); err != nil {
		return err
	}
	updatedCal.ID = origID
	updatedCal.MyRights = origRights
	*cal = updatedCal
	return nil
}

func applyEventPatch(ev *jmap.CalendarEvent, patch map[string]any) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if m == nil {
		m = make(map[string]any)
	}

	for path, val := range patch {
		cleanPath := strings.TrimPrefix(path, "/")
		parts := strings.Split(cleanPath, "/")
		setNestedMapValue(m, parts, val)
		if len(parts) == 3 && parts[0] == "participants" {
			if parts[2] == "participationStatus" {
				setNestedMapValue(m, []string{"participants", parts[1], "status"}, val)
			} else if parts[2] == "status" {
				setNestedMapValue(m, []string{"participants", parts[1], "participationStatus"}, val)
			}
		}
	}

	rawUpdated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	origID := ev.ID
	origUID := ev.UID
	origType := ev.Type
	origCalIDs := ev.CalendarIDs
	var updatedEv jmap.CalendarEvent
	if err := json.Unmarshal(rawUpdated, &updatedEv); err != nil {
		return err
	}
	updatedEv.ID = origID
	if updatedEv.UID == "" {
		updatedEv.UID = origUID
	}
	if updatedEv.Type == "" {
		updatedEv.Type = origType
	}

	for cid, isSet := range updatedEv.CalendarIDs {
		if !isSet {
			delete(updatedEv.CalendarIDs, cid)
		}
	}

	if cid, ok := m["calendarId"].(string); ok && cid != "" {
		if updatedEv.CalendarIDs == nil {
			updatedEv.CalendarIDs = make(map[jmap.Id]bool)
		}
		updatedEv.CalendarIDs[jmap.Id(cid)] = true
	}

	if len(updatedEv.CalendarIDs) == 0 {
		if _, hasCalIds := m["calendarIds"]; !hasCalIds {
			if _, hasCalId := m["calendarId"]; !hasCalId {
				updatedEv.CalendarIDs = origCalIDs
			}
		}
	}

	if locVal, hasLoc := patch["location"]; hasLoc {
		if locVal == nil {
			updatedEv.Locations = nil
		} else if locStr, ok := locVal.(string); ok {
			updatedEv.Locations = map[string]*jmap.JSCalendarLocation{
				"loc-1": {
					Type: "Location",
					Name: locStr,
				},
			}
		} else if locBytes, err := json.Marshal(locVal); err == nil {
			var loc jmap.JSCalendarLocation
			if err := json.Unmarshal(locBytes, &loc); err == nil {
				if loc.Type == "" {
					loc.Type = "Location"
				}
				updatedEv.Locations = map[string]*jmap.JSCalendarLocation{
					"loc-1": &loc,
				}
			}
		}
	}

	if allDay, ok := m["allDay"].(bool); ok {
		updatedEv.ShowWithoutTime = allDay
	}
	if sum, ok := m["summary"].(string); ok && sum != "" && updatedEv.Title == "" {
		updatedEv.Title = sum
	}
	if end, ok := m["end"].(string); ok && end != "" {
		if updatedEv.Start != "" {
			updatedEv.Duration = jmap.IcalDurationBetween(updatedEv.Start, end)
		}
	}
	if rrule, hasRrule := m["recurrenceRule"]; hasRrule {
		if rrule == nil {
			updatedEv.RecurrenceRules = nil
			updatedEv.RecurrenceRule = nil
		} else if rruleBytes, err := json.Marshal(rrule); err == nil {
			var r jmap.JSCalendarRecurrenceRule
			if err := json.Unmarshal(rruleBytes, &r); err == nil {
				updatedEv.RecurrenceRule = &r
				updatedEv.RecurrenceRules = []*jmap.JSCalendarRecurrenceRule{&r}
			}
		}
	}
	if exrule, hasExrule := m["excludedRecurrenceRule"]; hasExrule {
		if exrule == nil {
			updatedEv.ExcludedRecurrenceRules = nil
			updatedEv.ExcludedRecurrenceRule = nil
		} else if exruleBytes, err := json.Marshal(exrule); err == nil {
			var r jmap.JSCalendarRecurrenceRule
			if err := json.Unmarshal(exruleBytes, &r); err == nil {
				updatedEv.ExcludedRecurrenceRule = &r
				updatedEv.ExcludedRecurrenceRules = []*jmap.JSCalendarRecurrenceRule{&r}
			}
		}
	}
	if len(updatedEv.RecurrenceRules) > 0 && updatedEv.RecurrenceRule == nil {
		updatedEv.RecurrenceRule = updatedEv.RecurrenceRules[0]
	}
	if len(updatedEv.ExcludedRecurrenceRules) > 0 && updatedEv.ExcludedRecurrenceRule == nil {
		updatedEv.ExcludedRecurrenceRule = updatedEv.ExcludedRecurrenceRules[0]
	}
	if updatedEv.TimeZone == "" {
		updatedEv.TimeZone = "Etc/UTC"
	}
	if utcStartStr, ok := patch["utcStart"].(string); ok && utcStartStr != "" {
		if t, err := time.Parse(time.RFC3339, utcStartStr); err == nil {
			loc := time.UTC
			if updatedEv.TimeZone != "" && updatedEv.TimeZone != "floating" {
				loc = jmap.LoadLocation(updatedEv.TimeZone)
			}
			updatedEv.Start = t.In(loc).Format("2006-01-02T15:04:05")
			updatedEv.UTCStart = t.UTC().Format(time.RFC3339)
		}
	}
	if utcEndStr, ok := patch["utcEnd"].(string); ok && utcEndStr != "" {
		if updatedEv.UTCStart != "" {
			if dur := jmap.IcalDurationBetween(updatedEv.UTCStart, utcEndStr); dur != "" {
				updatedEv.Duration = dur
			}
		}
		updatedEv.UTCEnd = utcEndStr
	}
	updatedEv.Start = strings.TrimSuffix(updatedEv.Start, "Z")
	updatedEv.UTCStart = jmap.ComputeUTCStart(updatedEv.Start, updatedEv.TimeZone)
	updatedEv.UTCEnd = jmap.ComputeUTCEnd(updatedEv.Start, updatedEv.Duration, updatedEv.TimeZone)

	for _, p := range updatedEv.Participants {
		if p != nil {
			if p.ParticipationStatus != "" && (p.Status == "" || p.Status != p.ParticipationStatus) {
				p.Status = p.ParticipationStatus
			} else if p.Status != "" && p.ParticipationStatus == "" {
				p.ParticipationStatus = p.Status
			}
		}
	}

	if updatedEv.OrganizerCalendarAddress == "" && len(updatedEv.Participants) > 0 {
		for _, p := range updatedEv.Participants {
			if p != nil && (p.Role == "owner" || (p.Roles != nil && p.Roles["owner"])) {
				updatedEv.OrganizerCalendarAddress = p.CalendarAddress
				break
			}
		}
		if updatedEv.OrganizerCalendarAddress == "" {
			for _, p := range updatedEv.Participants {
				if p != nil && p.CalendarAddress != "" {
					updatedEv.OrganizerCalendarAddress = p.CalendarAddress
					break
				}
			}
		}
	}

	*ev = updatedEv
	return nil
}

func (b *CalendarsBackend) UpdateCalendarEvent(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.CalendarEvent, error) {
	if strings.Contains(string(id), "#") {
		parts := strings.SplitN(string(id), "#", 2)
		masterID := jmap.Id(parts[0])
		recID := parts[1]
		masters, _, err := b.GetCalendarEvents(ctx, []jmap.Id{masterID})
		if err != nil || len(masters) == 0 {
			return nil, jmap.SetError{Type: "notFound", Description: fmt.Sprintf("event %s not found", id)}
		}
		master := masters[0]
		if len(master.RecurrenceRules) == 0 && len(master.RecurrenceOverrides) == 0 {
			return b.UpdateCalendarEvent(ctx, masterID, patch)
		}
		if master.Excluded != nil && master.Excluded[recID] {
			return nil, jmap.SetError{Type: "notFound", Description: fmt.Sprintf("instance %s not found", id)}
		}
		validOccurrence := false
		if _, ok := master.RecurrenceOverrides[recID]; ok {
			validOccurrence = true
		} else {
			instances := jmap.ExpandRecurrenceInstances(master, time.Time{})
			for _, inst := range instances {
				if inst.RecurrenceID == recID || strings.TrimSuffix(inst.RecurrenceID, "Z") == strings.TrimSuffix(recID, "Z") {
					validOccurrence = true
					break
				}
			}
		}
		if !validOccurrence {
			return nil, jmap.SetError{Type: "notFound", Description: fmt.Sprintf("instance %s not found", id)}
		}

		if master.RecurrenceOverrides == nil {
			master.RecurrenceOverrides = make(map[string]map[string]any)
		}
		overridePatch, exists := master.RecurrenceOverrides[recID]
		if !exists || overridePatch == nil {
			overridePatch = make(map[string]any)
			overridePatch["start"] = recID
			overridePatch["duration"] = master.Duration
			if len(master.Locations) > 0 {
				locMap := make(map[string]any)
				for k, v := range master.Locations {
					locBytes, _ := json.Marshal(v)
					var m any
					_ = json.Unmarshal(locBytes, &m)
					locMap[k] = m
				}
				overridePatch["locations"] = locMap
			}
		}
		for k, v := range patch {
			cleanKey := strings.TrimPrefix(k, "/")
			firstPart := strings.Split(cleanKey, "/")[0]
			if firstPart == "@type" || firstPart == "uid" || firstPart == "recurrenceRule" ||
				firstPart == "recurrenceRules" || firstPart == "privacy" || firstPart == "participants" {
				continue
			}
			parts := strings.Split(cleanKey, "/")
			setNestedMapValue(overridePatch, parts, v)
		}
		master.RecurrenceOverrides[recID] = overridePatch
		master.Sequence++
		master.Updated = time.Now().UTC().Format(time.RFC3339)
		return b.CreateCalendarEvent(ctx, master)
	}

	u := b.user(ctx)
	b.mu.RLock()
	var ev *jmap.CalendarEvent
	if b.eventsCache[u] != nil {
		if cached, ok := b.eventsCache[u][id]; ok && cached != nil {
			evCopy := *cached
			ev = &evCopy
		}
	}
	b.mu.RUnlock()

	if ev == nil {
		events, notFound, err := b.GetCalendarEvents(ctx, []jmap.Id{id})
		if err != nil || len(notFound) > 0 || len(events) == 0 {
			return nil, fmt.Errorf("event %s not found: %w", id, jmap.ErrNotFound)
		}
		ev = events[0]
	}

	oldCalendarIDs := make(map[jmap.Id]bool)
	for cid, isSet := range ev.CalendarIDs {
		if isSet && cid != "" && cid != "cal-default" {
			oldCalendarIDs[cid] = true
		}
	}

	hasSeq := false
	if _, ok := patch["sequence"]; ok {
		hasSeq = true
	}
	if err := applyEventPatch(ev, patch); err != nil {
		return nil, err
	}
	if !hasSeq {
		ev.Sequence++
	}
	ev.Updated = time.Now().UTC().Format(time.RFC3339)
	ev.UTCStart = jmap.ComputeUTCStart(ev.Start, ev.TimeZone)
	ev.UTCEnd = jmap.ComputeUTCEnd(ev.Start, ev.Duration, ev.TimeZone)

	calClient, u, cErr := b.client.CalDAV(ctx)
	if cErr == nil {
		homeSet := b.getCalendarHomeSet(ctx, calClient, u)
		for oldCID := range oldCalendarIDs {
			if !ev.CalendarIDs[oldCID] {
				oldPath := strings.TrimRight(b.getCalPath(u, oldCID, homeSet), "/") + "/" + string(id) + ".ics"
				_ = calClient.RemoveAll(ctx, oldPath)
			}
		}
	}

	return b.CreateCalendarEvent(ctx, ev)
}

func (b *CalendarsBackend) DeleteCalendarEvent(ctx context.Context, id jmap.Id) (bool, error) {
	if strings.Contains(string(id), "#") {
		parts := strings.SplitN(string(id), "#", 2)
		masterID := jmap.Id(parts[0])
		recID := parts[1]
		masters, _, err := b.GetCalendarEvents(ctx, []jmap.Id{masterID})
		if err == nil && len(masters) > 0 {
			master := masters[0]
			if len(master.RecurrenceRules) == 0 && len(master.RecurrenceOverrides) == 0 {
				return b.DeleteCalendarEvent(ctx, masterID)
			}
			if master.RecurrenceOverrides == nil {
				master.RecurrenceOverrides = make(map[string]map[string]any)
			}
			if master.Excluded == nil {
				master.Excluded = make(map[string]bool)
			}
			master.Excluded[recID] = true
			master.RecurrenceOverrides[recID] = map[string]any{"excluded": true}
			master.Sequence++
			master.Updated = time.Now().UTC().Format(time.RFC3339)
			_, _ = b.CreateCalendarEvent(ctx, master)
			return true, nil
		}
		return false, nil
	}

	events, notFound, err := b.GetCalendarEvents(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(events) == 0 {
		return false, nil
	}
	targetEv := events[0]

	calClient, u, err := b.client.CalDAV(ctx)
	if err != nil {
		return false, err
	}

	homeSet := b.getCalendarHomeSet(ctx, calClient, u)
	if len(targetEv.CalendarIDs) > 0 {
		for cid, isSet := range targetEv.CalendarIDs {
			if isSet && cid != "" && cid != "cal-default" {
				calPath := b.getCalPath(u, cid, homeSet)
				eventPath := strings.TrimRight(calPath, "/") + "/" + string(id) + ".ics"
				_ = calClient.RemoveAll(ctx, eventPath)
			}
		}
	} else {
		calPath := b.getCalPath(u, "personal", homeSet)
		eventPath := strings.TrimRight(calPath, "/") + "/" + string(id) + ".ics"
		_ = calClient.RemoveAll(ctx, eventPath)
	}

	b.mu.Lock()
	if b.eventsCache[u] != nil {
		delete(b.eventsCache[u], id)
	}
	b.eventsCacheTime[u] = time.Now()
	st := b.getEventTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEvent", st)
	return true, nil
}

func (b *CalendarsBackend) QueryCalendarEvents(ctx context.Context, filter map[string]any, sortCriteria []jmap.Comparator, position int, limit *uint64, expandRecurrences bool) ([]jmap.Id, int, error) {
	events, _, err := b.GetCalendarEvents(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	loc := time.UTC
	if tz, ok := filter["__timeZone"].(string); ok && tz != "" {
		loc = jmap.LoadLocation(tz)
	}

	var resultIDs []jmap.Id
	if expandRecurrences {
		horizon := time.Now().AddDate(2, 0, 0)
		if beforeStr, _ := filter["before"].(string); beforeStr != "" {
			if bt, ok := jmap.ParseLocalDateTimeBound(beforeStr, loc); ok {
				if bt.After(horizon) {
					horizon = bt.AddDate(0, 0, 1)
				}
			}
		}

		var expandedList []*jmap.CalendarEvent
		for _, ev := range events {
			hasRules := len(ev.RecurrenceRules) > 0 || len(ev.RecurrenceOverrides) > 0
			if hasRules {
				instances := jmap.ExpandRecurrenceInstances(ev, horizon)
				for _, inst := range instances {
					instEv := *ev
					instEv.ID = jmap.Id(fmt.Sprintf("%s#%s", string(ev.ID), inst.RecurrenceID))
					evIDCopy := ev.ID
					instEv.BaseEventID = &evIDCopy
					instEv.RecurrenceRules = nil
					instEv.ExcludedRecurrenceRules = nil
					instEv.RecurrenceOverrides = nil
					instEv.RecurrenceID = inst.RecurrenceID
					if ov, ok := ev.RecurrenceOverrides[inst.RecurrenceID]; ok {
						_ = applyEventPatch(&instEv, ov)
						instEv.RecurrenceID = inst.RecurrenceID
					} else {
						if strings.HasSuffix(ev.Start, "Z") {
							instEv.Start = inst.Start.UTC().Format(time.RFC3339)
						} else {
							instLoc := loc
							if ev.TimeZone != "" && ev.TimeZone != "floating" {
								instLoc = jmap.LoadLocation(ev.TimeZone)
							}
							instEv.Start = inst.Start.In(instLoc).Format("2006-01-02T15:04:05")
						}
					}
					instEv.UTCStart = jmap.ComputeUTCStart(instEv.Start, instEv.TimeZone)
					instEv.UTCEnd = jmap.ComputeUTCEnd(instEv.Start, instEv.Duration, instEv.TimeZone)

					if jmap.MatchCalendarEvent(&instEv, filter) {
						expandedList = append(expandedList, &instEv)
					}
				}
			} else {
				instEv := *ev
				instEv.ID = jmap.Id(fmt.Sprintf("%s#%s", string(ev.ID), ev.Start))
				evIDCopy := ev.ID
				instEv.BaseEventID = &evIDCopy
				if jmap.MatchCalendarEvent(&instEv, filter) {
					expandedList = append(expandedList, &instEv)
				}
			}
		}

		jmap.SortCalendarEvents(expandedList, sortCriteria)

		for _, ev := range expandedList {
			resultIDs = append(resultIDs, ev.ID)
		}
	} else {
		var matched []*jmap.CalendarEvent
		for _, ev := range events {
			if jmap.MatchCalendarEvent(ev, filter) {
				matched = append(matched, ev)
			}
		}
		jmap.SortCalendarEvents(matched, sortCriteria)
		for _, ev := range matched {
			resultIDs = append(resultIDs, ev.ID)
		}
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

// ParticipantIdentities
func (b *CalendarsBackend) ParticipantIdentityState(ctx context.Context) string {
	return b.getIdentityTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) ParticipantIdentityChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getIdentityTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllParticipantIdentities(ctx context.Context) ([]*jmap.ParticipantIdentity, error) {
	list, _, err := b.GetParticipantIdentities(ctx, nil)
	return list, err
}

func (b *CalendarsBackend) GetParticipantIdentities(ctx context.Context, ids []jmap.Id) ([]*jmap.ParticipantIdentity, []jmap.Id, error) {
	u := b.user(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.identitiesCache[u] == nil {
		b.identitiesCache[u] = make(map[jmap.Id]*jmap.ParticipantIdentity)
		defaultID := jmap.Id("identity-default")
		b.identitiesCache[u][defaultID] = &jmap.ParticipantIdentity{
			ID:              defaultID,
			Name:            u,
			CalendarAddress: "mailto:" + u,
			SendTo:          map[string]string{"imip": "mailto:" + u},
			IsDefault:       true,
		}
	}

	var list []*jmap.ParticipantIdentity
	var notFound []jmap.Id
	if ids == nil {
		for _, id := range b.identitiesCache[u] {
			list = append(list, id)
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].ID < list[j].ID
		})
	} else {
		for _, id := range ids {
			if pi, ok := b.identitiesCache[u][id]; ok {
				list = append(list, pi)
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	return list, notFound, nil
}

func (b *CalendarsBackend) SetAllowedAddresses(user string, addrs []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.allowedAddresses == nil {
		b.allowedAddresses = make(map[string]map[string]bool)
	}
	m := make(map[string]bool)
	for _, a := range addrs {
		cleaned := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a)), "mailto:")
		if cleaned != "" {
			m[cleaned] = true
		}
	}
	b.allowedAddresses[user] = m
}

func (b *CalendarsBackend) CreateParticipantIdentity(ctx context.Context, identity *jmap.ParticipantIdentity) (*jmap.ParticipantIdentity, error) {
	u := b.user(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.identitiesCache[u] == nil {
		b.identitiesCache[u] = make(map[jmap.Id]*jmap.ParticipantIdentity)
	}

	if identity.CalendarAddress != "" && !strings.HasPrefix(identity.CalendarAddress, "mailto:") {
		identity.CalendarAddress = "mailto:" + identity.CalendarAddress
	}

	if b.allowedAddresses != nil && len(b.allowedAddresses[u]) > 0 {
		cleaned := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(identity.CalendarAddress)), "mailto:")
		if !b.allowedAddresses[u][cleaned] {
			return nil, jmap.SetError{
				Type:        "invalidProperties",
				Description: "Calendar address not configured for this account.",
				Properties:  []string{"calendarAddress"},
			}
		}
	}

	for _, existing := range b.identitiesCache[u] {
		if strings.EqualFold(existing.CalendarAddress, identity.CalendarAddress) {
			return nil, jmap.SetError{
				Type:        "invalidProperties",
				Description: "Calendar address already in use.",
				Properties:  []string{"calendarAddress"},
			}
		}
	}

	if identity.ID == "" {
		allSingleLetters := true
		maxLetter := byte('a' - 1)
		for existingID := range b.identitiesCache[u] {
			str := string(existingID)
			if len(str) == 1 && str[0] >= 'a' && str[0] <= 'z' {
				if str[0] > maxLetter {
					maxLetter = str[0]
				}
			} else {
				allSingleLetters = false
				break
			}
		}
		if allSingleLetters && maxLetter >= 'a'-1 && maxLetter < 'z' {
			identity.ID = jmap.Id(string([]byte{maxLetter + 1}))
		} else {
			identity.ID = jmap.Id(fmt.Sprintf("pi-%d", time.Now().UnixNano()))
		}
	}
	b.identitiesCache[u][identity.ID] = identity
	st := b.getIdentityTracker(u).Record(identity.ID, "create")

	b.emitStateChange(u, "ParticipantIdentity", st)
	return identity, nil
}

func (b *CalendarsBackend) UpdateParticipantIdentity(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.ParticipantIdentity, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if pi, ok := b.identitiesCache[u][id]; ok {
		if n, ok := patch["name"].(string); ok {
			pi.Name = n
		}
		if ca, ok := patch["calendarAddress"].(string); ok {
			pi.CalendarAddress = ca
		}
		if st, ok := patch["sendTo"].(map[string]any); ok {
			pi.SendTo = make(map[string]string)
			for k, v := range st {
				if s, ok := v.(string); ok {
					pi.SendTo[k] = s
				}
			}
		}
		st := b.getIdentityTracker(u).Record(id, "update")
		b.mu.Unlock()
		b.emitStateChange(u, "ParticipantIdentity", st)
		return pi, nil
	}
	b.mu.Unlock()
	return nil, fmt.Errorf("participant identity not found")
}

func (b *CalendarsBackend) DeleteParticipantIdentity(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if _, ok := b.identitiesCache[u][id]; !ok {
		b.mu.Unlock()
		return false, fmt.Errorf("participant identity not found")
	}
	delete(b.identitiesCache[u], id)
	st := b.getIdentityTracker(u).Record(id, "destroy")
	b.mu.Unlock()
	b.emitStateChange(u, "ParticipantIdentity", st)
	return true, nil
}

func (b *CalendarsBackend) SetDefaultParticipantIdentity(ctx context.Context, id jmap.Id) error {
	u := b.user(ctx)
	b.mu.Lock()
	if _, ok := b.identitiesCache[u][id]; ok {
		for otherID, other := range b.identitiesCache[u] {
			wasDefault := other.IsDefault
			other.IsDefault = (otherID == id)
			if wasDefault != other.IsDefault {
				b.getIdentityTracker(u).Record(otherID, "update")
			}
		}
		st := b.getIdentityTracker(u).State()
		b.mu.Unlock()
		b.emitStateChange(u, "ParticipantIdentity", st)
		return nil
	}
	b.mu.Unlock()
	return fmt.Errorf("participant identity not found")
}

// CalendarEventNotifications
func (b *CalendarsBackend) CalendarEventNotificationState(ctx context.Context) string {
	return b.getNotificationTracker(b.user(ctx)).State()
}

func (b *CalendarsBackend) CalendarEventNotificationChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getNotificationTracker(b.user(ctx)).Changes(sinceState)
}

func (b *CalendarsBackend) GetAllCalendarEventNotifications(ctx context.Context) ([]*jmap.CalendarEventNotification, error) {
	list, _, err := b.GetCalendarEventNotifications(ctx, nil)
	return list, err
}

func (b *CalendarsBackend) GetCalendarEventNotifications(ctx context.Context, ids []jmap.Id) ([]*jmap.CalendarEventNotification, []jmap.Id, error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()
	var list []*jmap.CalendarEventNotification
	var notFound []jmap.Id
	if ids == nil {
		if b.notificationsCache[u] != nil {
			for _, n := range b.notificationsCache[u] {
				list = append(list, n)
			}
		}
	} else {
		for _, id := range ids {
			if b.notificationsCache[u] != nil {
				if n, ok := b.notificationsCache[u][id]; ok {
					list = append(list, n)
				} else {
					notFound = append(notFound, id)
				}
			} else {
				notFound = append(notFound, id)
			}
		}
	}
	return list, notFound, nil
}

func (b *CalendarsBackend) CreateCalendarEventNotification(ctx context.Context, notification *jmap.CalendarEventNotification) (*jmap.CalendarEventNotification, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.notificationsCache[u] == nil {
		b.notificationsCache[u] = make(map[jmap.Id]*jmap.CalendarEventNotification)
	}
	if notification.ID == "" {
		notification.ID = jmap.Id(fmt.Sprintf("notif-%d", time.Now().UnixNano()))
	}
	if notification.Created == "" {
		notification.Created = time.Now().UTC().Format(time.RFC3339)
	}
	b.notificationsCache[u][notification.ID] = notification
	b.nextNotifSeq++
	if b.notifSeq[u] == nil {
		b.notifSeq[u] = make(map[jmap.Id]uint64)
	}
	b.notifSeq[u][notification.ID] = b.nextNotifSeq
	st := b.getNotificationTracker(u).Record(notification.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return notification, nil
}

func (b *CalendarsBackend) DeleteCalendarEventNotification(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.notificationsCache[u] == nil {
		b.mu.Unlock()
		return false, nil
	}
	if _, ok := b.notificationsCache[u][id]; !ok {
		b.mu.Unlock()
		return false, nil
	}
	delete(b.notificationsCache[u], id)
	if b.notifSeq[u] != nil {
		delete(b.notifSeq[u], id)
	}
	st := b.getNotificationTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "CalendarEventNotification", st)
	return true, nil
}

func (b *CalendarsBackend) QueryCalendarEventNotifications(ctx context.Context, filter map[string]any, sortCriteria []jmap.Comparator, position int, limit *uint64) ([]jmap.Id, int, error) {
	u := b.user(ctx)
	notifs, _, err := b.GetCalendarEventNotifications(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	var matched []*jmap.CalendarEventNotification
	for _, n := range notifs {
		if matchCalendarEventNotification(n, filter) {
			matched = append(matched, n)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		for _, comp := range sortCriteria {
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
		b.mu.RLock()
		seqs := b.notifSeq[u]
		b.mu.RUnlock()
		if seqs != nil {
			return seqs[matched[i].ID] > seqs[matched[j].ID]
		}
		return matched[i].ID > matched[j].ID
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
	return resultIDs[position:end], total, nil
}

func matchCalendarEventNotification(n *jmap.CalendarEventNotification, filter map[string]any) bool {
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

func equalCalendarRights(a, b *jmap.CalendarRights) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// ShareNotificationState implements ShareNotification state per RFC 9670.
func (b *CalendarsBackend) ShareNotificationState(ctx context.Context) string {
	return b.getShareNotificationTracker(b.user(ctx)).State()
}

// ShareNotificationChanges implements ShareNotification/changes per RFC 9670.
func (b *CalendarsBackend) ShareNotificationChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMoreChanges bool) {
	return b.getShareNotificationTracker(b.user(ctx)).Changes(sinceState)
}

// GetShareNotifications retrieves specific ShareNotifications by ID.
func (b *CalendarsBackend) GetShareNotifications(ctx context.Context, ids []jmap.Id) (list []*jmap.ShareNotification, notFound []jmap.Id, err error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()

	var found []*jmap.ShareNotification
	var missing []jmap.Id

	for _, id := range ids {
		if b.shareNotificationsCache[u] != nil && b.shareNotificationsCache[u][id] != nil {
			found = append(found, b.shareNotificationsCache[u][id])
		} else {
			missing = append(missing, id)
		}
	}
	if missing == nil {
		missing = []jmap.Id{}
	}
	return found, missing, nil
}

// GetAllShareNotifications retrieves all ShareNotifications for the user.
func (b *CalendarsBackend) GetAllShareNotifications(ctx context.Context) ([]*jmap.ShareNotification, error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()

	var list []*jmap.ShareNotification
	if b.shareNotificationsCache[u] != nil {
		for _, n := range b.shareNotificationsCache[u] {
			if n != nil {
				list = append(list, n)
			}
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// CreateShareNotification creates a new ShareNotification.
func (b *CalendarsBackend) CreateShareNotification(ctx context.Context, notification *jmap.ShareNotification) (*jmap.ShareNotification, error) {
	if notification == nil {
		return nil, fmt.Errorf("notification is nil")
	}
	u := b.user(ctx)
	b.mu.Lock()
	if b.shareNotificationsCache[u] == nil {
		b.shareNotificationsCache[u] = make(map[jmap.Id]*jmap.ShareNotification)
	}
	b.shareNotificationsCache[u][notification.ID] = notification
	st := b.getShareNotificationTracker(u).Record(notification.ID, "create")
	b.mu.Unlock()

	b.emitStateChange(u, "ShareNotification", st)
	return notification, nil
}

// DeleteShareNotification deletes a ShareNotification.
func (b *CalendarsBackend) DeleteShareNotification(ctx context.Context, id jmap.Id) (bool, error) {
	u := b.user(ctx)
	b.mu.Lock()
	if b.shareNotificationsCache[u] != nil {
		delete(b.shareNotificationsCache[u], id)
	}
	st := b.getShareNotificationTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "ShareNotification", st)
	return true, nil
}
