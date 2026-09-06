package managesieve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/foxcpp/go-sieve"

	"imap-jmap/jmap"
)

// Backend implements jmap.SieveBackend by communicating with a ManageSieve (RFC 5804) server.
type Backend struct {
	addr        string
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	trackers    map[string]*jmap.ChangeTracker
	broadcaster *jmap.Broadcaster
	nameToID    map[string]map[string]jmap.Id // user -> scriptName -> jmap.Id
	idToName    map[string]map[jmap.Id]string // user -> jmap.Id -> scriptName
	idCounter   uint64
}

var _ jmap.SieveBackend = (*Backend)(nil)

// NewBackend creates a new ManageSieve-backed SieveBackend pointing at the given address.
func NewBackend(addr string) *Backend {
	return &Backend{
		addr:     addr,
		trackers: make(map[string]*jmap.ChangeTracker),
		nameToID: make(map[string]map[string]jmap.Id),
		idToName: make(map[string]map[jmap.Id]string),
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
func (b *Backend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *Backend) emitStateChange(u, newState string) {
	if b.broadcaster != nil && u != "" {
		accID := jmap.AccountIDForSubject(u)
		b.broadcaster.PublishStateChange(accID, "SieveScript", newState)
	}
}

func (b *Backend) getTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	t := b.trackers[u]
	if t == nil {
		t = jmap.NewChangeTracker(1000)
		b.trackers[u] = t
	}
	return t
}

func (b *Backend) userAndPass(ctx context.Context) (string, string) {
	creds, ok := jmap.CredentialsFromContext(ctx)
	if ok && creds.Username != "" {
		return creds.Username, creds.Password
	}
	subj, _ := jmap.SubjectFromContext(ctx)
	if subj != "" {
		return subj, subj
	}
	accID, ok := jmap.AccountIDFromContext(ctx)
	if ok && accID != "" {
		if s, valid := jmap.SubjectForAccountID(accID); valid && s != "" {
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

func (b *Backend) idForName(u, name string) jmap.Id {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nameToID[u] == nil {
		b.nameToID[u] = make(map[string]jmap.Id)
		b.idToName[u] = make(map[jmap.Id]string)
	}
	if id, exists := b.nameToID[u][name]; exists {
		return id
	}
	b.idCounter++
	id := jmap.Id(fmt.Sprintf("sieve-%d", b.idCounter))
	b.nameToID[u][name] = id
	b.idToName[u][id] = name
	return id
}

func (b *Backend) nameForID(u string, id jmap.Id) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.idToName[u] != nil {
		if name, exists := b.idToName[u][id]; exists {
			return name
		}
	}
	// Fallback to id as string
	return string(id)
}

func (b *Backend) registerID(u, name string, id jmap.Id) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nameToID[u] == nil {
		b.nameToID[u] = make(map[string]jmap.Id)
		b.idToName[u] = make(map[jmap.Id]string)
	}
	b.nameToID[u][name] = id
	b.idToName[u][id] = name
}

// SieveScriptState returns the current state token for the user.
func (b *Backend) SieveScriptState(ctx context.Context) string {
	user, _ := b.userAndPass(ctx)
	return b.getTracker(user).State()
}

// SieveScriptChanges returns created, updated, destroyed scripts since sinceState.
func (b *Backend) SieveScriptChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
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
func (b *Backend) GetSieveScripts(ctx context.Context, ids []jmap.Id) ([]*jmap.SieveScript, []jmap.Id, error) {
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
		var list []*jmap.SieveScript
		for _, info := range infos {
			content, _ := c.GetScript(info.Name)
			id := b.idForName(user, info.Name)
			list = append(list, &jmap.SieveScript{
				ID:       id,
				Name:     info.Name,
				Content:  content,
				IsActive: info.Active,
				IsValid:  true,
			})
		}
		return list, nil, nil
	}

	var list []*jmap.SieveScript
	var notFound []jmap.Id

	for _, id := range ids {
		name := b.nameForID(user, id)
		info, exists := infoMap[name]
		if !exists {
			// Try matching by id directly as name
			info, exists = infoMap[string(id)]
			if exists {
				name = string(id)
			}
		}
		if !exists {
			notFound = append(notFound, id)
			continue
		}

		content, gErr := c.GetScript(name)
		if gErr != nil {
			notFound = append(notFound, id)
			continue
		}

		list = append(list, &jmap.SieveScript{
			ID:       id,
			Name:     name,
			Content:  content,
			IsActive: info.Active,
			IsValid:  true,
		})
	}

	return list, notFound, nil
}

// GetAllSieveScripts fetches all Sieve scripts for the user.
func (b *Backend) GetAllSieveScripts(ctx context.Context) ([]*jmap.SieveScript, error) {
	list, _, err := b.GetSieveScripts(ctx, nil)
	return list, err
}

// CreateSieveScript uploads and optionally activates a new Sieve script.
func (b *Backend) CreateSieveScript(ctx context.Context, script *jmap.SieveScript) (*jmap.SieveScript, error) {
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
	if name == "" {
		name = string(script.ID)
	}
	if name == "" {
		name = fmt.Sprintf("script-%d", b.idCounter+1)
	}

	if err := c.PutScript(name, script.Content); err != nil {
		return nil, err
	}

	if script.IsActive {
		if err := c.SetActive(name); err != nil {
			return nil, err
		}
	}

	if script.ID == "" {
		script.ID = b.idForName(user, name)
	} else {
		b.registerID(user, name, script.ID)
	}
	script.Name = name

	st := b.getTracker(user).Record(script.ID, "create")
	b.emitStateChange(user, st)

	return script, nil
}

// UpdateSieveScript updates an existing Sieve script.
func (b *Backend) UpdateSieveScript(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.SieveScript, error) {
	c, user, err := b.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	oldName := b.nameForID(user, id)
	content, err := c.GetScript(oldName)
	if err != nil {
		return nil, fmt.Errorf("sieve script %s: %w", id, jmap.ErrNotFound)
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

	if newName != oldName {
		if err := c.PutScript(newName, content); err != nil {
			return nil, err
		}
		_ = c.DeleteScript(oldName)
		b.registerID(user, newName, id)
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

	st := b.getTracker(user).Record(id, "update")
	b.emitStateChange(user, st)

	return &jmap.SieveScript{
		ID:       id,
		Name:     newName,
		Content:  content,
		IsActive: isActive,
		IsValid:  true,
	}, nil
}

// DeleteSieveScript deletes a Sieve script.
func (b *Backend) DeleteSieveScript(ctx context.Context, id jmap.Id) (bool, error) {
	c, user, err := b.dial(ctx)
	if err != nil {
		return false, err
	}
	defer c.Close()

	name := b.nameForID(user, id)
	// If active, deactivate first per RFC 5804 rule that active script cannot be deleted directly
	infos, _ := c.ListScripts()
	for _, inf := range infos {
		if inf.Name == name && inf.Active {
			_ = c.SetActive("")
			break
		}
	}

	if err := c.DeleteScript(name); err != nil {
		return false, nil
	}

	st := b.getTracker(user).Record(id, "destroy")
	b.emitStateChange(user, st)

	return true, nil
}

// QuerySieveScripts filters scripts by name, isActive, isValid.
func (b *Backend) QuerySieveScripts(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	all, err := b.GetAllSieveScripts(ctx)
	if err != nil {
		return nil, 0, err
	}

	nameFilter, _ := filter["name"].(string)
	isActiveFilter, hasActiveFilter := filter["isActive"].(bool)
	isValidFilter, hasValidFilter := filter["isValid"].(bool)

	var matched []*jmap.SieveScript
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
