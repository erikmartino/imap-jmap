package jmap_test

import (
	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

func newTestServer(opts ...jmap.Option) *jmap.Server {
	// Wire every advertised capability's backend so the default test server behaves like a
	// real, full-featured server (no advertised method returns "unknown method").
	mb := memory.NewMemoryBackend()
	bb := memory.NewMemoryBlobBackend()
	fb := memory.NewMemoryFileNodeBackend()
	cal := memory.NewMemoryCalendarsBackend()
	contacts := memory.NewMemoryContactsBackend()
	sieve := memory.NewMemorySieveBackend()
	imap := memory.NewMemoryIMAPAccessBackend()

	allOpts := []jmap.Option{
		jmap.WithMailBackend(mb),
		jmap.WithBlobBackend(bb),
		jmap.WithFileNodeBackend(fb),
		jmap.WithCalendarsBackend(cal),
		jmap.WithContactsBackend(contacts),
		jmap.WithSieveBackend(sieve),
		jmap.WithIMAPAccessBackend(imap),
	}
	allOpts = append(allOpts, opts...)

	srv := jmap.NewServer(nil, allOpts...)
	if memAuth, ok := srv.AuthBackend.(*memory.MemoryAuthBackend); ok && memAuth != nil {
		memAuth.SetBackends(mb, cal, contacts, fb)
	}
	mb.SetBroadcaster(srv.Broadcaster)
	cal.SetBroadcaster(srv.Broadcaster)
	contacts.SetBroadcaster(srv.Broadcaster)
	sieve.SetBroadcaster(srv.Broadcaster)
	fb.SetBroadcaster(srv.Broadcaster)
	return srv
}
