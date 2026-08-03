package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// MemoryFileNodeBackend provides an in-memory implementation of jmap.FileNodeBackend
// for the JMAP FileNode file storage extension.
type MemoryFileNodeBackend struct {
	mu          sync.RWMutex
	nodes       map[jmap.Id]*jmap.FileNode
	state       *changeTracker
	nextID      uint64
	broadcaster *jmap.Broadcaster
}

// Ensure MemoryFileNodeBackend implements jmap.FileNodeBackend interface.
var _ jmap.FileNodeBackend = (*MemoryFileNodeBackend)(nil)

// NewMemoryFileNodeBackend initializes a new MemoryFileNodeBackend instance.
func NewMemoryFileNodeBackend() *MemoryFileNodeBackend {
	return &MemoryFileNodeBackend{
		nodes:  make(map[jmap.Id]*jmap.FileNode),
		state:  newChangeTracker(1000),
		nextID: 0,
	}
}

// SetBroadcaster connects a Broadcaster so FileNode mutations emit RFC 8620 Section 7.1
// StateChange push events, keeping any subscribed UI up to date.
func (b *MemoryFileNodeBackend) SetBroadcaster(bc *jmap.Broadcaster) {
	b.broadcaster = bc
}

// record commits a change to the tracker and publishes a StateChange for the "FileNode" type.
func (b *MemoryFileNodeBackend) record(id jmap.Id, action string) {
	newState := b.state.record(id, action)
	if b.broadcaster != nil {
		b.broadcaster.PublishStateChange("primary", "FileNode", newState)
	}
}

// FileNodeState returns the current change state token per RFC 8620.
func (b *MemoryFileNodeBackend) FileNodeState(ctx context.Context) string {
	return b.state.State()
}

// FileNodeChanges returns created/updated/destroyed nodes since the given state per RFC 8620 Section 5.2.
func (b *MemoryFileNodeBackend) FileNodeChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmap.Id, newState string, hasMore bool) {
	return b.state.Changes(sinceState)
}

