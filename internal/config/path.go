package config

import (
	"os"
	"path/filepath"
)

// DefaultConfigName is the launcher config filename looked up by DefaultPath.
const DefaultConfigName = "launcher.json"

// DefaultPath resolves the launcher.json path with the following priority:
//  1. MCP_LAUNCHER_CONFIG environment variable (explicit override)
//  2. the directory of the running executable
//  3. the current working directory (fallback)
//
// It is shared by every binary in this module (the launcher and the mcp-token
// broker) so they agree on where configuration lives (issue #25).
func DefaultPath() string {
	if p := os.Getenv("MCP_LAUNCHER_CONFIG"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), DefaultConfigName)
	}
	return DefaultConfigName
}
