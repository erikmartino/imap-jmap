package jmapfilenode

import (
	"imap-jmap/jmap/jmapcore"
)

// FileNodeCapabilityURI is the JMAP capability URI for FileNode file storage extension.
const FileNodeCapabilityURI = "urn:ietf:params:jmap:filenode"

// FileNodeCapability defines the capability object for "urn:ietf:params:jmap:filenode".
type FileNodeCapability struct {
	MaxFileSize uint64 `json:"maxFileSize,omitempty"`
}

// FileNode represents a FileNode object in the JMAP FileNode extension.
type FileNode struct {
	ID        jmapcore.Id  `json:"id"`
	Name      string       `json:"name"`
	ParentID  *jmapcore.Id `json:"parentId,omitempty"`
	BlobID    *jmapcore.Id `json:"blobId,omitempty"`
	Size      uint64       `json:"size"`
	Type      string       `json:"type,omitempty"`
	IsFolder  bool         `json:"isFolder"`
	CreatedAt string       `json:"createdAt,omitempty"`
	UpdatedAt string       `json:"updatedAt,omitempty"`
}
