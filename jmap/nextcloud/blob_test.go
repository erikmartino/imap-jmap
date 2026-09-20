package nextcloud_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"

	"imap-jmap/jmap/jmapauth"
	"imap-jmap/jmap/jmapblob"
	"imap-jmap/jmap/jmapcore"
	"imap-jmap/jmap/jmapfilenode"
	"imap-jmap/jmap/jmappush"
	"imap-jmap/jmap/nextcloud"
)

type mockFallbackBlobBackend struct {
	blobs map[string]*jmapblob.Blob
}

func (m *mockFallbackBlobBackend) PutBlob(ctx context.Context, accountID, contentType string, data []byte) (*jmapblob.Blob, error) {
	hash := sha256.Sum256(data)
	id := hex.EncodeToString(hash[:])
	b := &jmapblob.Blob{
		ID:        id,
		AccountID: accountID,
		Data:      data,
		Size:      int64(len(data)),
		Type:      contentType,
	}
	m.blobs[id] = b
	return b, nil
}

func (m *mockFallbackBlobBackend) GetBlob(ctx context.Context, accountID, blobID string) (*jmapblob.Blob, bool, error) {
	b, ok := m.blobs[blobID]
	return b, ok, nil
}

func (m *mockFallbackBlobBackend) GetAllBlobs(ctx context.Context, accountID string) ([]*jmapblob.Blob, error) {
	var res []*jmapblob.Blob
	for _, b := range m.blobs {
		res = append(res, b)
	}
	return res, nil
}

func (m *mockFallbackBlobBackend) CopyBlob(ctx context.Context, fromAccountID, toAccountID string, blobID string) (*jmapblob.Blob, error) {
	b, ok := m.blobs[blobID]
	if !ok {
		return nil, jmapblob.ErrBlobNotFound
	}
	return m.PutBlob(ctx, toAccountID, b.Type, bytes.Clone(b.Data))
}

func (m *mockFallbackBlobBackend) LookupBlobReferences(ctx context.Context, typeNames []string, blobID jmapcore.Id) (map[string][]jmapcore.Id, error) {
	res := make(map[string][]jmapcore.Id)
	for _, tn := range typeNames {
		if tn == "Email" {
			res["Email"] = []jmapcore.Id{"email-123"}
		}
	}
	return res, nil
}

