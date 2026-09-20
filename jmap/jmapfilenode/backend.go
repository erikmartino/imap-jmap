package jmapfilenode

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// FileNodeBackend defines the storage interface for the JMAP FileNode extension.
type FileNodeBackend interface {
	FileNodeState(ctx context.Context) string
	FileNodeChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetAllFileNodes(ctx context.Context) ([]*FileNode, error)
	GetFileNodes(ctx context.Context, ids []jmapcore.Id) (list []*FileNode, notFound []jmapcore.Id, err error)
	CreateFileNode(ctx context.Context, node *FileNode) (*FileNode, error)
	UpdateFileNode(ctx context.Context, id jmapcore.Id, patch map[string]any) (*FileNode, error)
	DeleteFileNode(ctx context.Context, id jmapcore.Id) (bool, error)
	QueryFileNodes(ctx context.Context, filter map[string]any, position int, limit *uint64) (ids []jmapcore.Id, total int, err error)
}
