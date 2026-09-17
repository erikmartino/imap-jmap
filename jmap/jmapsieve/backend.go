package jmapsieve

import (
	"context"

	"imap-jmap/jmap/jmapcore"
)

// SieveBackend defines the storage interface for JMAP for Sieve Scripts (RFC 9661) resources.
type SieveBackend interface {
	SieveScriptState(ctx context.Context) string
	SieveScriptChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool)
	GetSieveScripts(ctx context.Context, ids []jmapcore.Id) (list []*SieveScript, notFound []jmapcore.Id, err error)
	GetAllSieveScripts(ctx context.Context) ([]*SieveScript, error)
	CreateSieveScript(ctx context.Context, script *SieveScript) (*SieveScript, error)
	UpdateSieveScript(ctx context.Context, id jmapcore.Id, patch map[string]any) (*SieveScript, error)
	DeleteSieveScript(ctx context.Context, id jmapcore.Id) (bool, error)
	QuerySieveScripts(ctx context.Context, filter map[string]any, position int, limit *uint64) (ids []jmapcore.Id, total int, err error)
	ValidateSieveScript(ctx context.Context, content string) (isValid bool, errDetail string)
}
