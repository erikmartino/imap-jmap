package nextcloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapfilenode"
	"imap-jmap/jmap/jmappush"
)

// ErrForbidden is returned when the operation is not permitted or user is unauthenticated.
var ErrForbidden = errors.New("forbidden")

// FileNodeBackend implements jmapfilenode.FileNodeBackend backed by Nextcloud WebDAV via github.com/emersion/go-webdav.
type FileNodeBackend struct {
	client      *Client
	mu          sync.RWMutex
	trackersMu  sync.Mutex
	broadcaster *jmappush.Broadcaster
	blobBackend *BlobBackend

	nodeTrackers map[string]*jmappush.ChangeTracker
	nodesCache   map[string]map[jmapcore.Id]*jmapfilenode.FileNode
	pathToID     map[string]map[string]jmapcore.Id
	idToPath     map[string]map[jmapcore.Id]string
	nextID       uint64
}

var _ jmapfilenode.FileNodeBackend = (*FileNodeBackend)(nil)

// NewFileNodeBackend initializes a new Nextcloud-backed FileNodeBackend.
func NewFileNodeBackend(client *Client) *FileNodeBackend {
	return &FileNodeBackend{
		client:       client,
		nodeTrackers: make(map[string]*jmappush.ChangeTracker),
		nodesCache:   make(map[string]map[jmapcore.Id]*jmapfilenode.FileNode),
		pathToID:     make(map[string]map[string]jmapcore.Id),
		idToPath:     make(map[string]map[jmapcore.Id]string),
	}
}

// SetBlobBackend sets the associated BlobBackend for content sync.
func (b *FileNodeBackend) SetBlobBackend(bb *BlobBackend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.blobBackend = bb
}

// BlobBackend returns the associated BlobBackend.
func (b *FileNodeBackend) BlobBackend() *BlobBackend {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.blobBackend
}

func (b *FileNodeBackend) SetBroadcaster(bc *jmappush.Broadcaster) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcaster = bc
}

func (b *FileNodeBackend) emitStateChange(u, typeName, newState string) {
	b.mu.RLock()
	bc := b.broadcaster
	b.mu.RUnlock()
	if bc != nil {
		accountID := jmapauth.AccountIDForSubject(u)
		bc.PublishStateChange(accountID, typeName, newState)
		if accountID != u {
			bc.PublishStateChange(u, typeName, newState)
		}
	}
}

func (b *FileNodeBackend) user(ctx context.Context) string {
	u, _ := b.client.getUserAndPass(ctx)
	if u != "" {
		return u
	}
	if sub, ok := jmapauth.SubjectFromContext(ctx); ok && sub != "" {
		return sub
	}
	return ""
}

func (b *FileNodeBackend) getNodeTracker(u string) *jmappush.ChangeTracker {
	b.trackersMu.Lock()
	defer b.trackersMu.Unlock()
	if b.nodeTrackers[u] == nil {
		b.nodeTrackers[u] = jmappush.NewChangeTracker(1000)
	}
	return b.nodeTrackers[u]
}

func (b *FileNodeBackend) FileNodeState(ctx context.Context) string {
	return b.getNodeTracker(b.user(ctx)).State()
}

func (b *FileNodeBackend) FileNodeChanges(ctx context.Context, sinceState string) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	return b.getNodeTracker(b.user(ctx)).Changes(sinceState)
}

func cleanRelPath(p string) string {
	clean := path.Clean(p)
	clean = strings.TrimPrefix(clean, "/remote.php/webdav")
	clean = strings.TrimPrefix(clean, "remote.php/webdav")
	clean = strings.Trim(clean, "/")
	return clean
}

func mimeTypeForName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".txt", ".text":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".html", ".htm":
		return "text/html"
	case ".json":
		return "application/json"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".csv":
		return "text/csv"
	default:
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
		return "application/octet-stream"
	}
}

func (b *FileNodeBackend) ensureMapsLocked(u string) {
	if b.nodesCache[u] == nil {
		b.nodesCache[u] = make(map[jmapcore.Id]*jmapfilenode.FileNode)
	}
	if b.pathToID[u] == nil {
		b.pathToID[u] = make(map[string]jmapcore.Id)
	}
	if b.idToPath[u] == nil {
		b.idToPath[u] = make(map[jmapcore.Id]string)
	}
}

