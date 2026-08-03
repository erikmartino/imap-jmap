package jmap

import (
	"context"
)

// FileNode represents a FileNode object in the JMAP FileNode extension.
type FileNode struct {
	ID        Id     `json:"id"`
	Name      string `json:"name"`
	ParentID  *Id    `json:"parentId,omitempty"`
	BlobID    *Id    `json:"blobId,omitempty"`
	Size      uint64 `json:"size,omitempty"`
	Type      string `json:"type,omitempty"`
	IsFolder  bool   `json:"isFolder"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// RegisterFileNodeHandlers registers FileNode/* method handlers into MethodRegistry.
func RegisterFileNodeHandlers(r *MethodRegistry) {
	r.Register("FileNode/get", handleFileNodeGet())
	r.Register("FileNode/query", handleFileNodeQuery())
	r.Register("FileNode/set", handleFileNodeSet())
	r.Register("FileNode/changes", handleFileNodeChanges())
	r.Register("FileNode/queryChanges", handleFileNodeQueryChanges())
}

func handleFileNodeGet() MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		list := []*FileNode{}
		notFound := []Id{}

		return "FileNode/get", map[string]any{
			"accountId": accountID,
			"state":     "0",
			"list":      list,
			"notFound":  notFound,
		}
	}
}

func handleFileNodeQuery() MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		return "FileNode/query", map[string]any{
			"accountId":           accountID,
			"queryState":          "0",
			"canCalculateChanges": true,
			"position":            0,
			"ids":                 []Id{},
			"total":               0,
		}
	}
}

func handleFileNodeSet() MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		return "FileNode/set", map[string]any{
			"accountId": accountID,
			"oldState":  "0",
			"newState":  "0",
			"created":   map[string]*FileNode{},
			"updated":   map[string]any{},
			"destroyed": []Id{},
		}
	}
}

func handleFileNodeChanges() MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		return "FileNode/changes", map[string]any{
			"accountId":      accountID,
			"oldState":       args["sinceState"],
			"newState":       "0",
			"hasMoreChanges": false,
			"created":        []Id{},
			"updated":        []Id{},
			"destroyed":      []Id{},
		}
	}
}

func handleFileNodeQueryChanges() MethodHandler {
	return func(ctx context.Context, args map[string]any, clientCallID string) (string, map[string]any) {
		accountID, _ := args["accountId"].(string)

		return "FileNode/queryChanges", map[string]any{
			"accountId":     accountID,
			"oldQueryState": args["sinceQueryState"],
			"newQueryState": "0",
			"added":         []any{},
			"removed":       []Id{},
		}
	}
}
