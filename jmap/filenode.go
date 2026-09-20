package jmap

import (
	"imap-jmap/jmap/jmapfilenode"
)

// FileNode represents a FileNode object in the JMAP FileNode extension.
type FileNode = jmapfilenode.FileNode

// RegisterFileNodeHandlers registers FileNode/* method handlers into MethodRegistry.
var RegisterFileNodeHandlers = jmapfilenode.RegisterFileNodeHandlers
