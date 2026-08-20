package memory

import (
	"context"
	"imap-jmap/jmap"
)

func (mb *MemoryBackend) ThreadState(ctx context.Context) string {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.threadState.State()
}

// ThreadChanges returns created, updated, and destroyed Threads since sinceState.
func (mb *MemoryBackend) ThreadChanges(ctx context.Context, sinceState string, maxChanges *uint64) ([]jmap.Id, []jmap.Id, []jmap.Id, string, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	us := mb.getStoreLocked(ctx)
	return us.threadState.Changes(sinceState, maxChanges)
}

// EmailState returns current change state token for Email resources.

func (mb *MemoryBackend) GetThreads(ctx context.Context, ids []jmap.Id) ([]*jmap.Thread, []jmap.Id, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	var list []*jmap.Thread
	var notFound []jmap.Id

	for _, id := range ids {
		if item, ok := us.threads[id]; ok {
			list = append(list, item)
		} else {
			notFound = append(notFound, id)
		}
	}
	return list, notFound, nil
}

// GetAllThreads retrieves all threads.
func (mb *MemoryBackend) GetAllThreads(ctx context.Context) ([]*jmap.Thread, error) {
	mb.mu.RLock()
	us := mb.getStoreLocked(ctx)
	defer mb.mu.RUnlock()

	list := make([]*jmap.Thread, 0, len(us.threads))
	for _, item := range us.threads {
		list = append(list, item)
	}
	return list, nil
}

// GetEmails retrieves emails by ID.
