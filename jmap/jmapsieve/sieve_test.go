package jmapsieve

import (
	"context"
	"testing"

	"imap-jmap/jmap/jmapcore"
)

type mockSieveBackend struct {
	scripts map[jmapcore.Id]*SieveScript
	state   string
}

func (m *mockSieveBackend) SieveScriptState(ctx context.Context) string {
	return m.state
}

func (m *mockSieveBackend) SieveScriptChanges(ctx context.Context, sinceState string) (created, updated, destroyed []jmapcore.Id, newState string, hasMoreChanges bool) {
	return nil, nil, nil, m.state, false
}

func (m *mockSieveBackend) GetSieveScripts(ctx context.Context, ids []jmapcore.Id) ([]*SieveScript, []jmapcore.Id, error) {
	var list []*SieveScript
	var notFound []jmapcore.Id
	for _, id := range ids {
		if s, ok := m.scripts[id]; ok {
			list = append(list, s)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (m *mockSieveBackend) GetAllSieveScripts(ctx context.Context) ([]*SieveScript, error) {
	var list []*SieveScript
	for _, s := range m.scripts {
		list = append(list, s)
	}
	return list, nil
}

func (m *mockSieveBackend) CreateSieveScript(ctx context.Context, script *SieveScript) (*SieveScript, error) {
	m.scripts[script.ID] = script
	return script, nil
}

func (m *mockSieveBackend) UpdateSieveScript(ctx context.Context, id jmapcore.Id, patch map[string]any) (*SieveScript, error) {
	s, ok := m.scripts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *mockSieveBackend) DeleteSieveScript(ctx context.Context, id jmapcore.Id) (bool, error) {
	delete(m.scripts, id)
	return true, nil
}

func (m *mockSieveBackend) QuerySieveScripts(ctx context.Context, filter map[string]any, position int, limit *uint64) ([]jmapcore.Id, int, error) {
	var ids []jmapcore.Id
	for id := range m.scripts {
		ids = append(ids, id)
	}
	return ids, len(ids), nil
}

func (m *mockSieveBackend) ValidateSieveScript(ctx context.Context, content string) (bool, string) {
	if content == "invalid" {
		return false, "syntax error"
	}
	return true, ""
}

func TestSieveScriptValidation(t *testing.T) {
	backend := &mockSieveBackend{
		scripts: make(map[jmapcore.Id]*SieveScript),
		state:   "s1",
	}

	valid, _ := backend.ValidateSieveScript(context.Background(), "require [\"fileinto\"];")
	if !valid {
		t.Errorf("expected script to be valid")
	}

	invalid, detail := backend.ValidateSieveScript(context.Background(), "invalid")
	if invalid || detail != "syntax error" {
		t.Errorf("expected script to be invalid with syntax error, got valid=%v detail=%s", invalid, detail)
	}
}

func TestRegisterSieveHandlers(t *testing.T) {
	backend := &mockSieveBackend{
		scripts: make(map[jmapcore.Id]*SieveScript),
		state:   "s1",
	}
	reg := NewMethodRegistry()
	RegisterSieveHandlers(reg, backend)

	expectedMethods := []string{
		"SieveScript/get",
		"SieveScript/changes",
		"SieveScript/set",
		"SieveScript/query",
		"SieveScript/queryChanges",
		"SieveScript/validate",
	}

	for _, m := range expectedMethods {
		h, ok := reg.Get(m)
		if !ok || h == nil {
			t.Errorf("expected method handler registered for %s", m)
		}
	}
}
