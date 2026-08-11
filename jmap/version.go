package jmap

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
)

// Version is the application version string. Build tooling or ldflags (-ldflags "-X imap-jmap/jmap.Version=...") can set this.
var Version = "dev"

// Commit is the git commit hash string. Build tooling or ldflags can set this.
var Commit = ""

// BuildTime is the build timestamp string. Build tooling or ldflags can set this.
var BuildTime = ""

// VersionInfo returns structured version information, attempting to infer commit from vcs info if unset.
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildTime string `json:"buildTime,omitempty"`
}

// GetVersionInfo returns the populated VersionInfo struct.
func GetVersionInfo() VersionInfo {
	v := VersionInfo{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if v.Commit == "" && setting.Key == "vcs.revision" {
				v.Commit = setting.Value
			}
			if v.BuildTime == "" && setting.Key == "vcs.time" {
				v.BuildTime = setting.Value
			}
		}
		if v.Version == "" || v.Version == "dev" {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				v.Version = info.Main.Version
			}
		}
	}

	return v
}

func infoAvailable() bool {
	_, ok := debug.ReadBuildInfo()
	return ok
}

// handleVersion serves GET /version or GET /healthz/version without authentication.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	info := GetVersionInfo()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(info)
	}
}