func TestEmbeddedNextcloudBlobBackend(t *testing.T) {
	user1 := "user@example.com"
	user2 := "user2@example.com"
	client, _, _, fileNodeBackend, _, blobBackend, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user1, user2)
	defer cleanup()

	ctx1 := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user1),
			user1,
		),
		user1, user1,
	)
	ctx2 := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user2),
			user2,
		),
		user2, user2,
	)

	// 1. PutBlob in user1
	testContent := []byte("Hello Nextcloud WebDAV Blob Storage!")
	blob, err := blobBackend.PutBlob(ctx1, user1, "text/plain", testContent)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}
	if blob == nil || blob.ID == "" {
		t.Fatalf("PutBlob returned nil or empty blob ID")
	}
	if blob.Size != int64(len(testContent)) {
		t.Errorf("Blob size mismatch: got %d, want %d", blob.Size, len(testContent))
	}
	if blob.Type != "text/plain" {
		t.Errorf("Blob type mismatch: got %s, want text/plain", blob.Type)
	}

	// Verify blob was persisted on WebDAV under .blobs/{blobID}
	fs1, _, err := client.WebDAV(ctx1)
	if err != nil {
		t.Fatalf("client.WebDAV failed: %v", err)
	}
	rc, err := fs1.Open(ctx1, ".blobs/"+blob.ID)
	if err != nil {
		t.Fatalf("Failed to open .blobs/%s on WebDAV: %v", blob.ID, err)
	}
	davBytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("Failed to read .blobs/%s: %v", blob.ID, err)
	}
	if !bytes.Equal(davBytes, testContent) {
		t.Errorf("WebDAV content mismatch: got %q, want %q", davBytes, testContent)
	}

	// 2. GetBlob (cached hit)
	fetched, found, err := blobBackend.GetBlob(ctx1, user1, blob.ID)
	if err != nil {
		t.Fatalf("GetBlob failed: %v", err)
	}
	if !found || fetched == nil {
		t.Fatalf("GetBlob did not find blob %s", blob.ID)
	}
	if !bytes.Equal(fetched.Data, testContent) {
		t.Errorf("GetBlob data mismatch: got %q, want %q", fetched.Data, testContent)
	}

	// 3. GetBlob (WebDAV read - simulate cache miss by creating a fresh backend instance)
	freshBlobBackend := nextcloud.NewBlobBackend(client, fileNodeBackend)
	fetchedFromDav, foundDav, err := freshBlobBackend.GetBlob(ctx1, user1, blob.ID)
	if err != nil {
		t.Fatalf("fresh GetBlob failed: %v", err)
	}
	if !foundDav || fetchedFromDav == nil {
		t.Fatalf("fresh GetBlob failed to find blob on WebDAV")
	}
	if !bytes.Equal(fetchedFromDav.Data, testContent) {
		t.Errorf("fresh GetBlob data mismatch: got %q, want %q", fetchedFromDav.Data, testContent)
	}
	if fetchedFromDav.Type != "text/plain" {
		t.Errorf("fresh GetBlob type mismatch: got %s, want text/plain", fetchedFromDav.Type)
	}

	// 4. GetAllBlobs
	allBlobs, err := blobBackend.GetAllBlobs(ctx1, user1)
	if err != nil {
		t.Fatalf("GetAllBlobs failed: %v", err)
	}
	if len(allBlobs) == 0 {
		t.Fatalf("GetAllBlobs returned 0 blobs, expected at least 1")
	}

	// 5. CopyBlob from user1 to user2
	copied, err := blobBackend.CopyBlob(ctx1, user1, user2, blob.ID)
	if err != nil {
		t.Fatalf("CopyBlob failed: %v", err)
	}
	if copied == nil || copied.ID != blob.ID {
		t.Fatalf("CopyBlob returned unexpected blob: %v", copied)
	}

	// Verify user2 now has the blob
	fetchedUser2, foundUser2, err := blobBackend.GetBlob(ctx2, user2, blob.ID)
	if err != nil || !foundUser2 || fetchedUser2 == nil {
		t.Fatalf("User2 GetBlob failed or not found: %v", err)
	}
	if !bytes.Equal(fetchedUser2.Data, testContent) {
		t.Errorf("User2 blob content mismatch: got %q, want %q", fetchedUser2.Data, testContent)
	}

	// 6. LookupBlobReferences
	// Before creating a FileNode referencing it:
	refs, err := blobBackend.LookupBlobReferences(ctx1, []string{"FileNode"}, jmapcore.Id(blob.ID))
	if err != nil {
		t.Fatalf("LookupBlobReferences failed: %v", err)
	}
	if len(refs["FileNode"]) != 0 {
		t.Errorf("Expected 0 FileNode refs before creating node, got %v", refs["FileNode"])
	}

	// Now create a FileNode referencing it:
	bid := jmapcore.Id(blob.ID)
	fn, err := fileNodeBackend.CreateFileNode(ctx1, &jmapfilenode.FileNode{
		Name:     "attached.txt",
		BlobID:   &bid,
		Size:     uint64(len(testContent)),
		Type:     "text/plain",
		IsFolder: false,
	})
	if err != nil {
		t.Fatalf("CreateFileNode failed: %v", err)
	}

	refsAfter, err := blobBackend.LookupBlobReferences(ctx1, []string{"FileNode"}, jmapcore.Id(blob.ID))
	if err != nil {
		t.Fatalf("LookupBlobReferences after node creation failed: %v", err)
	}
	if len(refsAfter["FileNode"]) != 1 || refsAfter["FileNode"][0] != fn.ID {
		t.Errorf("Expected FileNode ref [%s], got %v", fn.ID, refsAfter["FileNode"])
	}

	// 7. Fallback support
	mockFB := &mockFallbackBlobBackend{
		blobs: make(map[string]*jmapblob.Blob),
	}
	fallbackBlob, _ := mockFB.PutBlob(ctx1, user1, "image/png", []byte{0x89, 0x50, 0x4e, 0x47})
	blobBackend.SetFallback(mockFB)

	// Fetching fallback blob through blobBackend
	fetchedFallback, foundFB, err := blobBackend.GetBlob(ctx1, user1, fallbackBlob.ID)
	if err != nil || !foundFB || fetchedFallback == nil {
		t.Fatalf("Fallback GetBlob failed: %v", err)
	}
	if !bytes.Equal(fetchedFallback.Data, fallbackBlob.Data) {
		t.Errorf("Fallback data mismatch")
	}

	// Lookup references on fallback
	refsWithFallback, err := blobBackend.LookupBlobReferences(ctx1, []string{"Email", "FileNode"}, jmapcore.Id(blob.ID))
	if err != nil {
		t.Fatalf("LookupBlobReferences with fallback failed: %v", err)
	}
	if len(refsWithFallback["Email"]) != 1 || refsWithFallback["Email"][0] != "email-123" {
		t.Errorf("Expected Email ref from fallback: got %v", refsWithFallback["Email"])
	}
}

