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

func TestRunList_LegacyPrefix(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-launcher/github/PRIVATE_KEY", "abc")

	var buf bytes.Buffer
	err := runList(nil, store, &buf)
	if err != nil {
		t.Fatalf("runList failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (new + legacy), got %d: %v", len(lines), lines)
	}
	if lines[0] != "mcp-launcher/github/PRIVATE_KEY" {
		t.Errorf("expected legacy key first (sorted), got %q", lines[0])
	}
	if lines[1] != "mcp-token/github/APP_ID" {
		t.Errorf("expected new key second (sorted), got %q", lines[1])
	}
}

func TestRunList_LegacyPrefixFilterByService(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-launcher/csb/TOKEN", "xyz")

	var buf bytes.Buffer
	err := runList([]string{"github"}, store, &buf)
	if err != nil {
		t.Fatalf("runList failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %v", len(lines), lines)
	}
	if lines[0] != "mcp-token/github/APP_ID" {
		t.Errorf("expected github/APP_ID, got %q", lines[0])
	}
}

func TestRunList_Dedup(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-token/github/APP_ID", "123") // same key, redundant set

	var buf bytes.Buffer
	err := runList(nil, store, &buf)
	if err != nil {
		t.Fatalf("runList failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line (deduped), got %d: %v", len(lines), lines)
	}
}

