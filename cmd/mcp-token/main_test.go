package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

// writeConfig writes a launcher.json into a temp dir and points DefaultPath at
// it via MCP_LAUNCHER_CONFIG (restored automatically by t.Setenv).
func writeConfig(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "launcher.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("MCP_LAUNCHER_CONFIG", p)
}

func TestRun_ServiceNotFound(t *testing.T) {
	writeConfig(t, `{"other":{"command":"x"}}`)
	err := run("csb", keystore.NewMemoryStore(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestRun_NoTokenSource(t *testing.T) {
	writeConfig(t, `{"csb":{"command":"x"}}`)
	err := run("csb", keystore.NewMemoryStore(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no token_source") {
		t.Fatalf("expected no token_source error, got %v", err)
	}
}

func TestRun_UnsupportedType(t *testing.T) {
	writeConfig(t, `{"csb":{"command":"x","env_keys":{"AWS":"k"},"token_source":{"type":"aws_sts","target_env_key":"AWS"}}}`)
	err := run("csb", keystore.NewMemoryStore(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestRun_MissingTargetEnvKey(t *testing.T) {
	writeConfig(t, `{"csb":{"command":"x","env_keys":{"OTHER":"k"},"token_source":{"type":"github_app","target_env_key":"GITHUB_TOKEN"}}}`)
	err := run("csb", keystore.NewMemoryStore(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "target_env_key") {
		t.Fatalf("expected target_env_key error, got %v", err)
	}
}

// TestRun_MintFailsWithoutCredentials exercises the happy path up to the actual
// mint: with an empty keystore the github_app fetcher fails reading APP_ID
// before any network call, so this stays deterministic offline.
func TestRun_MintFailsWithoutCredentials(t *testing.T) {
	writeConfig(t, `{"csb":{"command":"x","env_keys":{"GITHUB_TOKEN":"mcp-token/csb/GITHUB_TOKEN"},"token_source":{"type":"github_app","target_env_key":"GITHUB_TOKEN","app_id_key":"mcp-token/csb/APP_ID","private_key_key":"mcp-token/csb/PRIVATE_KEY","installation_id_key":"mcp-token/csb/INSTALLATION_ID"}}}`)
	err := run("csb", keystore.NewMemoryStore(), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "minting token") {
		t.Fatalf("expected minting token error, got %v", err)
	}
}

func TestRunList_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := runList(nil, keystore.NewMemoryStore(), &buf)
	if err != nil {
		t.Fatalf("runList failed: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if out != "(no keys registered)" {
		t.Errorf("expected empty message, got %q", out)
	}
}

func TestRunList_All(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-token/github/PRIVATE_KEY", "abc")
	store.Set("mcp-token/csb/TOKEN", "xyz")

	var buf bytes.Buffer
	err := runList(nil, store, &buf)
	if err != nil {
		t.Fatalf("runList failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "mcp-token/csb/TOKEN" {
		t.Errorf("expected csb/TOKEN first (sorted), got %q", lines[0])
	}
	if lines[2] != "mcp-token/github/PRIVATE_KEY" {
		t.Errorf("expected github/PRIVATE_KEY last (sorted), got %q", lines[2])
	}
}

func TestRunList_FilterByService(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-token/github/PRIVATE_KEY", "abc")
	store.Set("mcp-token/csb/TOKEN", "xyz")

	var buf bytes.Buffer
	err := runList([]string{"github"}, store, &buf)
	if err != nil {
		t.Fatalf("runList failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != "mcp-token/github/APP_ID" {
		t.Errorf("expected github/APP_ID first, got %q", lines[0])
	}
}