func TestEmbeddedNextcloudExistingFilesDiscovery(t *testing.T) {
	user := "user@example.com"
	_, _, _, fileNodeBackend, _, blobBackend, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user)
	defer cleanup()

	ctx := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user),
			user,
		),
		user, user,
	)

	// Call GetAllFileNodes — Nextcloud already created initial test data for us!
	nodes, err := fileNodeBackend.GetAllFileNodes(ctx)
	if err != nil {
		t.Fatalf("GetAllFileNodes failed: %v", err)
	}

	nodeByName := make(map[string]*jmapfilenode.FileNode)
	for _, n := range nodes {
		nodeByName[n.Name] = n
	}

	// Check welcome.txt
	welcome, ok := nodeByName["welcome.txt"]
	if !ok {
		t.Fatalf("welcome.txt was not discovered in WebDAV files")
	}
	if welcome.IsFolder {
		t.Errorf("welcome.txt should not be a folder")
	}
	if welcome.Type != "text/plain" {
		t.Errorf("welcome.txt type got %q, want text/plain", welcome.Type)
	}
	expectedSize := uint64(len("Welcome to Nextcloud on JMAP!"))
	if welcome.Size != expectedSize {
		t.Errorf("welcome.txt size got %d, want %d", welcome.Size, expectedSize)
	}
	if welcome.BlobID == nil || *welcome.BlobID == "" {
		t.Fatalf("welcome.txt missing BlobID")
	}

	// Fetch welcome.txt's blob from BlobBackend
	welcomeBlob, found, err := blobBackend.GetBlob(ctx, user, string(*welcome.BlobID))
	if err != nil || !found || welcomeBlob == nil {
		t.Fatalf("Failed to fetch welcome.txt blob %s: %v", *welcome.BlobID, err)
	}
	if string(welcomeBlob.Data) != "Welcome to Nextcloud on JMAP!" {
		t.Errorf("welcome.txt blob content mismatch: got %q", string(welcomeBlob.Data))
	}

	// Check Readme.md
	readme, ok := nodeByName["Readme.md"]
	if !ok {
		t.Fatalf("Readme.md was not discovered in WebDAV")
	}
	if readme.Type != "text/markdown" {
		t.Errorf("Readme.md type got %q, want text/markdown", readme.Type)
	}

	// Check Photos folder
	photos, ok := nodeByName["Photos"]
	if !ok {
		t.Fatalf("Photos folder was not discovered in WebDAV")
	}
	if !photos.IsFolder {
		t.Errorf("Photos should be a folder")
	}

	// Check banner.jpg inside Photos
	banner, ok := nodeByName["banner.jpg"]
	if !ok {
		t.Fatalf("banner.jpg was not discovered in WebDAV")
	}
	if banner.ParentID == nil || *banner.ParentID != photos.ID {
		t.Errorf("banner.jpg parentId mismatch: got %v, want %s", banner.ParentID, photos.ID)
	}
	if banner.Type != "image/jpeg" {
		t.Errorf("banner.jpg type got %q, want image/jpeg", banner.Type)
	}

	// Fetch banner.jpg's blob
	bannerBlob, found, err := blobBackend.GetBlob(ctx, user, string(*banner.BlobID))
	if err != nil || !found || bannerBlob == nil {
		t.Fatalf("Failed to fetch banner.jpg blob %s: %v", *banner.BlobID, err)
	}
	if string(bannerBlob.Data) != "Sample JPEG image data for Nextcloud Photos." {
		t.Errorf("banner.jpg blob content mismatch: got %q", string(bannerBlob.Data))
	}
}