// syncFromWebDAV walks Nextcloud WebDAV and populates the node cache, ensuring all existing files are discovered.
func (b *FileNodeBackend) syncFromWebDAV(ctx context.Context, u string) error {
	fs, _, err := b.client.WebDAV(ctx)
	if err != nil {
		return err
	}

	type scanItem struct {
		relPath  string
		parentID *jmapcore.Id
	}
	queue := []scanItem{{relPath: "", parentID: nil}}
	visited := make(map[string]bool)
	visited[""] = true

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		fis, err := fs.ReadDir(ctx, item.relPath, false)
		if err != nil {
			continue
		}

		for _, fi := range fis {
			clean := cleanRelPath(fi.Path)
			if clean == item.relPath || clean == "" || clean == "." || strings.HasPrefix(clean, ".blobs") || strings.HasPrefix(path.Base(clean), ".") {
				continue
			}
			if visited[clean] {
				continue
			}
			visited[clean] = true

			nodeName := path.Base(clean)
			if nodeName == "" || nodeName == "." || nodeName == ".." || nodeName == "webdav" {
				continue
			}

			b.mu.Lock()
			b.ensureMapsLocked(u)

			nodeID, exists := b.pathToID[u][clean]
			if !exists {
				b.nextID++
				nodeID = jmapcore.Id(fmt.Sprintf("fn-%d", b.nextID))
				b.pathToID[u][clean] = nodeID
				b.idToPath[u][nodeID] = clean
			}

			nowStr := fi.ModTime.Format(time.RFC3339)
			if nowStr == "" {
				nowStr = time.Now().UTC().Format(time.RFC3339)
			}

			var node *jmapfilenode.FileNode
			if fi.IsDir {
				node = &jmapfilenode.FileNode{
					ID:        nodeID,
					Name:      nodeName,
					ParentID:  item.parentID,
					Type:      "folder",
					IsFolder:  true,
					CreatedAt: nowStr,
					UpdatedAt: nowStr,
				}
				existing := b.nodesCache[u][nodeID]
				if existing != nil && existing.CreatedAt != "" {
					node.CreatedAt = existing.CreatedAt
				}
				b.nodesCache[u][nodeID] = node
				b.mu.Unlock()

				queue = append(queue, scanItem{relPath: clean, parentID: &nodeID})
			} else {
				mimeType := mimeTypeForName(nodeName)
				h := sha256.Sum256([]byte(u + ":" + clean))
				blobID := jmapcore.Id(hex.EncodeToString(h[:]))

				node = &jmapfilenode.FileNode{
					ID:        nodeID,
					Name:      nodeName,
					ParentID:  item.parentID,
					BlobID:    &blobID,
					Size:      uint64(fi.Size),
					Type:      mimeType,
					IsFolder:  false,
					CreatedAt: nowStr,
					UpdatedAt: nowStr,
				}

				existing := b.nodesCache[u][nodeID]
				if existing != nil {
					if existing.Size > 0 && node.Size == 0 {
						node.Size = existing.Size
					}
					if existing.Type != "" {
						node.Type = existing.Type
					}
					if existing.BlobID != nil {
						node.BlobID = existing.BlobID
					}
					if existing.CreatedAt != "" {
						node.CreatedAt = existing.CreatedAt
					}
				}
				b.nodesCache[u][nodeID] = node
				b.mu.Unlock()
			}
		}
	}

	return nil
}

// GetFileByBlobID returns the WebDAV path and content of a file matching blobID.
func (b *FileNodeBackend) GetFileByBlobID(ctx context.Context, blobID string) (string, string, []byte, error) {
	u := b.user(ctx)
	b.mu.RLock()
	defer b.mu.RUnlock()

	userCache := b.nodesCache[u]
	if userCache == nil {
		return "", "", nil, jmapcore.ErrNotFound
	}

	for id, n := range userCache {
		if n.BlobID != nil && string(*n.BlobID) == blobID {
			rel := b.idToPath[u][id]
			return rel, n.Type, nil, nil
		}
	}
	return "", "", nil, jmapcore.ErrNotFound
}

