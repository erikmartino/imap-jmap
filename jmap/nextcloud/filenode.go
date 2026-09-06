package nextcloud

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// FileNodeBackend implements jmap.FileNodeBackend backed by Nextcloud WebDAV via github.com/emersion/go-webdav.
type FileNodeBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmap.Broadcaster

	nodeTrackers map[string]*jmap.ChangeTracker
	nodesCache   map[string]map[jmap.Id]*jmap.FileNode
	nextID       uint64
}

var _ jmap.FileNodeBackend = (*FileNodeBackend)(nil)

// NewFileNodeBackend initializes a new Nextcloud-backed FileNodeBackend.
func NewFileNodeBackend(client *Client) *FileNodeBackend {
	return &FileNodeBackend{
		client:       client,
		nodeTrackers: make(map[string]*jmap.ChangeTracker),
		nodesCache:   make(map[string]map[jmap.Id]*jmap.FileNode),
	}
}

func (b *FileNodeBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *FileNodeBackend) emitStateChange(u, typeName, newState string) {
	bc := b.broadcaster
	if bc != nil {
		accountID := jmap.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
		}
	}
}

func (b *FileNodeBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	return u
}

func (b *FileNodeBackend) getNodeTracker(u string) *jmap.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.nodeTrackers[u] == nil {
		b.nodeTrackers[u] = jmap.NewChangeTracker(1000)
	}
	return b.nodeTrackers[u]
}

func (b *FileNodeBackend) FileNodeState(ctx context.Context) string {
	return b.getNodeTracker(b.user(ctx)).State()
}

func (b *FileNodeBackend) FileNodeChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	return b.getNodeTracker(b.user(ctx)).Changes(sinceState)
}

func (b *FileNodeBackend) GetAllFileNodes(ctx context.Context) ([]*jmap.FileNode, error) {
	nodes, _, err := b.GetFileNodes(ctx, nil)
	return nodes, err
}

func (b *FileNodeBackend) GetFileNodes(ctx context.Context, ids []jmap.Id) ([]*jmap.FileNode, []jmap.Id, error) {
	fs, u, err := b.client.WebDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	fis, err := fs.ReadDir(ctx, "", false)
	if err != nil {
		return nil, nil, nil
	}

	b.mu.Lock()
	if b.nodesCache[u] == nil {
		b.nodesCache[u] = make(map[jmap.Id]*jmap.FileNode)
	}

	for _, fi := range fis {
		cleanPath := strings.TrimRight(path.Clean(fi.Path), "/")
		if cleanPath == "" || cleanPath == "." || cleanPath == "/remote.php/webdav" || strings.HasSuffix(cleanPath, "/webdav") {
			continue
		}
		nodeName := path.Base(fi.Path)
		if nodeName == "" || nodeName == "." || nodeName == ".." || nodeName == "webdav" {
			continue
		}

		var existing *jmap.FileNode
		for _, n := range b.nodesCache[u] {
			if n.Name == nodeName {
				existing = n
				break
			}
		}

		if existing == nil {
			b.nextID++
			nodeID := jmap.Id(fmt.Sprintf("fn-%d", b.nextID))
			nodeType := "file"
			if fi.IsDir {
				nodeType = "folder"
			}
			nowStr := fi.ModTime.Format(time.RFC3339)
			if nowStr == "" {
				nowStr = time.Now().UTC().Format(time.RFC3339)
			}
			existing = &jmap.FileNode{
				ID:        nodeID,
				Name:      nodeName,
				Type:      nodeType,
				Size:      uint64(fi.Size),
				IsFolder:  fi.IsDir,
				CreatedAt: nowStr,
				UpdatedAt: nowStr,
			}
			b.nodesCache[u][nodeID] = existing
		} else {
			if fi.IsDir {
				existing.IsFolder = true
			}
			if !fi.ModTime.IsZero() {
				existing.UpdatedAt = fi.ModTime.Format(time.RFC3339)
			}
		}
	}

	userCache := b.nodesCache[u]
	var list []*jmap.FileNode
	var notFound []jmap.Id

	if len(ids) > 0 {
		for _, id := range ids {
			if n, ok := userCache[id]; ok {
				list = append(list, n)
			} else {
				notFound = append(notFound, id)
			}
		}
	} else {
		for _, n := range userCache {
			list = append(list, n)
		}
		sort.Slice(list, func(i, j int) bool {
			return list[i].ID < list[j].ID
		})
	}
	b.mu.Unlock()

	return list, notFound, nil
}