func TestEmbeddedNextcloudFileNodesCRUD(t *testing.T) {
	user := "user@example.com"
	client, _, _, fileNodeBackend, _, blobBackend, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user)
	defer cleanup()

	ctx := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user),
			user,
		),
		user, user,
	)

	// 1. PutBlob to get a valid blobID
	fileData := []byte("Production WebDAV File Node Content")
	blob, err := blobBackend.PutBlob(ctx, user, "text/plain", fileData)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	// 2. Create FileNode using this blobID
	bid := jmapcore.Id(blob.ID)
	fileNode := &jmapfilenode.FileNode{
		Name:     "data.txt",
		BlobID:   &bid,
		Size:     uint64(len(fileData)),
		Type:     "text/plain",
		IsFolder: false,
	}
	createdFile, err := fileNodeBackend.CreateFileNode(ctx, fileNode)
	if err != nil {
		t.Fatalf("CreateFileNode failed: %v", err)
	}
	if createdFile.ID == "" {
		t.Fatalf("Created FileNode missing ID")
	}

	// Verify that the file content was ACTUALLY written to WebDAV (not 0 bytes)
	fs, _, err := client.WebDAV(ctx)
	if err != nil {
		t.Fatalf("client.WebDAV failed: %v", err)
	}
	rc, err := fs.Open(ctx, "data.txt")
	if err != nil {
		t.Fatalf("Failed to open data.txt on WebDAV: %v", err)
	}
	readBytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("Failed to read data.txt: %v", err)
	}
	if !bytes.Equal(readBytes, fileData) {
		t.Errorf("WebDAV content mismatch: got %q, want %q", readBytes, fileData)
	}

	// 3. Create a folder
	folderNode := &jmapfilenode.FileNode{
		Name:     "Projects",
		IsFolder: true,
		Type:     "folder",
	}
	createdFolder, err := fileNodeBackend.CreateFileNode(ctx, folderNode)
	if err != nil {
		t.Fatalf("CreateFileNode folder failed: %v", err)
	}

	// 4. Update FileNode: move data.txt into Projects
	updatedFile, err := fileNodeBackend.UpdateFileNode(ctx, createdFile.ID, map[string]any{
		"parentId": string(createdFolder.ID),
	})
	if err != nil {
		t.Fatalf("UpdateFileNode move failed: %v", err)
	}
	if updatedFile.ParentID == nil || *updatedFile.ParentID != createdFolder.ID {
		t.Errorf("ParentID after move mismatch: got %v, want %s", updatedFile.ParentID, createdFolder.ID)
	}

	// Verify WebDAV path moved to Projects/data.txt
	rcMoved, err := fs.Open(ctx, "Projects/data.txt")
	if err != nil {
		t.Fatalf("Failed to open Projects/data.txt on WebDAV after move: %v", err)
	}
	movedBytes, _ := io.ReadAll(rcMoved)
	_ = rcMoved.Close()
	if !bytes.Equal(movedBytes, fileData) {
		t.Errorf("Projects/data.txt content mismatch: got %q, want %q", movedBytes, fileData)
	}

	// 5. Update FileNode: rename to "renamed.txt"
	renamedFile, err := fileNodeBackend.UpdateFileNode(ctx, createdFile.ID, map[string]any{
		"name": "renamed.txt",
	})
	if err != nil {
		t.Fatalf("UpdateFileNode rename failed: %v", err)
	}
	if renamedFile.Name != "renamed.txt" {
		t.Errorf("Name after rename mismatch: got %s, want renamed.txt", renamedFile.Name)
	}

	// Verify WebDAV path is now Projects/renamed.txt
	rcRenamed, err := fs.Open(ctx, "Projects/renamed.txt")
	if err != nil {
		t.Fatalf("Failed to open Projects/renamed.txt on WebDAV after rename: %v", err)
	}
	renamedBytes, _ := io.ReadAll(rcRenamed)
	_ = rcRenamed.Close()
	if !bytes.Equal(renamedBytes, fileData) {
		t.Errorf("Projects/renamed.txt content mismatch: got %q, want %q", renamedBytes, fileData)
	}

	// 6. Query FileNodes with filter
	// Query by parentId == createdFolder.ID
	pIDs, total, err := fileNodeBackend.QueryFileNodes(ctx, map[string]any{
		"parentId": string(createdFolder.ID),
	}, 0, nil)
	if err != nil {
		t.Fatalf("QueryFileNodes failed: %v", err)
	}
	if total != 1 || len(pIDs) != 1 || pIDs[0] != createdFile.ID {
		t.Errorf("Query by parentId failed: got %v (total=%d), want [%s]", pIDs, total, createdFile.ID)
	}

	// Query isFolder: true
	fIDs, fTotal, err := fileNodeBackend.QueryFileNodes(ctx, map[string]any{
		"isFolder": true,
	}, 0, nil)
	if err != nil {
		t.Fatalf("QueryFileNodes isFolder failed: %v", err)
	}
	foundCreatedFolder := false
	for _, fid := range fIDs {
		if fid == createdFolder.ID {
			foundCreatedFolder = true
		}
	}
	if !foundCreatedFolder || fTotal < 1 {
		t.Errorf("Query by isFolder failed: got %v (total=%d), want to contain [%s]", fIDs, fTotal, createdFolder.ID)
	}

	// 7. Delete FileNode
	deleted, err := fileNodeBackend.DeleteFileNode(ctx, createdFile.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteFileNode failed: %v", err)
	}

	// Verify it is no longer found in GetFileNodes
	_, notFound, err := fileNodeBackend.GetFileNodes(ctx, []jmapcore.Id{createdFile.ID})
	if err != nil {
		t.Fatalf("GetFileNodes after delete failed: %v", err)
	}
	if len(notFound) != 1 || notFound[0] != createdFile.ID {
		t.Errorf("Expected deleted node in notFound: got %v", notFound)
	}

	// Delete folder
	deletedFolder, err := fileNodeBackend.DeleteFileNode(ctx, createdFolder.ID)
	if err != nil || !deletedFolder {
		t.Fatalf("DeleteFileNode folder failed: %v", err)
	}
}