func TestRunDelete_SingleKey(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")

	var buf bytes.Buffer
	err := runDelete([]string{"github", "APP_ID"}, store, nil, &buf)
	if err != nil {
		t.Fatalf("runDelete failed: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "✓ Deleted") {
		t.Errorf("expected success message, got %q", out)
	}
	if !strings.Contains(out, "mcp-token/github/APP_ID") {
		t.Errorf("expected key name in output, got %q", out)
	}
	// Verify it is actually deleted from the store
	if _, err := store.Get("mcp-token/github/APP_ID"); !keystore.IsNotFound(err) {
		t.Errorf("expected key to be deleted from store, got %v", err)
	}
}

func TestRunDelete_SingleKey_LegacyPrefix(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")

	var buf bytes.Buffer
	err := runDelete([]string{"github", "APP_ID"}, store, nil, &buf)
	if err != nil {
		t.Fatalf("runDelete failed: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "✓ Deleted") {
		t.Errorf("expected success message, got %q", out)
	}
	if !strings.Contains(out, "mcp-launcher/github/APP_ID") {
		t.Errorf("expected legacy key name in output, got %q", out)
	}
}

func TestRunDelete_NotFound(t *testing.T) {
	store := keystore.NewMemoryStore()
	var buf bytes.Buffer
	err := runDelete([]string{"github", "NONEXISTENT"}, store, nil, &buf)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestRunDelete_UsageError(t *testing.T) {
	store := keystore.NewMemoryStore()
	var buf bytes.Buffer

	// No args
	err := runDelete(nil, store, nil, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}

	// Single arg (not --all)
	err = runDelete([]string{"github"}, store, nil, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunDelete_All(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-token/github/PRIVATE_KEY", "abc")
	store.Set("mcp-token/csb/TOKEN", "xyz")

	var buf bytes.Buffer
	in := strings.NewReader("y\n")
	err := runDelete([]string{"--all", "github"}, store, in, &buf)
	if err != nil {
		t.Fatalf("runDelete --all failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Deleted 2 key(s)") {
		t.Errorf("expected 2 keys deleted, got %q", out)
	}
	// Verify github keys are deleted
	if _, err := store.Get("mcp-token/github/APP_ID"); !keystore.IsNotFound(err) {
		t.Errorf("expected APP_ID to be deleted")
	}
	if _, err := store.Get("mcp-token/github/PRIVATE_KEY"); !keystore.IsNotFound(err) {
		t.Errorf("expected PRIVATE_KEY to be deleted")
	}
	// csb key should remain
	if v, err := store.Get("mcp-token/csb/TOKEN"); err != nil {
		t.Errorf("expected csb/TOKEN to remain, got %v", err)
	} else if v != "xyz" {
		t.Errorf("expected csb/TOKEN value to be 'xyz', got %q", v)
	}
}

func TestRunDelete_All_Cancelled(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")

	var buf bytes.Buffer
	in := strings.NewReader("n\n")
	err := runDelete([]string{"--all", "github"}, store, in, &buf)
	if err != nil {
		t.Fatalf("runDelete --all failed: %v", err)
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("expected cancelled message, got %q", buf.String())
	}
	// Key should still exist
	if _, err := store.Get("mcp-token/github/APP_ID"); err != nil {
		t.Errorf("expected key to remain after cancellation, got %v", err)
	}
}

func TestRunDelete_All_Empty(t *testing.T) {
	store := keystore.NewMemoryStore()
	var buf bytes.Buffer
	err := runDelete([]string{"--all", "nonexistent"}, store, nil, &buf)
	if err != nil {
		t.Fatalf("runDelete --all failed: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "no keys registered") {
		t.Errorf("expected no keys message, got %q", out)
	}
}

func TestRunDelete_All_Force(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-token/github/APP_ID", "123")
	store.Set("mcp-token/github/PRIVATE_KEY", "abc")

	var buf bytes.Buffer
	// No stdin reader needed because --force skips confirmation
	err := runDelete([]string{"--all", "github", "--force"}, store, nil, &buf)
	if err != nil {
		t.Fatalf("runDelete --all --force failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted 2 key(s)") {
		t.Errorf("expected 2 keys deleted, got %q", buf.String())
	}
}

func TestRunConvert_Empty(t *testing.T) {
	var buf bytes.Buffer
	err := runConvert(nil, keystore.NewMemoryStore(), nil, &buf)
	if err != nil {
		t.Fatalf("runConvert failed: %v", err)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "no legacy keys") {
		t.Errorf("expected no legacy keys message, got %q", out)
	}
}

func TestRunConvert_All(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")
	store.Set("mcp-launcher/github/PRIVATE_KEY", "abc")
	store.Set("mcp-token/csb/TOKEN", "xyz") // already new prefix, should be untouched

	var buf bytes.Buffer
	in := strings.NewReader("y\n")
	err := runConvert(nil, store, in, &buf)
	if err != nil {
		t.Fatalf("runConvert failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Converted 2 key(s)") {
		t.Errorf("expected 2 keys converted, got %q", out)
	}
	// Old keys should be deleted
	if _, err := store.Get("mcp-launcher/github/APP_ID"); !keystore.IsNotFound(err) {
		t.Errorf("expected old key to be deleted")
	}
	// New keys should exist
	if v, err := store.Get("mcp-token/github/APP_ID"); err != nil {
		t.Errorf("expected new key, got %v", err)
	} else if v != "123" {
		t.Errorf("expected value 123, got %q", v)
	}
	if v, err := store.Get("mcp-token/github/PRIVATE_KEY"); err != nil {
		t.Errorf("expected new key, got %v", err)
	} else if v != "abc" {
		t.Errorf("expected value abc, got %q", v)
	}
	// csb key should still exist
	if _, err := store.Get("mcp-token/csb/TOKEN"); err != nil {
		t.Errorf("expected csb/TOKEN to remain, got %v", err)
	}
}

func TestRunConvert_SingleService(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")
	store.Set("mcp-launcher/github/PRIVATE_KEY", "abc")
	store.Set("mcp-launcher/csb/TOKEN", "xyz")

	var buf bytes.Buffer
	in := strings.NewReader("y\n")
	err := runConvert([]string{"github"}, store, in, &buf)
	if err != nil {
		t.Fatalf("runConvert failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Converted 2 key(s)") {
		t.Errorf("expected 2 keys converted, got %q", buf.String())
	}
	// github keys should be migrated
	if v, _ := store.Get("mcp-token/github/APP_ID"); v != "123" {
		t.Errorf("expected github/APP_ID to be migrated")
	}
	// csb keys should remain as legacy
	if _, err := store.Get("mcp-launcher/csb/TOKEN"); err != nil {
		t.Errorf("expected csb/TOKEN to remain as legacy, got %v", err)
	}
}

func TestRunConvert_Cancelled(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")

	var buf bytes.Buffer
	in := strings.NewReader("n\n")
	err := runConvert(nil, store, in, &buf)
	if err != nil {
		t.Fatalf("runConvert failed: %v", err)
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("expected cancelled message, got %q", buf.String())
	}
	// Old key should still exist
	if _, err := store.Get("mcp-launcher/github/APP_ID"); err != nil {
		t.Errorf("expected legacy key to remain after cancellation, got %v", err)
	}
}

func TestRunConvert_Force(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")
	store.Set("mcp-launcher/github/PRIVATE_KEY", "abc")

	var buf bytes.Buffer
	// No stdin reader needed — --force skips confirmation
	err := runConvert([]string{"--force"}, store, nil, &buf)
	if err != nil {
		t.Fatalf("runConvert --force failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Converted 2 key(s)") {
		t.Errorf("expected 2 keys converted, got %q", buf.String())
	}
	// Verify deletion of old keys
	if _, err := store.Get("mcp-launcher/github/APP_ID"); !keystore.IsNotFound(err) {
		t.Errorf("expected old key to be deleted")
	}
}

func TestRunConvert_UsageError(t *testing.T) {
	store := keystore.NewMemoryStore()
	var buf bytes.Buffer
	err := runConvert([]string{"github", "extra"}, store, nil, &buf)
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunConvert_SameValueAlreadyAtNewKey(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")
	store.Set("mcp-token/github/APP_ID", "123") // same value already exists

	var buf bytes.Buffer
	in := strings.NewReader("y\n")
	err := runConvert(nil, store, in, &buf)
	if err != nil {
		t.Fatalf("runConvert failed: %v", err)
	}
	out := buf.String()
	// It should silently delete the old key (same value already present)
	if _, err := store.Get("mcp-launcher/github/APP_ID"); !keystore.IsNotFound(err) {
		t.Errorf("expected old key to be deleted")
	}
	if v, _ := store.Get("mcp-token/github/APP_ID"); v != "123" {
		t.Errorf("expected new key to remain '123', got %q", v)
	}
	// Old key was deleted (cleanup), but no real conversion happened
	if !strings.Contains(out, "Converted 0 key(s)") {
		t.Errorf("expected 0 keys converted (value already present), got %q", out)
	}
}

func TestRunConvert_ConflictingValueAtNewKey(t *testing.T) {
	store := keystore.NewMemoryStore()
	store.Set("mcp-launcher/github/APP_ID", "123")
	store.Set("mcp-token/github/APP_ID", "999") // different value already exists

	var buf bytes.Buffer
	in := strings.NewReader("y\n")
	err := runConvert(nil, store, in, &buf)
	if err != nil {
		t.Fatalf("runConvert failed: %v", err)
	}
	out := buf.String()
	// Should skip — old key remains, new key unchanged
	if _, err := store.Get("mcp-launcher/github/APP_ID"); err != nil {
		t.Errorf("expected old key to remain after skip")
	}
	if v, _ := store.Get("mcp-token/github/APP_ID"); v != "999" {
		t.Errorf("expected new key to keep '999', got %q", v)
	}
	if !strings.Contains(out, "skipping") {
		t.Errorf("expected skip message due to conflict, got %q", out)
	}
}
