package jmapextstore

import (
	"context"
	"encoding/json"
	"fmt"
)

// Store defines an interface for reading, writing, and deleting per-account JMAP extension data upstream.
type Store interface {
	Get(ctx context.Context, accountID, key string) ([]byte, error)
	Put(ctx context.Context, accountID, key string, data []byte) error
	Delete(ctx context.Context, accountID, key string) error
}

// Load loads and unmarshals JSON data for key into target.
// Returns (val, true, nil) if found and unmarshaled, (nil, false, nil) if not found.
func Load[T any](ctx context.Context, s Store, accountID, key string) (*T, bool, error) {
	if s == nil {
		return nil, false, nil
	}
	data, err := s.Get(ctx, accountID, key)
	if err != nil {
		return nil, false, err
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	var val T
	if err := json.Unmarshal(data, &val); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal jmapextstore %s: %w", key, err)
	}
	return &val, true, nil
}

// Save marshals val to JSON and writes it to s for key.
func Save[T any](ctx context.Context, s Store, accountID, key string, val T) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("failed to marshal jmapextstore %s: %w", key, err)
	}
	return s.Put(ctx, accountID, key, data)
}

// Delete removes key from the store.
func Delete(ctx context.Context, s Store, accountID, key string) error {
	if s == nil {
		return nil
	}
	return s.Delete(ctx, accountID, key)
}