func TestEmbeddedNextcloudBlobEdgeCases(t *testing.T) {
	user := "edgecases@example.com"
	_, _, _, fileNodeBackend, _, blobBackend, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user)
	defer cleanup()

	ctx := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user),
			user,
		),
		user, user,
	)

	// SetFileNodeBackend coverage
	blobBackend.SetFileNodeBackend(fileNodeBackend)
	if fileNodeBackend.BlobBackend() != blobBackend {
		t.Errorf("BlobBackend getter mismatch")
	}

	// GetBlob nonexistent
	nonExistent, found, err := blobBackend.GetBlob(ctx, user, "does-not-exist-blob-id")
	if err != nil {
		t.Fatalf("GetBlob nonexistent returned error: %v", err)
	}
	if found || nonExistent != nil {
		t.Errorf("Expected not found for non-existent blob")
	}

	// CopyBlob nonexistent source
	_, err = blobBackend.CopyBlob(ctx, user, "other@example.com", "does-not-exist-blob-id")
	if err != jmapblob.ErrBlobNotFound {
		t.Errorf("Expected ErrBlobNotFound, got %v", err)
	}

	// PutBlob with empty contentType defaults to application/octet-stream
	b, err := blobBackend.PutBlob(ctx, user, "", []byte("raw data"))
	if err != nil {
		t.Fatalf("PutBlob empty content-type failed: %v", err)
	}
	if b.Type != "application/octet-stream" {
		t.Errorf("Expected application/octet-stream, got %s", b.Type)
	}
}

func TestEmbeddedNextcloudChangeTrackingAndPush(t *testing.T) {
	user := "pushuser@example.com"
	_, _, _, fileNodeBackend, _, _, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user)
	defer cleanup()

	ctx := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user),
			user,
		),
		user, user,
	)

	// Setup broadcaster
	broadcaster := jmappush.NewBroadcaster()
	fileNodeBackend.SetBroadcaster(broadcaster)

	ch := broadcaster.Subscribe(user)
	defer broadcaster.Unsubscribe(ch)

	initState := fileNodeBackend.FileNodeState(ctx)
	if initState == "" {
		t.Fatalf("Initial state should not be empty")
	}

	// Create a node
	node, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name:     "pushed.txt",
		IsFolder: false,
	})
	if err != nil {
		t.Fatalf("CreateFileNode failed: %v", err)
	}

	stateAfterCreate := fileNodeBackend.FileNodeState(ctx)
	if stateAfterCreate == initState {
		t.Errorf("State did not advance after CreateFileNode")
	}

	// Check changes
	created, _, _, _, _ := fileNodeBackend.FileNodeChanges(ctx, initState)
	if len(created) != 1 || created[0] != node.ID {
		t.Errorf("Changes created mismatch: got %v, want [%s]", created, node.ID)
	}

	// Update node
	_, err = fileNodeBackend.UpdateFileNode(ctx, node.ID, map[string]any{
		"name": "pushed_renamed.txt",
	})
	if err != nil {
		t.Fatalf("UpdateFileNode failed: %v", err)
	}

	_, updatedAfter, _, _, _ := fileNodeBackend.FileNodeChanges(ctx, stateAfterCreate)
	if len(updatedAfter) != 1 || updatedAfter[0] != node.ID {
		t.Errorf("Changes updated mismatch: got %v, want [%s]", updatedAfter, node.ID)
	}

	stateAfterUpdate := fileNodeBackend.FileNodeState(ctx)

	// Delete node
	_, err = fileNodeBackend.DeleteFileNode(ctx, node.ID)
	if err != nil {
		t.Fatalf("DeleteFileNode failed: %v", err)
	}

	_, _, destroyedAfter, _, _ := fileNodeBackend.FileNodeChanges(ctx, stateAfterUpdate)
	if len(destroyedAfter) != 1 || destroyedAfter[0] != node.ID {
		t.Errorf("Changes destroyed mismatch: got %v, want [%s]", destroyedAfter, node.ID)
	}
}

