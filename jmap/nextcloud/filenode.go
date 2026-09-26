package nextcloud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	movedIDs     map[string]map[jmapcore.Id]jmapcore.Id
}

var _ jmapfilenode.FileNodeBackend = (*FileNodeBackend)(nil)

// NewFileNodeBackend initializes a new Nextcloud-backed FileNodeBackend.
func NewFileNodeBackend(client *Client) *FileNodeBackend {
	return &FileNodeBackend{
		client:       client,
		nodeTrackers: make(map[string]*jmappush.ChangeTracker),
		nodesCache:   make(map[string]map[jmapcore.Id]*jmapfilenode.FileNode),
		movedIDs:     make(map[string]map[jmapcore.Id]jmapcore.Id),
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
	if clean == "." {
		return ""
	}
	return clean
}

// FileNodeIDForPath derives a deterministic JMAP ID from a WebDAV relative path.
func FileNodeIDForPath(relPath string) jmapcore.Id {
	clean := cleanRelPath(relPath)
	if clean == "" {
		return "fn-root"
	}
	return jmapcore.Id("fn-" + base64.RawURLEncoding.EncodeToString([]byte(clean)))
}

// PathForFileNodeID decodes a WebDAV relative path from a deterministic JMAP ID.
func PathForFileNodeID(id jmapcore.Id) (string, error) {
	s := string(id)
	if s == "" || s == "root" || s == "fn-root" {
		return "", nil
	}
	raw := strings.TrimPrefix(s, "fn-")
	raw = strings.TrimRight(raw, "=")
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("invalid file node id encoding: %w", err)
	}
	return cleanRelPath(string(data)), nil
}

func (b *FileNodeBackend) relPathForNodeLocked(u string, n *jmapfilenode.FileNode) string {
	if n == nil {
		return ""
	}
	if n.ParentID == nil || *n.ParentID == "" || *n.ParentID == "root" || *n.ParentID == "fn-root" {
		return cleanRelPath(n.Name)
	}
	parent := b.nodesCache[u][*n.ParentID]
	if parent != nil {
		pRel := b.relPathForNodeLocked(u, parent)
		if pRel != "" {
			return path.Join(pRel, n.Name)
		}
		return cleanRelPath(n.Name)
	}
	if pRel, err := PathForFileNodeID(*n.ParentID); err == nil && pRel != "" {
		return path.Join(pRel, n.Name)
	}
	return cleanRelPath(n.Name)
}

func (b *FileNodeBackend) trackMovedLocked(u string, oldID, newID jmapcore.Id) {
	if oldID == "" || newID == "" || oldID == newID {
		return
	}
	if b.movedIDs[u] == nil {
		b.movedIDs[u] = make(map[jmapcore.Id]jmapcore.Id)
	}
	b.movedIDs[u][oldID] = newID
}

func (b *FileNodeBackend) resolveMovedIDLocked(u string, id jmapcore.Id) jmapcore.Id {
	if b.movedIDs[u] != nil {
		if target, ok := b.movedIDs[u][id]; ok && target != "" {
			return target
		}
	}
	return id
}

