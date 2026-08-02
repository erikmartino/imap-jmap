package jmap_test

import (
	"imap-jmap/jmap"
	"imap-jmap/jmap/memory"
)

func newTestServer(opts ...jmap.Option) *jmap.Server {
	mb := memory.NewMemoryBackend()
	bb := memory.NewMemoryBlobBackend()

	allOpts := []jmap.Option{
		jmap.WithMailBackend(mb),
		jmap.WithBlobBackend(bb),
	}
	allOpts = append(allOpts, opts...)

	srv := jmap.NewServer(nil, allOpts...)
	mb.SetBroadcaster(srv.Broadcaster)
	return srv
}