func TestEmbeddedNextcloudFileNodeEdgeCases(t *testing.T) {
	user := "edgefiles@example.com"
	_, _, _, fileNodeBackend, _, _, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user)
	defer cleanup()

	ctx := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user),
			user,
		),
		user, user,
	)

	// Create nodes with various extensions to test MIME type determination
	exts := []struct {
		name     string
		wantType string
	}{
		{"custom_readme.md", "text/markdown"},
		{"config.json", "application/json"},
		{"doc.pdf", "application/pdf"},
		{"image.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"archive.customext", "application/octet-stream"},
	}

	for _, tc := range exts {
		n, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
			Name:     tc.name,
			IsFolder: false,
		})
		if err != nil {
			t.Fatalf("CreateFileNode for %s failed: %v", tc.name, err)
		}
		if n.Type != tc.wantType {
			t.Errorf("MIME type for %s got %q, want %q", tc.name, n.Type, tc.wantType)
		}
	}

	// Update non-existent file
	_, err := fileNodeBackend.UpdateFileNode(ctx, "non-existent-id", map[string]any{"name": "test"})
	if err != jmapcore.ErrNotFound {
		t.Errorf("Expected ErrNotFound on update non-existent, got %v", err)
	}

	// Delete non-existent file
	okDel, err := fileNodeBackend.DeleteFileNode(ctx, "non-existent-id")
	if err != nil || okDel {
		t.Errorf("Expected okDel=false and err=nil on delete non-existent, got ok=%v, err=%v", okDel, err)
	}

	// Query with pagination and negative position
	limit := uint64(2)
	ids, total, err := fileNodeBackend.QueryFileNodes(ctx, nil, -1, &limit)
	if err != nil {
		t.Fatalf("QueryFileNodes with negative position failed: %v", err)
	}
	if total < len(exts) {
		t.Errorf("QueryFileNodes total mismatch: got %d, want at least %d", total, len(exts))
	}
	if len(ids) > 2 {
		t.Errorf("QueryFileNodes limit not respected: got %d", len(ids))
	}

	// Query by name filter
	nameIDs, nameTotal, err := fileNodeBackend.QueryFileNodes(ctx, map[string]any{
		"name": "custom_readme.md",
	}, 0, nil)
	if err != nil || nameTotal != 1 || len(nameIDs) != 1 {
		t.Errorf("QueryFileNodes by name failed: got total=%d ids=%v", nameTotal, nameIDs)
	}

	// Query by type filter
	typeIDs, typeTotal, err := fileNodeBackend.QueryFileNodes(ctx, map[string]any{
		"type": "application/json",
	}, 0, nil)
	if err != nil || typeTotal != 1 || len(typeIDs) != 1 {
		t.Errorf("QueryFileNodes by type failed: got total=%d ids=%v", typeTotal, typeIDs)
	}
}

