package jmapfilenode_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapfilenode"
	"imap-jmap/jmap/jmaphandler"
)

type memFileNodeBackend struct {
	mu      sync.RWMutex
	nodes   map[jmapcore.Id]*jmapfilenode.FileNode
	state   int
	created []jmapcore.Id
	updated []jmapcore.Id
	deleted []jmapcore.Id
	hasMore bool
}

func newMemFileNodeBackend() *memFileNodeBackend {
	return &memFileNodeBackend{
		nodes: make(map[jmapcore.Id]*jmapfilenode.FileNode),
		state: 1,
	}
}

func (b *memFileNodeBackend) FileNodeState(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return fmt.Sprintf("%d", b.state)
}

func (b *memFileNodeBackend) FileNodeChanges(ctx context.Context, sinceState string) ([]jmapcore.Id, []jmapcore.Id, []jmapcore.Id, string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cur := fmt.Sprintf("%d", b.state)
	if sinceState == cur {
		return nil, nil, nil, cur, false
	}
	return b.created, b.updated, b.deleted, cur, b.hasMore
}

func (b *memFileNodeBackend) GetAllFileNodes(ctx context.Context) ([]*jmapfilenode.FileNode, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var list []*jmapfilenode.FileNode
	for _, n := range b.nodes {
		list = append(list, n)
	}
	return list, nil
}

