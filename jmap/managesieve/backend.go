package managesieve

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/foxcpp/go-sieve"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmappush"
	"imap-jmap/jmap/jmapsieve"
)

// Backend implements jmapsieve.SieveBackend by communicating with a ManageSieve (RFC 5804) server.
type Backend struct {
	addr        string
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	trackers    map[string]*jmappush.ChangeTracker
	broadcaster *jmappush.Broadcaster
	movedIDs    map[string]map[jmapcore.Id]jmapcore.Id // user -> oldID -> newID (transient redirection)
}

var _ jmapsieve.SieveBackend = (*Backend)(nil)

// NewBackend creates a new ManageSieve-backed SieveBackend pointing at the given address.
func NewBackend(addr string) *Backend {
	return &Backend{
		addr:     addr,
		trackers: make(map[string]*jmappush.ChangeTracker),
		movedIDs: make(map[string]map[jmapcore.Id]jmapcore.Id),
	}
}

// NewEmbeddedBackend spins up an in-process ManageSieve server and returns a live Backend connected to it.
func NewEmbeddedBackend(usernames ...string) (*EmbeddedServer, *Backend, func()) {
	srv, err := NewEmbeddedServer()
	if err != nil {
		panic(fmt.Sprintf("failed to start embedded ManageSieve server: %v", err))
	}
	b := NewBackend(srv.Addr())
	cleanup := func() {
		_ = srv.Close()
	}
	return srv, b, cleanup
}

// SetBroadcaster connects an event Broadcaster for push notifications.
func (b *Backend) SetBroadcaster(bc *jmappush.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *Backend) emitStateChange(u, newState string) {
	if b.broadcaster != nil && u != "" {
		accID := jmapauth.AccountIDForSubject(u)
		b.broadcaster.PublishStateChange(accID, "SieveScript", newState)
	}
}

func (b *Backend) getTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	t := b.trackers[u]
	if t == nil {
		t = jmappush.NewChangeTracker(1000)
		b.trackers[u] = t
	}
	return t
}

func (b *Backend) userAndPass(ctx context.Context) (string, string) {
	creds, ok := jmapauth.CredentialsFromContext(ctx)
	if ok && creds.Username != "" {
		return creds.Username, creds.Password
	}
	subj, _ := jmapauth.SubjectFromContext(ctx)
	if subj != "" {
		return subj, subj
	}
	accID, ok := jmapauth.AccountIDFromContext(ctx)
	if ok && accID != "" {
		if s, valid := jmapauth.SubjectForAccountID(accID); valid && s != "" {
			return s, s
		}
		return accID, accID
	}
	return "user@example.com", "user@example.com"
}

func (b *Backend) dial(ctx context.Context) (*Client, string, error) {
	user, pass := b.userAndPass(ctx)
	c, err := Dial(b.addr)
	if err != nil {
		return nil, user, fmt.Errorf("ManageSieve dial failed: %w", err)
	}
	if err := c.Authenticate(user, pass); err != nil {
		_ = c.Close()
		return nil, user, fmt.Errorf("ManageSieve authenticate failed: %w", err)
	}
	return c, user, nil
}

// SieveScriptIDForName returns the deterministic JMAP Id for a Sieve script name.
// Per RFC 9661 §2.1 and RFC 8620 §1.2, IDs must be valid URL and Filename Safe Base64 strings.
func SieveScriptIDForName(name string) jmapcore.Id {
	return jmapcore.Id("s-" + base64.RawURLEncoding.EncodeToString([]byte(name)))
}

// NameForSieveScriptID decodes the Sieve script name from a deterministic JMAP Id.
// If the ID is not formatted as "s-<base64>", it falls back to the ID string itself.
func NameForSieveScriptID(id jmapcore.Id) string {
	s := string(id)
	if strings.HasPrefix(s, "s-") {
		if decoded, err := base64.RawURLEncoding.DecodeString(s[2:]); err == nil {
			return string(decoded)
		}
	}
	return s
}

func (b *Backend) resolveID(u string, id jmapcore.Id) jmapcore.Id {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if userMoved, ok := b.movedIDs[u]; ok {
		if target, moved := userMoved[id]; moved {
			return target
		}
	}
	return id
}

func (b *Backend) recordMove(u string, oldID, newID jmapcore.Id) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.movedIDs[u] == nil {
		b.movedIDs[u] = make(map[jmapcore.Id]jmapcore.Id)
	}
	b.movedIDs[u][oldID] = newID
}