func (b *FileNodeBackend) pathForNodeIDLocked(u string, id jmapcore.Id) string {
	resolved := b.resolveMovedIDLocked(u, id)
	if n := b.nodesCache[u][resolved]; n != nil {
		return b.relPathForNodeLocked(u, n)
	}
	p, _ := PathForFileNodeID(resolved)
	return p
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

			nodeID := FileNodeIDForPath(clean)

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
					if existing.Type != "" && existing.Type != "folder" && existing.Type != "directory" {
						node.Type = existing.Type
					}
					if existing.BlobID != nil {
						node.BlobID = existing.BlobID
					}
					if existing.CreatedAt != "" {
						node.CreatedAt = existing.CreatedAt
					}
				}
				// Ensure files are strictly files and not folders
				node.IsFolder = false
				if node.Type == "" || node.Type == "folder" || node.Type == "directory" {
					node.Type = mimeTypeForName(nodeName)
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

	for _, n := range userCache {
		if !n.IsFolder && n.Type != "folder" && n.Type != "directory" && n.BlobID != nil && string(*n.BlobID) == blobID {
			rel := b.relPathForNodeLocked(u, n)
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
		for _, rawID := range ids {
			id := b.resolveMovedIDLocked(u, rawID)
			if n, ok := userCache[id]; ok {
				list = append(list, n)
			} else {
				notFound = append(notFound, rawID)
			}
		}
	} else {
		seen := make(map[jmapcore.Id]bool)
		for _, n := range userCache {
			if !seen[n.ID] {
				seen[n.ID] = true
				list = append(list, n)
			}
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
	parentRel := ""
	if node.ParentID != nil {
		parentRel = b.pathForNodeIDLocked(u, *node.ParentID)
		if parentRel == "" && *node.ParentID != "root" && *node.ParentID != "fn-root" {
			b.mu.Unlock()
			return nil, fmt.Errorf("parent not found: %s", *node.ParentID)
		}
	}
	if parentRel != "" {
		if stat, err := fs.Stat(ctx, parentRel); err != nil || !stat.IsDir {
			b.mu.Unlock()
			return nil, fmt.Errorf("parent not found: %s", *node.ParentID)
		}
	}
	targetRel := path.Join(parentRel, node.Name)
	node.ID = FileNodeIDForPath(targetRel)
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
		node.BlobID = nil
		node.Size = 0

		if stat, err := fs.Stat(ctx, targetRel); err == nil && !stat.IsDir {
			return nil, fmt.Errorf("cannot create folder %q: path is an existing file", targetRel)
		}
		_ = fs.Mkdir(ctx, targetRel)
	} else {
		node.IsFolder = false
		if node.Type == "" || node.Type == "file" || node.Type == "folder" || node.Type == "directory" {
			node.Type = mimeTypeForName(node.Name)
		}

		if stat, err := fs.Stat(ctx, targetRel); err == nil && stat.IsDir {
			return nil, fmt.Errorf("cannot create file %q: path is an existing folder", targetRel)
		}

		wc, err := fs.Create(ctx, targetRel)
		if err == nil && wc != nil {
			var writtenData []byte
			if node.BlobID != nil && bb != nil {
				if blob, found, _ := bb.GetBlob(ctx, u, string(*node.BlobID)); found && blob != nil {
					_, _ = wc.Write(blob.Data)
					writtenData = blob.Data
					node.Size = uint64(len(blob.Data))
					if node.Type == "" || node.Type == "file" || node.Type == "folder" || node.Type == "directory" {
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
		if node.Type == "" || node.Type == "file" || node.Type == "folder" || node.Type == "directory" {
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
	oldRel := b.pathForNodeIDLocked(u, id)
	oldName := node.Name
	oldParent := node.ParentID

	oldIsFolder := node.IsFolder
	for k, v := range patch {
		switch k {
		case "name":
			if s, ok := v.(string); ok && s != "" {
				node.Name = s
			}
		case "type":
			if s, ok := v.(string); ok {
				if node.IsFolder {
					if s != "folder" && s != "directory" {
						b.mu.Unlock()
						return nil, fmt.Errorf("cannot set file MIME type %q on a folder", s)
					}
				} else {
					if s == "folder" || s == "directory" {
						b.mu.Unlock()
						return nil, fmt.Errorf("cannot set folder type on a file")
					}
					node.Type = s
				}
			}
		case "isFolder":
			if bVal, ok := v.(bool); ok {
				if bVal != oldIsFolder {
					b.mu.Unlock()
					return nil, fmt.Errorf("cannot change isFolder on an existing node: file cannot become folder or vice versa")
				}
			}
		case "size":
			if f, ok := v.(float64); ok {
				if !node.IsFolder {
					node.Size = uint64(f)
				}
			}
		case "parentId":
			if s, ok := v.(string); ok && s != "" {
				pid := jmapcore.Id(s)
				node.ParentID = &pid
			} else if v == nil || v == "" {
				node.ParentID = nil
			}
		case "blobId":
			if node.IsFolder {
				b.mu.Unlock()
				return nil, fmt.Errorf("cannot set blobId on a folder")
			}
			if s, ok := v.(string); ok && s != "" {
				bid := jmapcore.Id(s)
				node.BlobID = &bid
			} else if v == nil {
				node.BlobID = nil
			}
		}
	}
	node.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	bb := b.blobBackend
	b.mu.Unlock()

	// Handle Move / Rename on WebDAV if name or parent changed
	parentChanged := (oldParent == nil && node.ParentID != nil) || (oldParent != nil && node.ParentID == nil) || (oldParent != nil && node.ParentID != nil && *oldParent != *node.ParentID)
	nameChanged := node.Name != oldName && oldName != ""

	if nameChanged || parentChanged {
		b.mu.RLock()
		newParentRel := ""
		if node.ParentID != nil {
			newParentRel = b.pathForNodeIDLocked(u, *node.ParentID)
		}
		newRel := path.Join(newParentRel, node.Name)
		b.mu.RUnlock()

		if fs, _, err := b.client.WebDAV(ctx); err == nil && oldRel != "" && newRel != oldRel {
			if stat, err := fs.Stat(ctx, newRel); err == nil {
				if !node.IsFolder && stat.IsDir {
					return nil, fmt.Errorf("cannot move file to %q: destination is an existing folder", newRel)
				}
				if node.IsFolder && !stat.IsDir {
					return nil, fmt.Errorf("cannot move folder to %q: destination is an existing file", newRel)
				}
			}
			_ = fs.Move(ctx, oldRel, newRel, nil)
			b.mu.Lock()
			newDeterministicID := FileNodeIDForPath(newRel)
			oldDeterministicID := FileNodeIDForPath(oldRel)
			delete(b.nodesCache[u], id)
			delete(b.nodesCache[u], oldDeterministicID)
			node.ID = newDeterministicID
			b.nodesCache[u][newDeterministicID] = node
			b.trackMovedLocked(u, id, newDeterministicID)
			b.trackMovedLocked(u, oldDeterministicID, newDeterministicID)
			b.mu.Unlock()
		}
	}

	// Update blob content on WebDAV if blobId was patched
	if _, blobPatched := patch["blobId"]; blobPatched && node.BlobID != nil && !node.IsFolder {
		b.mu.RLock()
		currentRel := b.pathForNodeIDLocked(u, node.ID)
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
	b.nodesCache[u][node.ID] = node
	st := b.getNodeTracker(u).Record(id, "update")
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return node, nil
}

func (b *FileNodeBackend) DeleteFileNode(ctx context.Context, rawID jmapcore.Id) (bool, error) {
	if rawID == "" {
		return false, nil
	}
	u := b.user(ctx)
	if u == "" {
		return false, ErrForbidden
	}
	b.mu.RLock()
	id := b.resolveMovedIDLocked(u, rawID)
	b.mu.RUnlock()
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
	targetRel := b.pathForNodeIDLocked(u, id)
	b.mu.RUnlock()

	if targetRel == "" {
		targetRel = nodes[0].Name
	}

	_ = fs.RemoveAll(ctx, targetRel)

	b.mu.Lock()
	if b.nodesCache[u] != nil {
		delete(b.nodesCache[u], id)
		delete(b.nodesCache[u], rawID)
		targetID := FileNodeIDForPath(targetRel)
		delete(b.nodesCache[u], targetID)
	}
	if b.movedIDs[u] != nil {
		delete(b.movedIDs[u], id)
		delete(b.movedIDs[u], rawID)
	}
	st := b.getNodeTracker(u).Record(rawID, "destroy")
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
				if typeVal == "file" {
					if n.IsFolder || n.Type == "folder" || n.Type == "directory" {
						continue
					}
				} else if typeVal == "folder" || typeVal == "directory" {
					if !n.IsFolder && n.Type != "folder" && n.Type != "directory" {
						continue
					}
				} else if !strings.EqualFold(n.Type, typeVal) {
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