func (b *FileNodeBackend) GetAllFileNodes(ctx context.Context) ([]*jmapfilenode.FileNode, error) {
	nodes, _, err := b.GetFileNodes(ctx, nil)
	return nodes, err
}

func (b *FileNodeBackend) GetFileNodes(ctx context.Context, ids []jmapcore.Id) ([]*jmapfilenode.FileNode, []jmapcore.Id, error) {
	u := b.user(ctx)
	if u == "" {
		return nil, ids, ErrForbidden
	}
	_ = b.syncFromWebDAV(ctx, u)

	b.mu.RLock()
	defer b.mu.RUnlock()

	userCache := b.nodesCache[u]
	if userCache == nil {
		userCache = make(map[jmapcore.Id]*jmapfilenode.FileNode)
	}

	var list []*jmapfilenode.FileNode
	var notFound []jmapcore.Id

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

	return list, notFound, nil
}

func (b *FileNodeBackend) CreateFileNode(ctx context.Context, node *jmapfilenode.FileNode) (*jmapfilenode.FileNode, error) {
	if node == nil {
		return nil, fmt.Errorf("node is nil")
	}
	if node.Name == "" {
		return nil, fmt.Errorf("node name is empty")
	}
	fs, u, err := b.client.WebDAV(ctx)
	if err != nil {
		return nil, err
	}
	if u == "" {
		return nil, ErrForbidden
	}

	b.mu.Lock()
	b.ensureMapsLocked(u)
	if node.ParentID != nil {
		if _, ok := b.idToPath[u][*node.ParentID]; !ok {
			b.mu.Unlock()
			return nil, fmt.Errorf("parent not found: %s", *node.ParentID)
		}
	}
	if node.ID == "" {
		b.nextID++
		node.ID = jmapcore.Id(fmt.Sprintf("fn-%d", b.nextID))
	}

	parentRel := ""
	if node.ParentID != nil {
		parentRel = b.idToPath[u][*node.ParentID]
	}
	targetRel := path.Join(parentRel, node.Name)
	b.pathToID[u][targetRel] = node.ID
	b.idToPath[u][node.ID] = targetRel
	bb := b.blobBackend
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
		node.Type = "folder"
		_ = fs.Mkdir(ctx, targetRel)
	} else {
		wc, err := fs.Create(ctx, targetRel)
		if err == nil && wc != nil {
			var writtenData []byte
			if node.BlobID != nil && bb != nil {
				if blob, found, _ := bb.GetBlob(ctx, u, string(*node.BlobID)); found && blob != nil {
					_, _ = wc.Write(blob.Data)
					writtenData = blob.Data
					node.Size = uint64(len(blob.Data))
					if node.Type == "" || node.Type == "file" {
						node.Type = blob.Type
					}
				}
			}
			_ = wc.Close()

			if node.BlobID == nil {
				hash := sha256.Sum256(writtenData)
				bid := jmapcore.Id(hex.EncodeToString(hash[:]))
				node.BlobID = &bid
			}
		}
		if node.Type == "" {
			node.Type = mimeTypeForName(node.Name)
		}
	}

	b.mu.Lock()
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