// SieveScriptState returns the current state token for the user.
func (b *Backend) SieveScriptState(ctx context.Context) string {
	user, _ := b.userAndPass(ctx)
	return b.getTracker(user).State()
}

// SieveScriptChanges returns created, updated, destroyed scripts since sinceState.
func (b *Backend) SieveScriptChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMore bool) {
	user, _ := b.userAndPass(ctx)
	return b.getTracker(user).Changes(sinceState)
}

// ValidateSieveScript validates Sieve script syntax using go-sieve.
func (b *Backend) ValidateSieveScript(ctx context.Context, content string) (bool, string) {
	if strings.TrimSpace(content) == "" {
		return false, "sieve script content is empty"
	}
	_, err := sieve.Load(strings.NewReader(content), sieve.DefaultOptions())
	if err != nil {
		return false, err.Error()
	}
	return true, ""
}

// GetSieveScripts fetches specific Sieve scripts by ID.
func (b *Backend) GetSieveScripts(ctx context.Context, ids []jmapcore.Id) ([]*jmapsieve.SieveScript, []jmapcore.Id, error) {
	c, user, err := b.dial(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()

	infos, err := c.ListScripts()
	if err != nil {
		return nil, nil, err
	}

	infoMap := make(map[string]ScriptInfo, len(infos))
	for _, info := range infos {
		infoMap[info.Name] = info
	}

	if len(ids) == 0 {
		var list []*jmapsieve.SieveScript
		for _, info := range infos {
			content, _ := c.GetScript(info.Name)
			id := SieveScriptIDForName(info.Name)
			list = append(list, &jmapsieve.SieveScript{
				ID:       id,
				Name:     info.Name,
				Content:  content,
				IsActive: info.Active,
				IsValid:  true,
			})
		}
		return list, nil, nil
	}

	var list []*jmapsieve.SieveScript
	var notFound []jmapcore.Id

	for _, rawID := range ids {
		resolvedID := b.resolveID(user, rawID)
		name := NameForSieveScriptID(resolvedID)
		info, exists := infoMap[name]
		if !exists {
			// Try matching by resolvedID or rawID directly as name
			info, exists = infoMap[string(resolvedID)]
			if exists {
				name = string(resolvedID)
			} else {
				info, exists = infoMap[string(rawID)]
				if exists {
					name = string(rawID)
				}
			}
		}
		if !exists {
			notFound = append(notFound, rawID)
			continue
		}

		content, gErr := c.GetScript(name)
		if gErr != nil {
			notFound = append(notFound, rawID)
			continue
		}

		list = append(list, &jmapsieve.SieveScript{
			ID:       SieveScriptIDForName(name),
			Name:     name,
			Content:  content,
			IsActive: info.Active,
			IsValid:  true,
		})
	}

	return list, notFound, nil
}

// GetAllSieveScripts fetches all Sieve scripts for the user.
func (b *Backend) GetAllSieveScripts(ctx context.Context) ([]*jmapsieve.SieveScript, error) {
	list, _, err := b.GetSieveScripts(ctx, nil)
	return list, err
}

// CreateSieveScript uploads and optionally activates a new Sieve script.
func (b *Backend) CreateSieveScript(ctx context.Context, script *jmapsieve.SieveScript) (*jmapsieve.SieveScript, error) {
	if script == nil {
		return nil, fmt.Errorf("script is nil")
	}
	isValid, errDetail := b.ValidateSieveScript(ctx, script.Content)
	if !isValid {
		return nil, fmt.Errorf("invalid sieve script: %s", errDetail)
	}
	script.IsValid = true

	c, user, err := b.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	name := script.Name
	if name == "" && script.ID != "" {
		name = NameForSieveScriptID(script.ID)
	}
	if name == "" {
		name = "default"
	}

	if err := c.PutScript(name, script.Content); err != nil {
		return nil, err
	}

	if script.IsActive {
		if err := c.SetActive(name); err != nil {
			return nil, err
		}
	}

	script.ID = SieveScriptIDForName(name)
	script.Name = name

	st := b.getTracker(user).Record(script.ID, "create")
	b.emitStateChange(user, st)

	return script, nil
}

// UpdateSieveScript updates an existing Sieve script.
func (b *Backend) UpdateSieveScript(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapsieve.SieveScript, error) {
	c, user, err := b.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	resolvedID := b.resolveID(user, id)
	oldName := NameForSieveScriptID(resolvedID)
	content, err := c.GetScript(oldName)
	if err != nil {
		if string(resolvedID) != oldName {
			if c2, err2 := c.GetScript(string(resolvedID)); err2 == nil {
				oldName = string(resolvedID)
				content = c2
				err = nil
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("sieve script %s: %w", id, jmapcore.ErrNotFound)
	}

	newName := oldName
	if n, ok := patch["name"].(string); ok && n != "" {
		newName = n
	}

	if cPatch, ok := patch["content"].(string); ok {
		isValid, errDetail := b.ValidateSieveScript(ctx, cPatch)
		if !isValid {
			return nil, fmt.Errorf("invalid sieve script: %s", errDetail)
		}
		content = cPatch
	}

	newID := SieveScriptIDForName(newName)

	if newName != oldName {
		wasActive := false
		infos, _ := c.ListScripts()
		for _, inf := range infos {
			if inf.Name == oldName && inf.Active {
				wasActive = true
				break
			}
		}

		if err := c.PutScript(newName, content); err != nil {
			return nil, err
		}
		if wasActive {
			_ = c.SetActive(newName)
		}
		_ = c.DeleteScript(oldName)
		b.recordMove(user, id, newID)
		if resolvedID != id {
			b.recordMove(user, resolvedID, newID)
		}
	} else if _, ok := patch["content"]; ok {
		if err := c.PutScript(newName, content); err != nil {
			return nil, err
		}
	}

	isActive := false
	infos, _ := c.ListScripts()
	for _, inf := range infos {
		if inf.Name == newName && inf.Active {
			isActive = true
			break
		}
	}

	if act, ok := patch["isActive"].(bool); ok {
		if act {
			if err := c.SetActive(newName); err != nil {
				return nil, err
			}
			isActive = true
		} else if isActive {
			_ = c.SetActive("")
			isActive = false
		}
	}

	st := b.getTracker(user).Record(newID, "update")
	b.emitStateChange(user, st)

	return &jmapsieve.SieveScript{
		ID:       newID,
		Name:     newName,
		Content:  content,
		IsActive: isActive,
		IsValid:  true,
	}, nil
}

// DeleteSieveScript deletes a Sieve script.
func (b *Backend) DeleteSieveScript(ctx context.Context, id jmapcore.Id) (bool, error) {
	c, user, err := b.dial(ctx)
	if err != nil {
		return false, err
	}
	defer c.Close()

	resolvedID := b.resolveID(user, id)
	name := NameForSieveScriptID(resolvedID)

	// If active, deactivate first per RFC 5804 rule that active script cannot be deleted directly
	infos, _ := c.ListScripts()
	var found bool
	for _, inf := range infos {
		if inf.Name == name || inf.Name == string(resolvedID) {
			name = inf.Name
			found = true
			if inf.Active {
				_ = c.SetActive("")
			}
			break
		}
	}
	if !found {
		return false, nil
	}

	if err := c.DeleteScript(name); err != nil {
		return false, nil
	}

	st := b.getTracker(user).Record(id, "destroy")
	b.emitStateChange(user, st)

	return true, nil
}

// QuerySieveScripts filters scripts by name, isActive, isValid.
func (b *Backend) QuerySieveScripts(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	all, err := b.GetAllSieveScripts(ctx)
	if err != nil {
		return nil, 0, err
	}

	nameFilter, _ := filter["name"].(string)
	isActiveFilter, hasActiveFilter := filter["isActive"].(bool)
	isValidFilter, hasValidFilter := filter["isValid"].(bool)

	var matched []*jmapsieve.SieveScript
	for _, s := range all {
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

	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Name != matched[j].Name {
			return matched[i].Name < matched[j].Name
		}
		return matched[i].ID < matched[j].ID
	})

	total := len(matched)
	position = jmapcore.NormalizePosition(position, total)
	if position >= total {
		return []jmapcore.Id{}, total, nil
	}

	end := total
	if limit != nil && position+int(*limit) < end {
		end = position + int(*limit)
	}

	ids := make([]jmapcore.Id, 0, end-position)
	for i := position; i < end; i++ {
		ids = append(ids, matched[i].ID)
	}

	return ids, total, nil
}