func (b *FileNodeBackend) CreateFileNode(ctx context.Context, node *jmap.FileNode) (*jmap.FileNode, error) {
	if node == nil {
		return nil, fmt.Errorf("node is nil")
	}
	fs, u, err := b.client.WebDAV(ctx)
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	if node.ID == "" {
		b.nextID++
		node.ID = jmap.Id(fmt.Sprintf("fn-%d", b.nextID))
	}
	b.mu.Unlock()
	nowStr := time.Now().UTC().Format(time.RFC3339)
	if node.CreatedAt == "" {
		node.CreatedAt = nowStr
	}
	if node.UpdatedAt == "" {
		node.UpdatedAt = nowStr
	}

	if node.IsFolder || node.Type == "folder" || node.Type == "directory" {
		node.IsFolder = true
		_ = fs.Mkdir(ctx, node.Name)
	} else {
		wc, err := fs.Create(ctx, node.Name)
		if err == nil && wc != nil {
			_ = wc.Close()
		}
	}

	b.mu.Lock()
	if b.nodesCache[u] == nil {
		b.nodesCache[u] = make(map[jmap.Id]*jmap.FileNode)
	}
	action := "create"
	if _, exists := b.nodesCache[u][node.ID]; exists {
		action = "update"
	}
	b.nodesCache[u][node.ID] = node
	st := b.getNodeTracker(u).Record(node.ID, action)
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return node, nil
}

func (b *FileNodeBackend) UpdateFileNode(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.FileNode, error) {
	nodes, notFound, err := b.GetFileNodes(ctx, []jmap.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(nodes) == 0 {
		return nil, jmap.ErrNotFound
	}

	u := b.user(ctx)
	b.mu.Lock()
	node := b.nodesCache[u][id]
	if node == nil {
		node = nodes[0]
	}
	oldName := node.Name
	for k, v := range patch {
		switch k {
		case "name":
			if s, ok := v.(string); ok && s != "" {
				node.Name = s
			}
		case "type":
			if s, ok := v.(string); ok {
				node.Type = s
			}
		case "isFolder":
			if bVal, ok := v.(bool); ok {
				node.IsFolder = bVal
			}
		case "size":
			if f, ok := v.(float64); ok {
				node.Size = uint64(f)
			}
		case "parentId":
			if s, ok := v.(string); ok {
				pid := jmap.Id(s)
				node.ParentID = &pid
			} else if v == nil {
				node.ParentID = nil
			}
		case "blobId":
			if s, ok := v.(string); ok {
				bid := jmap.Id(s)
				node.BlobID = &bid
			} else if v == nil {
				node.BlobID = nil
			}
		}
	}
	node.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b.nodesCache[u][id] = node
	st := b.getNodeTracker(u).Record(id, "update")
	b.mu.Unlock()

	if node.Name != oldName && oldName != "" {
		if fs, _, err := b.client.WebDAV(ctx); err == nil {
			_ = fs.Move(ctx, oldName, node.Name, nil)
		}
	}

	b.emitStateChange(u, "FileNode", st)
	return node, nil
}

func (b *FileNodeBackend) DeleteFileNode(ctx context.Context, id jmap.Id) (bool, error) {
	nodes, notFound, err := b.GetFileNodes(ctx, []jmap.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(nodes) == 0 {
		return false, nil
	}

	fs, u, err := b.client.WebDAV(ctx)
	if err != nil {
		return false, err
	}

	targetName := nodes[0].Name
	if targetName == "" {
		targetName = string(id)
	}

	_ = fs.RemoveAll(ctx, targetName)

	b.mu.Lock()
	if b.nodesCache[u] != nil {
		delete(b.nodesCache[u], id)
	}
	st := b.getNodeTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return true, nil
}

func (b *FileNodeBackend) QueryFileNodes(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	nodes, _, err := b.GetFileNodes(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	var matching []jmap.Id
	for _, n := range nodes {
		if filter != nil {
			if name, ok := filter["name"].(string); ok && name != "" {
				if !strings.Contains(strings.ToLower(n.Name), strings.ToLower(name)) {
					continue
				}
			}
			if isF, ok := filter["isFolder"].(bool); ok {
				if n.IsFolder != isF {
					continue
				}
			}
			if typeVal, ok := filter["type"].(string); ok && typeVal != "" {
				if !strings.EqualFold(n.Type, typeVal) {
					continue
				}
			}
			if pid, ok := filter["parentId"].(string); ok {
				if n.ParentID == nil || string(*n.ParentID) != pid {
					continue
				}
			}
		}
		matching = append(matching, n.ID)
	}

	// Stable order for deterministic pagination
	sort.Slice(matching, func(i, j int) bool {
		return matching[i] < matching[j]
	})

	total := len(matching)
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
		ids = append(ids, matching[i])
	}

	return ids, total, nil
}