func (b *FileNodeBackend) UpdateFileNode(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapfilenode.FileNode, error) {
	if patch == nil {
		return nil, fmt.Errorf("patch is nil")
	}
	u := b.user(ctx)
	if u == "" {
		return nil, ErrForbidden
	}
	nodes, notFound, err := b.GetFileNodes(ctx, []jmapcore.Id{id})
	if err != nil {
		return nil, err
	}
	if len(notFound) > 0 || len(nodes) == 0 {
		return nil, jmapcore.ErrNotFound
	}

	b.mu.Lock()
	node := b.nodesCache[u][id]
	if node == nil {
		node = nodes[0]
	}
	oldName := node.Name
	oldParent := node.ParentID

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
			if s, ok := v.(string); ok && s != "" {
				pid := jmapcore.Id(s)
				node.ParentID = &pid
			} else if v == nil || v == "" {
				node.ParentID = nil
			}
		case "blobId":
			if s, ok := v.(string); ok && s != "" {
				bid := jmapcore.Id(s)
				node.BlobID = &bid
			} else if v == nil {
				node.BlobID = nil
			}
		}
	}
	node.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	oldRel := b.idToPath[u][id]
	bb := b.blobBackend
	b.mu.Unlock()

	// Handle Move / Rename on WebDAV if name or parent changed
	parentChanged := (oldParent == nil && node.ParentID != nil) || (oldParent != nil && node.ParentID == nil) || (oldParent != nil && node.ParentID != nil && *oldParent != *node.ParentID)
	nameChanged := node.Name != oldName && oldName != ""

	if nameChanged || parentChanged {
		b.mu.RLock()
		newParentRel := ""
		if node.ParentID != nil {
			newParentRel = b.idToPath[u][*node.ParentID]
		}
		newRel := path.Join(newParentRel, node.Name)
		b.mu.RUnlock()

		if fs, _, err := b.client.WebDAV(ctx); err == nil && oldRel != "" && newRel != oldRel {
			_ = fs.Move(ctx, oldRel, newRel, nil)
			b.mu.Lock()
			delete(b.pathToID[u], oldRel)
			b.pathToID[u][newRel] = id
			b.idToPath[u][id] = newRel
			b.mu.Unlock()
		}
	}

	// Update blob content on WebDAV if blobId was patched
	if _, blobPatched := patch["blobId"]; blobPatched && node.BlobID != nil && !node.IsFolder {
		b.mu.RLock()
		currentRel := b.idToPath[u][id]
		b.mu.RUnlock()
		if fs, _, err := b.client.WebDAV(ctx); err == nil && bb != nil && currentRel != "" {
			if blob, found, _ := bb.GetBlob(ctx, u, string(*node.BlobID)); found && blob != nil {
				if wc, err := fs.Create(ctx, currentRel); err == nil && wc != nil {
					_, _ = wc.Write(blob.Data)
					_ = wc.Close()
					node.Size = uint64(len(blob.Data))
				}
			}
		}
	}

	b.mu.Lock()
	b.nodesCache[u][id] = node
	st := b.getNodeTracker(u).Record(id, "update")
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return node, nil
}

func (b *FileNodeBackend) DeleteFileNode(ctx context.Context, id jmapcore.Id) (bool, error) {
	if id == "" {
		return false, nil
	}
	u := b.user(ctx)
	if u == "" {
		return false, ErrForbidden
	}
	nodes, notFound, err := b.GetFileNodes(ctx, []jmapcore.Id{id})
	if err != nil {
		return false, err
	}
	if len(notFound) > 0 || len(nodes) == 0 {
		return false, nil
	}

	fs, _, err := b.client.WebDAV(ctx)
	if err != nil {
		return false, err
	}

	b.mu.RLock()
	targetRel := b.idToPath[u][id]
	b.mu.RUnlock()

	if targetRel == "" {
		targetRel = nodes[0].Name
	}

	_ = fs.RemoveAll(ctx, targetRel)

	b.mu.Lock()
	if b.nodesCache[u] != nil {
		delete(b.nodesCache[u], id)
	}
	if b.pathToID[u] != nil {
		delete(b.pathToID[u], targetRel)
	}
	if b.idToPath[u] != nil {
		delete(b.idToPath[u], id)
	}
	st := b.getNodeTracker(u).Record(id, "destroy")
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return true, nil
}

func (b *FileNodeBackend) QueryFileNodes(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	u := b.user(ctx)
	if u == "" {
		return nil, 0, ErrForbidden
	}
	nodes, _, err := b.GetFileNodes(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	var matching []jmapcore.Id
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
				if pid == "" {
					if n.ParentID != nil {
						continue
					}
				} else {
					if n.ParentID == nil || string(*n.ParentID) != pid {
						continue
					}
				}
			}
			if bid, ok := filter["blobId"].(string); ok && bid != "" {
				if n.BlobID == nil || string(*n.BlobID) != bid {
					continue
				}
			}
		}
		matching = append(matching, n.ID)
	}

	sort.Slice(matching, func(i, j int) bool {
		return matching[i] < matching[j]
	})

	total := len(matching)
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
		ids = append(ids, matching[i])
	}

	return ids, total, nil
}
