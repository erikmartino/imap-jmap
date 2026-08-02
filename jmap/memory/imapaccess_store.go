package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"imap-jmap/jmap"
)

// MemoryIMAPAccessBackend implements jmap.IMAPAccessBackend for in-memory IMAPAccount storage (RFC 9698).
type MemoryIMAPAccessBackend struct {
	mu        sync.RWMutex
	accounts  map[jmap.Id]*jmap.IMAPAccount
	state     string
	idCounter uint64
}

// NewMemoryIMAPAccessBackend initializes a new MemoryIMAPAccessBackend with a default IMAP account.
func NewMemoryIMAPAccessBackend() *MemoryIMAPAccessBackend {
	b := &MemoryIMAPAccessBackend{
		accounts: make(map[jmap.Id]*jmap.IMAPAccount),
		state:    "imap-1",
	}

	// Create default IMAP account entry per RFC 9698
	defaultAcc := &jmap.IMAPAccount{
		ID:       "imap-acc-1",
		Host:     "imap.example.com",
		Port:     993,
		TLS:      "always",
		Username: "user@example.com",
		State:    "connected",
	}
	b.accounts[defaultAcc.ID] = defaultAcc
	return b
}

func (b *MemoryIMAPAccessBackend) State(ctx context.Context) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *MemoryIMAPAccessBackend) bumpState() {
	b.state = fmt.Sprintf("imap-%d", time.Now().UnixNano())
}

func (b *MemoryIMAPAccessBackend) GetIMAPAccounts(ctx context.Context, ids []jmap.Id) ([]*jmap.IMAPAccount, []jmap.Id, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(ids) == 0 {
		var list []*jmap.IMAPAccount
		for _, acc := range b.accounts {
			list = append(list, acc)
		}
		return list, []jmap.Id{}, nil
	}

	var list []*jmap.IMAPAccount
	var notFound []jmap.Id
	for _, id := range ids {
		if acc, ok := b.accounts[id]; ok {
			list = append(list, acc)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

func (b *MemoryIMAPAccessBackend) GetAllIMAPAccounts(ctx context.Context) ([]*jmap.IMAPAccount, error) {
	list, _, err := b.GetIMAPAccounts(ctx, nil)
	return list, err
}

func (b *MemoryIMAPAccessBackend) CreateIMAPAccount(ctx context.Context, account *jmap.IMAPAccount) (*jmap.IMAPAccount, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.idCounter++
	if account.ID == "" {
		account.ID = jmap.Id(fmt.Sprintf("imap-acc-%d", b.idCounter))
	}

	if account.Host == "" {
		account.Host = "localhost"
	}
	if account.Port == 0 {
		account.Port = 993
	}
	if account.TLS == "" {
		account.TLS = "always"
	}
	if account.State == "" {
		account.State = "connected"
	}

	b.accounts[account.ID] = account
	b.bumpState()
	return account, nil
}

func (b *MemoryIMAPAccessBackend) UpdateIMAPAccount(ctx context.Context, id jmap.Id, patch map[string]any) (*jmap.IMAPAccount, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	acc, ok := b.accounts[id]
	if !ok {
		return nil, fmt.Errorf("IMAPAccount %s not found", id)
	}

	if host, ok := patch["host"].(string); ok {
		acc.Host = host
	}
	if port, ok := patch["port"].(float64); ok {
		acc.Port = uint32(port)
	}
	if tlsOpt, ok := patch["tls"].(string); ok {
		acc.TLS = tlsOpt
	}
	if user, ok := patch["username"].(string); ok {
		acc.Username = user
	}
	if st, ok := patch["state"].(string); ok {
		acc.State = st
	}

	b.bumpState()
	return acc, nil
}

func (b *MemoryIMAPAccessBackend) DeleteIMAPAccount(ctx context.Context, id jmap.Id) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.accounts[id]; !ok {
		return false, nil
	}

	delete(b.accounts, id)
	b.bumpState()
	return true, nil
}
