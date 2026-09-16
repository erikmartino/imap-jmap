package jmapblob_test

import (
	"testing"

	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/spectest"
)

func TestBlobPrimitives(t *testing.T) {
	spectest.RequireID(t, "RFC8620#6-p1-MUST", "Binary blob object representation")

	b := jmapblob.Blob{
		ID:        "b-1",
		BlobID:    "b-1",
		AccountID: "acc-1",
		Type:      "text/plain",
		Size:      12,
		Data:      []byte("Hello World!"),
	}

	if b.ID != "b-1" || b.Size != 12 || string(b.Data) != "Hello World!" {
		t.Fatalf("unexpected Blob values: %+v", b)
	}
}