// GetFileNodes returns the FileNodes matching the given ids, or all nodes when ids is empty.
func (b *MemoryFileNodeBackend) GetFileNodes(ctx context.Context, ids []jmap.Id) ([]*jmap.FileNode, []jmap.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(ids) == 0 {
		list := make([]*jmap.FileNode, 0, len(b.nodes))
		for _, n := range b.nodes {
			list = append(list, cloneFileNode(n))
		}
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		return list, nil, nil
	}

	var list []*jmap.FileNode
	var notFound []jmap.Id
	for _, id := range ids {
		if n, ok := b.nodes[id]; ok {
			list = append(list, cloneFileNode(n))
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllFileNodes returns every stored FileNode.
func (b *MemoryFileNodeBackend) GetAllFileNodes(ctx context.Context) ([]*jmap.FileNode, error) {
	list, _, err := b.GetFileNodes(ctx, nil)
	return list, err
}

// CreateFileNode stores a new FileNode, validating parent references and folder/blob invariants.
func (b *MemoryFileNodeBackend) CreateFileNode(ctx context.Context, node *jmap.FileNode) (*jmap.FileNode, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if strings.TrimSpace(node.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if node.ParentID != nil {
		parent, ok := b.nodes[*node.ParentID]
		if !ok {
			return nil, fmt.Errorf("parent not found: %s", *node.ParentID)
		}
		if !parent.IsFolder {
			return nil, fmt.Errorf("parent is not a folder: %s", *node.ParentID)
		}
	}
	if node.IsFolder && node.BlobID != nil {
		return nil, fmt.Errorf("a folder cannot reference a blob")
	}

	if node.ID == "" {
		b.nextID++
		node.ID = jmap.Id(fmt.Sprintf("fn-%d", b.nextID))
	}

	now := time.Now().UTC().Format(time.RFC3339)
	node.CreatedAt = now
	node.UpdatedAt = now

	b.nodes[node.ID] = node
	b.record(node.ID, "create")
	return cloneFileNode(node), nil
}

// UpdateFileNode applies a partial patch to an existing FileNode, preserving unaddressed fields.
func (b *MemoryFileNodeBackend) UpdateFileNode(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.FileNode, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	node, ok := b.nodes[id]
	if !ok {
		return nil, fmt.Errorf("filenode %s: %w", id, jmap.ErrNotFound)
	}

	// Work on a copy so a malformed patch never partially mutates stored data.
	updated := cloneFileNode(node)

	for prop, val := range patch {
		switch prop {
		case "name":
			name, ok := val.(string)
			if !ok || strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("invalid name")
			}
			updated.Name = name
		case "parentId":
			if val == nil {
				updated.ParentID = nil
				continue
			}
			pid, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("invalid parentId")
			}
			if jmap.Id(pid) == id {
				return nil, fmt.Errorf("a node cannot be its own parent")
			}
			parent, ok := b.nodes[jmap.Id(pid)]
			if !ok {
				return nil, fmt.Errorf("parent not found: %s", pid)
			}
			if !parent.IsFolder {
				return nil, fmt.Errorf("parent is not a folder: %s", pid)
			}
			p := jmap.Id(pid)
			updated.ParentID = &p
		case "blobId":
			if val == nil {
				updated.BlobID = nil
				continue
			}
			bid, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("invalid blobId")
			}
			if updated.IsFolder {
				return nil, fmt.Errorf("a folder cannot reference a blob")
			}
			bb := jmap.Id(bid)
			updated.BlobID = &bb
		case "type":
			t, ok := val.(string)
			if !ok {
				return nil, fmt.Errorf("invalid type")
			}
			updated.Type = t
		case "size":
			if f, ok := val.(float64); ok {
				updated.Size = uint64(f)
			} else {
				return nil, fmt.Errorf("invalid size")
			}
		default:
			return nil, fmt.Errorf("unknown or immutable property: %s", prop)
		}
	}

	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b.nodes[id] = updated
	b.record(id, "update")
	return cloneFileNode(updated), nil
}

// DeleteFileNode removes a FileNode. Folders with children are rejected to avoid orphaning descendants.
func (b *MemoryFileNodeBackend) DeleteFileNode(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.nodes[id]; !ok {
		return false, nil
	}
	for _, n := range b.nodes {
		if n.ParentID != nil && *n.ParentID == id {
			return false, fmt.Errorf("folder is not empty: %s", id)
		}
	}
	delete(b.nodes, id)
	b.record(id, "destroy")
	return true, nil
}

// QueryFileNodes filters and paginates stored FileNodes.
func (b *MemoryFileNodeBackend) QueryFileNodes(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmap.Id, int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	nameFilter, hasName := filter["name"].(string)
	parentFilter, hasParent := filter["parentId"].(string)
	typeFilter, hasType := filter["type"].(string)
	isFolderFilter, hasIsFolder := filter["isFolder"].(bool)

	var matched []*jmap.FileNode
	for _, n := range b.nodes {
		if hasName && !strings.Contains(strings.ToLower(n.Name), strings.ToLower(nameFilter)) {
			continue
		}
		if hasParent {
			if parentFilter == "" {
				if n.ParentID != nil {
					continue
				}
			} else if n.ParentID == nil || string(*n.ParentID) != parentFilter {
				continue
			}
		}
		if hasType && n.Type != typeFilter {
			continue
		}
		if hasIsFolder && n.IsFolder != isFolderFilter {
			continue
		}
		matched = append(matched, n)
	}

	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

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

// cloneFileNode returns a deep copy so callers can never mutate stored state through a returned pointer.
func cloneFileNode(n *jmap.FileNode) *jmap.FileNode {
	c := *n
	if n.ParentID != nil {
		p := *n.ParentID
		c.ParentID = &p
	}
	if n.BlobID != nil {
		bb := *n.BlobID
		c.BlobID = &bb
	}
	return &c
}