func (b *memFileNodeBackend) GetFileNodes(ctx context.Context, ids []jmapcore.Id) ([]*jmapfilenode.FileNode, []jmapcore.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var list []*jmapfilenode.FileNode
	var notFound []jmapcore.Id
	for _, id := range ids {
		if n, ok := b.nodes[id]; ok {
			list = append(list, n)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *memFileNodeBackend) CreateFileNode(ctx context.Context, node *jmapfilenode.FileNode) (*jmapfilenode.FileNode, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if node.ID == "" {
		node.ID = jmapcore.Id(fmt.Sprintf("node-%d", len(b.nodes)+1))
	}
	b.nodes[node.ID] = node
	b.state++
	b.created = append(b.created, node.ID)
	return node, nil
}

func (b *memFileNodeBackend) UpdateFileNode(ctx context.Context, id jmapcore.Id, patch map[string]any) (*jmapfilenode.FileNode, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	node, ok := b.nodes[id]
	if !ok {
		return nil, jmapcore.ErrNotFound
	}
	if name, ok := patch["name"].(string); ok {
		node.Name = name
	}
	if isF, ok := patch["isFolder"].(bool); ok {
		node.IsFolder = isF
	}
	b.state++
	b.updated = append(b.updated, id)
	return node, nil
}

func (b *memFileNodeBackend) DeleteFileNode(ctx context.Context, id jmapcore.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.nodes[id]; !ok {
		return false, nil
	}
	delete(b.nodes, id)
	b.state++
	b.deleted = append(b.deleted, id)
	return true, nil
}

func (b *memFileNodeBackend) QueryFileNodes(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var matching []jmapcore.Id
	for id, n := range b.nodes {
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
		}
		matching = append(matching, id)
	}
	sort.Slice(matching, func(i, j int) bool {
		return string(matching[i]) < string(matching[j])
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
	return matching[position:end], total, nil
}

func TestFileNodeHandlers(t *testing.T) {
	backend := newMemFileNodeBackend()
	reg := jmaphandler.NewMethodRegistry()
	jmapfilenode.RegisterFileNodeHandlers(reg, backend)

	ctx := context.Background()

	// 1. FileNode/set create
	setHandler, _ := reg.Get("FileNode/set")
	if setHandler == nil {
		t.Fatal("FileNode/set not registered")
	}

	setRes, setArgs := setHandler(ctx, map[string]any{
		"accountId": "primary",
		"create": map[string]any{
			"f1": map[string]any{
				"name":     "Folder A",
				"isFolder": true,
			},
			"f2": map[string]any{
				"name":     "file.txt",
				"parentId": "#f1",
				"size":     float64(100),
			},
		},
	}, "c1")

	if setRes != "FileNode/set" {
		t.Fatalf("Expected FileNode/set, got %q", setRes)
	}
	created := setArgs["created"].(map[string]*jmapfilenode.FileNode)
	if len(created) != 2 {
		t.Fatalf("Expected 2 created nodes, got %d", len(created))
	}
	f1 := created["f1"]
	f2 := created["f2"]
	if f2.ParentID == nil || *f2.ParentID != f1.ID {
		t.Errorf("Expected f2.ParentID to equal f1.ID (%s), got %v", f1.ID, f2.ParentID)
	}

	// 2. FileNode/get
	getHandler, _ := reg.Get("FileNode/get")
	getRes, getArgs := getHandler(ctx, map[string]any{
		"accountId": "primary",
		"ids":       []any{string(f1.ID), "nonexistent"},
	}, "c2")
	if getRes != "FileNode/get" {
		t.Fatalf("Expected FileNode/get, got %q", getRes)
	}
	list := getArgs["list"].([]any)
	if len(list) != 1 {
		t.Errorf("Expected 1 found node, got %d", len(list))
	}
	notFound := getArgs["notFound"].([]jmapcore.Id)
	if len(notFound) != 1 || notFound[0] != "nonexistent" {
		t.Errorf("Expected [nonexistent] in notFound, got %v", notFound)
	}

	// 3. FileNode/query
	queryHandler, _ := reg.Get("FileNode/query")
	qRes, qArgs := queryHandler(ctx, map[string]any{
		"accountId": "primary",
		"filter":    map[string]any{"name": "file"},
	}, "c3")
	if qRes != "FileNode/query" {
		t.Fatalf("Expected FileNode/query, got %q", qRes)
	}
	if qArgs["total"].(int) != 1 {
		t.Errorf("Expected total=1, got %v", qArgs["total"])
	}

	// 4. FileNode/changes
	changesHandler, _ := reg.Get("FileNode/changes")
	cRes, cArgs := changesHandler(ctx, map[string]any{
		"accountId":  "primary",
		"sinceState": "1",
	}, "c4")
	if cRes != "FileNode/changes" {
		t.Fatalf("Expected FileNode/changes, got %q", cRes)
	}
	createdList := cArgs["created"].([]jmapcore.Id)
	if len(createdList) != 2 {
		t.Errorf("Expected 2 created in changes, got %d", len(createdList))
	}

	// 5. FileNode/set update & destroy
	_, updArgs := setHandler(ctx, map[string]any{
		"accountId": "primary",
		"update": map[string]any{
			string(f1.ID): map[string]any{"name": "Folder Renamed"},
		},
		"destroy": []any{string(f2.ID)},
	}, "c5")

	if len(updArgs["updated"].(map[string]any)) != 1 {
		t.Errorf("Expected 1 updated, got %v", updArgs["updated"])
	}
	if len(updArgs["destroyed"].([]jmapcore.Id)) != 1 {
		t.Errorf("Expected 1 destroyed, got %v", updArgs["destroyed"])
	}
}

func TestFileNodeQueryChangesHandler(t *testing.T) {
	backend := newMemFileNodeBackend()
	reg := jmaphandler.NewMethodRegistry()
	jmapfilenode.RegisterFileNodeHandlers(reg, backend)
	ctx := context.Background()

	qcHandler, _ := reg.Get("FileNode/queryChanges")
	if qcHandler == nil {
		t.Fatal("FileNode/queryChanges not registered")
	}

	// 1. When backend is nil (edge case)
	nilReg := jmaphandler.NewMethodRegistry()
	jmapfilenode.RegisterFileNodeHandlers(nilReg, nil)
	nilHandler, _ := nilReg.Get("FileNode/queryChanges")
	nilName, nilArgs := nilHandler(ctx, map[string]any{
		"accountId":       "primary",
		"sinceQueryState": "1",
	}, "c_nil")
	if nilName != "FileNode/queryChanges" || nilArgs["newQueryState"] != "0" {
		t.Errorf("Expected newQueryState 0 for nil backend, got %v", nilArgs)
	}

	// 2. Normal query changes with added items
	node1, _ := backend.CreateFileNode(ctx, &jmapfilenode.FileNode{Name: "file1.txt"})
	node2, _ := backend.CreateFileNode(ctx, &jmapfilenode.FileNode{Name: "file2.txt"})

	name, args := qcHandler(ctx, map[string]any{
		"accountId":       "primary",
		"sinceQueryState": "1",
	}, "c1")
	if name != "FileNode/queryChanges" {
		t.Fatalf("Expected FileNode/queryChanges, got %s", name)
	}
	added, ok := args["added"].([]map[string]any)
	if !ok || len(added) != 2 {
		t.Fatalf("Expected 2 added items, got %#v", args["added"])
	}
	if added[0]["id"] != node1.ID || added[1]["id"] != node2.ID {
		t.Errorf("Added IDs mismatch: %v, %v", added[0]["id"], added[1]["id"])
	}

	// 3. With upToId filter
	_, upToArgs := qcHandler(ctx, map[string]any{
		"accountId":       "primary",
		"sinceQueryState": "1",
		"upToId":          string(node1.ID),
	}, "c2")
	upToAdded := upToArgs["added"].([]map[string]any)
	if len(upToAdded) != 1 || upToAdded[0]["id"] != node1.ID {
		t.Errorf("Expected 1 added item up to %s, got %#v", node1.ID, upToAdded)
	}
	if upToArgs["upToId"] != string(node1.ID) {
		t.Errorf("Expected upToId in response, got %v", upToArgs["upToId"])
	}

	// 4. When hasMore is true, must return cannotCalculateChanges error
	backend.hasMore = true
	errName, errArgs := qcHandler(ctx, map[string]any{
		"accountId":       "primary",
		"sinceQueryState": "1",
	}, "c3")
	if errName != "error" {
		t.Fatalf("Expected error, got %s", errName)
	}
	if errArgs["type"] != "cannotCalculateChanges" {
		t.Errorf("Expected cannotCalculateChanges error type, got %v", errArgs["type"])
	}
}

func TestFileNodeEdgeCases(t *testing.T) {
	backend := newMemFileNodeBackend()
	reg := jmaphandler.NewMethodRegistry()
	jmapfilenode.RegisterFileNodeHandlers(reg, backend)
	ctx := context.Background()

	// 1. FileNode/get with properties projection
	f1, _ := backend.CreateFileNode(ctx, &jmapfilenode.FileNode{Name: "readme.txt", Size: 42, Type: "text/plain"})
	getHandler, _ := reg.Get("FileNode/get")
	_, getArgs := getHandler(ctx, map[string]any{
		"accountId":  "primary",
		"ids":        []any{string(f1.ID)},
		"properties": []any{"name", "size"},
	}, "c1")
	list := getArgs["list"].([]any)
	if len(list) != 1 {
		t.Fatalf("Expected 1 item in list, got %d", len(list))
	}
	item := list[0].(map[string]any)
	if item["name"] != "readme.txt" || item["id"] != string(f1.ID) {
		t.Errorf("Properties mismatch: %#v", item)
	}
	// type should NOT be returned because it was not in properties
	if _, hasType := item["type"]; hasType {
		t.Errorf("Expected type not to be included in projection, got %#v", item)
	}

	// 2. FileNode/get with null ids returns all
	_, allArgs := getHandler(ctx, map[string]any{
		"accountId": "primary",
	}, "c2")
	allList := allArgs["list"].([]any)
	if len(allList) != 1 {
		t.Errorf("Expected 1 item for null ids, got %d", len(allList))
	}

	// 3. FileNode/set with ifInState mismatch
	setHandler, _ := reg.Get("FileNode/set")
	errName, errArgs := setHandler(ctx, map[string]any{
		"accountId": "primary",
		"ifInState": "mismatch-state",
		"create": map[string]any{
			"f": map[string]any{"name": "fail.txt"},
		},
	}, "c3")
	if errName != "error" || errArgs["type"] != "stateMismatch" {
		t.Errorf("Expected stateMismatch error, got name=%s args=%#v", errName, errArgs)
	}

	// 4. FileNode/set with unresolved forward reference
	_, refArgs := setHandler(ctx, map[string]any{
		"accountId": "primary",
		"create": map[string]any{
			"c": map[string]any{"name": "child.txt", "parentId": "#missing"},
		},
	}, "c4")
	notCreated := refArgs["notCreated"].(map[string]any)
	if notCreated["c"] == nil {
		t.Errorf("Expected notCreated for unresolved ref, got %#v", notCreated)
	}

	// 5. FileNode/set update non-existent
	_, notUpdArgs := setHandler(ctx, map[string]any{
		"accountId": "primary",
		"update": map[string]any{
			"nonexistent": map[string]any{"name": "newname"},
		},
	}, "c5")
	notUpdated := notUpdArgs["notUpdated"].(map[string]any)
	if notUpdated["nonexistent"] == nil {
		t.Errorf("Expected notFound in notUpdated, got %#v", notUpdated)
	}

	// 6. FileNode/set destroy non-existent
	_, notDstArgs := setHandler(ctx, map[string]any{
		"accountId": "primary",
		"destroy":   []any{"nonexistent"},
	}, "c6")
	notDestroyed := notDstArgs["notDestroyed"].(map[string]any)
	if notDestroyed["nonexistent"] == nil {
		t.Errorf("Expected notFound in notDestroyed, got %#v", notDestroyed)
	}

	// 7. FileNode/query calculateTotal, position, and anchor
	queryHandler, _ := reg.Get("FileNode/query")
	_, qArgs := queryHandler(ctx, map[string]any{
		"accountId":      "primary",
		"calculateTotal": true,
	}, "c7")
	if qArgs["total"].(int) < 1 {
		t.Errorf("Expected calculateTotal > 0, got %v", qArgs["total"])
	}

	// Query with anchor
	_, qAnchorArgs := queryHandler(ctx, map[string]any{
		"accountId": "primary",
		"anchor":    string(f1.ID),
	}, "c7_anchor")
	if len(qAnchorArgs["ids"].([]jmapcore.Id)) != 1 {
		t.Errorf("Expected 1 id with anchor, got %v", qAnchorArgs["ids"])
	}

	// Query with nonexistent anchor -> anchorNotFound
	ancErrName, ancErrArgs := queryHandler(ctx, map[string]any{
		"accountId": "primary",
		"anchor":    "nonexistent-anchor",
	}, "c7_noanchor")
	if ancErrName != "error" || ancErrArgs["type"] != "anchorNotFound" {
		t.Errorf("Expected anchorNotFound error, got %s: %#v", ancErrName, ancErrArgs)
	}

	// Query with invalid anchor type -> invalidArguments
	ancTypeErrName, ancTypeErrArgs := queryHandler(ctx, map[string]any{
		"accountId": "primary",
		"anchor":    12345,
	}, "c7_badanchor")
	if ancTypeErrName != "error" || ancTypeErrArgs["type"] != "invalidArguments" {
		t.Errorf("Expected invalidArguments for bad anchor type, got %s: %#v", ancTypeErrName, ancTypeErrArgs)
	}

	// 8. FilterList helper direct call
	filtered := jmapfilenode.FilterList([]*jmapfilenode.FileNode{f1}, []string{"name"})
	if len(filtered) != 1 {
		t.Errorf("FilterList failed: got %v", filtered)
	}
}

