package jmapextstore

import (
	"context"
	"testing"
)

type memStore struct {
	data map[string]map[string][]byte
}

func newMemStore() *memStore {
	return &memStore{data: make(map[string]map[string][]byte)}
}

func (m *memStore) Get(ctx context.Context, accountID, key string) ([]byte, error) {
	if m.data[accountID] == nil {
		return nil, nil
	}
	return m.data[accountID][key], nil
}

func (m *memStore) Put(ctx context.Context, accountID, key string, data []byte) error {
	if m.data[accountID] == nil {
		m.data[accountID] = make(map[string][]byte)
	}
	m.data[accountID][key] = data
	return nil
}

func (m *memStore) Delete(ctx context.Context, accountID, key string) error {
	if m.data[accountID] != nil {
		delete(m.data[accountID], key)
	}
	return nil
}

func TestStoreLoadSaveDelete(t *testing.T) {
	ctx := context.Background()
	s := newMemStore()

	type sample struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	// 1. Initial Load should return not found
	val, ok, err := Load[sample](ctx, s, "acc1", "key1")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if ok || val != nil {
		t.Fatalf("expected not found, got ok=%v, val=%v", ok, val)
	}

	// 2. Save
	orig := sample{Name: "test", Value: 42}
	if err := Save(ctx, s, "acc1", "key1", orig); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 3. Load again
	loaded, ok, err := Load[sample](ctx, s, "acc1", "key1")
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	if !ok || loaded == nil {
		t.Fatalf("expected found")
	}
	if loaded.Name != "test" || loaded.Value != 42 {
		t.Errorf("unexpected loaded data: %+v", *loaded)
	}

	// 4. Delete
	if err := Delete(ctx, s, "acc1", "key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 5. Load after delete
	val2, ok2, err := Load[sample](ctx, s, "acc1", "key1")
	if err != nil {
		t.Fatalf("Load after Delete failed: %v", err)
	}
	if ok2 || val2 != nil {
		t.Fatalf("expected not found after delete, got ok=%v", ok2)
	}

	// 6. Nil store safety
	if err := Save[sample](ctx, nil, "acc1", "key1", orig); err != nil {
		t.Errorf("nil store Save returned error: %v", err)
	}
	valNil, okNil, errNil := Load[sample](ctx, nil, "acc1", "key1")
	if errNil != nil || okNil || valNil != nil {
		t.Errorf("nil store Load unexpected: %v, %v, %v", valNil, okNil, errNil)
	}
	if err := Delete(ctx, nil, "acc1", "key1"); err != nil {
		t.Errorf("nil store Delete returned error: %v", err)
	}
}