func TestEmbeddedNextcloudBlobAndFileNodeFullCoverage(t *testing.T) {
	user := "coverage_user@example.com"
	client, _, _, fileNodeBackend, _, blobBackend, cleanup := nextcloud.NewEmbeddedBackendWithBlobs(user)
	defer cleanup()

	ctx := jmapauth.ContextWithCredentials(
		jmapauth.ContextWithSubject(
			jmapauth.ContextWithAccountID(context.Background(), user),
			user,
		),
		user, user,
	)

	// 1. SetFileNodeBackend with nil, and re-setting same backend
	blobBackend.SetFileNodeBackend(nil)
	blobBackend.SetFileNodeBackend(fileNodeBackend)
	blobBackend.SetFileNodeBackend(fileNodeBackend) // no-op branch

	// 2. RegisterCachedBlob with nil blob
	blobBackend.RegisterCachedBlob(user, nil)

	// 3. User resolution with empty / unauthenticated context
	emptyCtx := context.Background()
	_, found, _ := blobBackend.GetBlob(emptyCtx, "", "nonexistent")
	if found {
		t.Errorf("Expected not found with empty ctx")
	}

	// 4. GetAllBlobs with fallback merging
	mockFB := &mockFallbackBlobBackend{blobs: make(map[string]*jmapblob.Blob)}
	fbBlob, _ := mockFB.PutBlob(ctx, user, "text/plain", []byte("fallback blob"))
	blobBackend.SetFallback(mockFB)

	allWithFallback, err := blobBackend.GetAllBlobs(ctx, user)
	if err != nil {
		t.Fatalf("GetAllBlobs with fallback failed: %v", err)
	}
	foundFBBlob := false
	for _, b := range allWithFallback {
		if b.ID == fbBlob.ID {
			foundFBBlob = true
			break
		}
	}
	if !foundFBBlob {
		t.Errorf("Expected fallback blob in GetAllBlobs")
	}

	// 5. CopyBlob with invalid toAccount
	_, err = blobBackend.CopyBlob(ctx, user, "", fbBlob.ID)
	if err == nil {
		t.Errorf("Expected error when copying to empty toAccountID")
	}

	// 6. GetBlob fetching from WebDAV via FileNode without pre-cached blob in BlobBackend
	// We know welcome.txt was seeded automatically by Nextcloud!
	welcomePath, _, _, err := fileNodeBackend.GetFileByBlobID(ctx, "nonexistent-blob")
	if err == nil || welcomePath != "" {
		t.Errorf("Expected error for nonexistent blob from GetFileByBlobID")
	}

	// Find the blobID of welcome.txt
	nodes, _ := fileNodeBackend.GetAllFileNodes(ctx)
	var welcomeNode *jmapfilenode.FileNode
	for _, n := range nodes {
		if n.Name == "welcome.txt" {
			welcomeNode = n
			break
		}
	}
	if welcomeNode != nil && welcomeNode.BlobID != nil {
		// Fresh backend created AFTER GetAllFileNodes has an empty blobCache,
		// so it must fall back to opening the WebDAV file via FileNode!
		freshBlobBackend := nextcloud.NewBlobBackend(client, nil)
		freshBlobBackend.SetFileNodeBackend(fileNodeBackend)
		b, found, err := freshBlobBackend.GetBlob(ctx, user, string(*welcomeNode.BlobID))
		if err != nil || !found || b == nil {
			t.Fatalf("freshBlobBackend.GetBlob for WebDAV file failed: %v", err)
		}
		if string(b.Data) != "Welcome to Nextcloud on JMAP!" {
			t.Errorf("welcome.txt data mismatch: got %q", string(b.Data))
		}
	}

	// 7. MIME type edge cases for .csv, .html, .svg, .zip, .tar.gz
	moreExts := []struct {
		name     string
		wantType string
	}{
		{"table.csv", "text/csv"},
		{"index.html", "text/html"},
		{"logo.svg", "image/svg+xml"},
		{"archive.zip", "application/zip"},
		{"backup.tar.gz", "application/gzip"},
	}
	for _, tc := range moreExts {
		n, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
			Name:     tc.name,
			IsFolder: false,
		})
		if err != nil {
			t.Fatalf("CreateFileNode for %s failed: %v", tc.name, err)
		}
		if n.Type != tc.wantType {
			t.Errorf("MIME type for %s got %q, want %q", tc.name, n.Type, tc.wantType)
		}
	}

	// 8. CreateFileNode error conditions
	_, err = fileNodeBackend.CreateFileNode(emptyCtx, &jmapfilenode.FileNode{Name: "test.txt"})
	if err == nil {
		t.Errorf("Expected error with unauthenticated ctx")
	}
	_, err = fileNodeBackend.CreateFileNode(ctx, nil)
	if err == nil {
		t.Errorf("Expected error with nil node")
	}
	_, err = fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{Name: ""})
	if err == nil {
		t.Errorf("Expected error with empty name")
	}
	badParent := jmapcore.Id("nonexistent-parent")
	_, err = fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name:     "child.txt",
		ParentID: &badParent,
	})
	if err == nil {
		t.Errorf("Expected error with nonexistent parentId")
	}

	// 9. UpdateFileNode patch combinations
	// Create folder and file
	folder, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name:     "TestFolder",
		IsFolder: true,
	})
	if err != nil {
		t.Fatalf("Failed to create folder: %v", err)
	}

	childFile, err := fileNodeBackend.CreateFileNode(ctx, &jmapfilenode.FileNode{
		Name:     "subfile.txt",
		ParentID: &folder.ID,
	})
	if err != nil {
		t.Fatalf("Failed to create child file: %v", err)
	}

	// Move child file to root by setting parentId: nil
	movedToRoot, err := fileNodeBackend.UpdateFileNode(ctx, childFile.ID, map[string]any{
		"parentId": nil,
		"type":     "text/plain",
		"size":     float64(50),
		"isFolder": false,
		"blobId":   nil,
	})
	if err != nil {
		t.Fatalf("UpdateFileNode move to root failed: %v", err)
	}
	if movedToRoot.ParentID != nil {
		t.Errorf("Expected nil parentId after moving to root, got %v", movedToRoot.ParentID)
	}

	// Update file with a new blobId to verify underlying WebDAV content is updated
	newContent := []byte("Brand New WebDAV Content via UpdateFileNode")
	newBlob, err := blobBackend.PutBlob(ctx, user, "text/plain", newContent)
	if err != nil {
		t.Fatalf("PutBlob failed: %v", err)
	}

	updatedWithBlob, err := fileNodeBackend.UpdateFileNode(ctx, childFile.ID, map[string]any{
		"blobId": newBlob.ID,
	})
	if err != nil {
		t.Fatalf("UpdateFileNode with blobId failed: %v", err)
	}
	if updatedWithBlob.BlobID == nil || string(*updatedWithBlob.BlobID) != newBlob.ID {
		t.Errorf("BlobID not updated on node")
	}

	// Verify WebDAV content on disk matches newContent
	fs, _, _ := client.WebDAV(ctx)
	rc, err := fs.Open(ctx, "subfile.txt")
	if err != nil {
		t.Fatalf("Failed to open subfile.txt on WebDAV: %v", err)
	}
	contentRead, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(contentRead, newContent) {
		t.Errorf("WebDAV content after blob update mismatch: got %q, want %q", contentRead, newContent)
	}

	// 10. UpdateFileNode error cases
	_, err = fileNodeBackend.UpdateFileNode(emptyCtx, childFile.ID, map[string]any{"name": "x"})
	if err == nil {
		t.Errorf("Expected error updating with empty ctx")
	}
	_, err = fileNodeBackend.UpdateFileNode(ctx, childFile.ID, nil)
	if err == nil {
		t.Errorf("Expected error updating with nil patch")
	}

	// 11. DeleteFileNode error cases
	_, err = fileNodeBackend.DeleteFileNode(emptyCtx, childFile.ID)
	if err == nil {
		t.Errorf("Expected error deleting with empty ctx")
	}
	// Attempt to delete root
	okDelRoot, err := fileNodeBackend.DeleteFileNode(ctx, "")
	if err != nil || okDelRoot {
		t.Errorf("Expected cannot delete root: ok=%v, err=%v", okDelRoot, err)
	}

	// 12. QueryFileNodes edge cases
	_, _, err = fileNodeBackend.QueryFileNodes(emptyCtx, nil, 0, nil)
	if err == nil {
		t.Errorf("Expected error querying with empty ctx")
	}
	// Query by blobId
	blobIDs, bTotal, err := fileNodeBackend.QueryFileNodes(ctx, map[string]any{
		"blobId": newBlob.ID,
	}, 0, nil)
	if err != nil || bTotal != 1 || len(blobIDs) != 1 || blobIDs[0] != childFile.ID {
		t.Errorf("QueryFileNodes by blobId failed: total=%d ids=%v", bTotal, blobIDs)
	}
	// Query with position >= total
	posIDs, _, err := fileNodeBackend.QueryFileNodes(ctx, nil, 9999, nil)
	if err != nil || len(posIDs) != 0 {
		t.Errorf("Expected empty result when position >= total, got %v", posIDs)
	}
	// Query with parentId: "" (root files)
	rootIDs, rTotal, err := fileNodeBackend.QueryFileNodes(ctx, map[string]any{
		"parentId": "",
	}, 0, nil)
	if err != nil || rTotal < 1 || len(rootIDs) < 1 {
		t.Errorf("QueryFileNodes parentId='' failed: total=%d ids=%v", rTotal, rootIDs)
	}

	// 13. GetFileNodes unauthenticated
	_, _, err = fileNodeBackend.GetFileNodes(emptyCtx, []jmapcore.Id{childFile.ID})
	if err == nil {
		t.Errorf("Expected error GetFileNodes with empty ctx")
	}
}

