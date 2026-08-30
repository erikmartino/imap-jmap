package nextcloud

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// FileNodeBackend implements jmap.FileNodeBackend backed by Nextcloud WebDAV via github.com/emersion/go-webdav.
type FileNodeBackend struct {
	client      *Client
	mu          sync.RWMutex
	broadcaster *jmap.Broadcaster

	nodeStates map[string]int
	nodesCache map[string]map[jmap.Id]*jmap.FileNode
}

var _ jmap.FileNodeBackend = (*FileNodeBackend)(nil)

// NewFileNodeBackend initializes a new Nextcloud-backed FileNodeBackend.
func NewFileNodeBackend(client *Client) *FileNodeBackend {
	return &FileNodeBackend{
		client:     client,
		nodeStates: make(map[string]int),
		nodesCache: make(map[string]map[jmap.Id]*jmap.FileNode),
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

func (b *FileNodeBackend) FileNodeState(ctx context.Context) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := b.user(ctx)
	st, ok := b.nodeStates[u]
	if !ok {
		st = 1
		b.nodeStates[u] = st
	}
	return strconv.Itoa(st)
}

func (b *FileNodeBackend) FileNodeChanges(ctx context.Context, sinceState string) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	cur := b.FileNodeState(ctx)
	if sinceState == cur {
		return nil, nil, nil, cur, false
	}
	nodes, _ := b.GetAllFileNodes(ctx)
	var created []jmap.Id
	for _, n := range nodes {
		created = append(created, n.ID)
	}
	return created, nil, nil, cur, false
}

func (b *FileNodeBackend) GetAllFileNodes(ctx context.Context) ([]*jmap.FileNode, error) {
	nodes, _, err := b.GetFileNodes(ctx, nil)
	return nodes, err
}

func (b *FileNodeBackend) GetFileNodes(ctx context.Context, ids []jmap.Id) ([]*jmap.FileNode, []jmap.Id, error) {
	fs, _, err := b.client.WebDAV(ctx)
	if err != nil {
		return nil, nil, err
	}

	fis, err := fs.ReadDir(ctx, "", false)
	if err != nil {
		return nil, nil, nil
	}

	var list []*jmap.FileNode
	idMap := make(map[jmap.Id]bool)
	for _, id := range ids {
		idMap[id] = true
	}

	for _, fi := range fis {
		nodeName := path.Base(fi.Path)
		if nodeName == "" || nodeName == "." || nodeName == ".." {
			continue
		}

		nodeID := jmap.Id(strings.ReplaceAll(nodeName, "/", "_"))
		if len(ids) > 0 && !idMap[nodeID] {
			continue
		}

		nodeType := "file"
		if fi.IsDir {
			nodeType = "folder"
		}

		node := &jmap.FileNode{
			ID:        nodeID,
			Name:      path.Base(nodeName),
			Type:      nodeType,
			Size:      uint64(fi.Size),
			IsFolder:  fi.IsDir,
			UpdatedAt: fi.ModTime.Format(time.RFC3339),
		}

		list = append(list, node)
	}

	var notFound []jmap.Id
	if len(ids) > 0 {
		foundMap := make(map[jmap.Id]bool)
		for _, n := range list {
			foundMap[n.ID] = true
		}
		for _, id := range ids {
			if !foundMap[id] {
				notFound = append(notFound, id)
			}
		}
	}

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

	if node.ID == "" {
		node.ID = jmap.Id(fmt.Sprintf("node-%d", time.Now().UnixNano()))
	}

	if node.IsFolder || node.Type == "folder" || node.Type == "directory" {
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
	b.nodesCache[u][node.ID] = node
	b.nodeStates[u]++
	st := strconv.Itoa(b.nodeStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return node, nil
}

func (b *FileNodeBackend) UpdateFileNode(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.FileNode, error) {
	u := b.user(ctx)
	b.mu.Lock()
	b.nodeStates[u]++
	st := strconv.Itoa(b.nodeStates[u])
	b.mu.Unlock()

	b.emitStateChange(u, "FileNode", st)
	return &jmap.FileNode{ID: id}, nil
}

func (b *FileNodeBackend) DeleteFileNode(ctx context.Context, id jmap.Id) (bool, error) {
	fs, u, err := b.client.WebDAV(ctx)
	if err != nil {
		return false, err
	}

	_ = fs.RemoveAll(ctx, string(id))

	b.mu.Lock()
	if b.nodesCache[u] != nil {
		delete(b.nodesCache[u], id)
	}
	b.nodeStates[u]++
	st := strconv.Itoa(b.nodeStates[u])
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
		}
		matching = append(matching, n.ID)
	}

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
