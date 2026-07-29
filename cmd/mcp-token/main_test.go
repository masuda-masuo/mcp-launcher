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

func TestRunRegister_PositionalValue(t *testing.T) {
	store := keystore.NewMemoryStore()
	err := runRegister([]string{"github", "PRIVATE_KEY", "secret-value"}, store, nil)
	if err != nil {
		t.Fatalf("runRegister failed: %v", err)
	}
	v, err := store.Get("mcp-token/github/PRIVATE_KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if v != "secret-value" {
		t.Errorf("expected 'secret-value', got %q", v)
	}
}

func TestRunRegister_StdinStoresValue(t *testing.T) {
	store := keystore.NewMemoryStore()
	in := strings.NewReader("secret-from-stdin")
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdin"}, store, in)
	if err != nil {
		t.Fatalf("runRegister --stdin failed: %v", err)
	}
	v, err := store.Get("mcp-token/github/PRIVATE_KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if v != "secret-from-stdin" {
		t.Errorf("expected 'secret-from-stdin', got %q", v)
	}
}

func TestRunRegister_StdinStripsTrailingNewlines(t *testing.T) {
	store := keystore.NewMemoryStore()
	in := strings.NewReader("multi-line\nvalue\n\n\n")
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdin"}, store, in)
	if err != nil {
		t.Fatalf("runRegister --stdin failed: %v", err)
	}
	v, err := store.Get("mcp-token/github/PRIVATE_KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if v != "multi-line\nvalue" {
		t.Errorf("expected 'multi-line\\nvalue', got %q", v)
	}
}

func TestRunRegister_StdinPreservesInteriorNewlines(t *testing.T) {
	store := keystore.NewMemoryStore()
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"
	in := strings.NewReader(pem)
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdin"}, store, in)
	if err != nil {
		t.Fatalf("runRegister --stdin failed: %v", err)
	}
	v, err := store.Get("mcp-token/github/PRIVATE_KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if v != pem {
		t.Errorf("expected interior newlines preserved\n got: %q\nwant: %q", v, pem)
	}
}

func TestRunRegister_StdinWithTrailingNewlineMatchesCatSubstitution(t *testing.T) {
	// "$(cat pem)" in shell strips ALL trailing newlines.
	// The stored value must be byte-identical to what shell command substitution produces.
	store := keystore.NewMemoryStore()
	pemBody := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----\n"
	in := strings.NewReader(pemBody)
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdin"}, store, in)
	if err != nil {
		t.Fatalf("runRegister --stdin failed: %v", err)
	}
	v, err := store.Get("mcp-token/github/PRIVATE_KEY")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	// "$(cat file)" strips the trailing newline
	expected := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"
	if v != expected {
		t.Errorf("trailing-newline stripping does not match shell behavior\n got:  %q\nwant: %q", v, expected)
	}
}

func TestRunRegister_BothStdinAndValueError(t *testing.T) {
	store := keystore.NewMemoryStore()
	in := strings.NewReader("stdin-value")
	err := runRegister([]string{"github", "PRIVATE_KEY", "positional-value", "--stdin"}, store, in)
	if err == nil {
		t.Fatal("expected error when both positional value and --stdin provided")
	}
	if !strings.Contains(err.Error(), "cannot provide both") {
		t.Errorf("expected 'cannot provide both' error, got %q", err.Error())
	}
}

func TestRunRegister_EmptyStdinError(t *testing.T) {
	store := keystore.NewMemoryStore()
	in := strings.NewReader("")
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdin"}, store, in)
	if err == nil {
		t.Fatal("expected error for empty stdin")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in error, got %q", err.Error())
	}
}

func TestRunRegister_StdinOnlyNewlinesError(t *testing.T) {
	store := keystore.NewMemoryStore()
	in := strings.NewReader("\n\n\n")
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdin"}, store, in)
	if err == nil {
		t.Fatal("expected error for stdin consisting only of newlines")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' in error, got %q", err.Error())
	}
}

// --- unrecognised flags must not become values (issue #49) -------------------
//
// Every positional argument of this tool is a secret or a key name, so the old
// permissive parsing turned a mistyped flag into the stored secret and still
// printed a success line. These tests pin the three shapes that actually bit
// us, plus the two forms that must keep working.

func TestRunRegister_UnknownFlagIsRejected(t *testing.T) {
	store := keystore.NewMemoryStore()
	err := runRegister([]string{"github", "PRIVATE_KEY", "--file"}, store, strings.NewReader(""))
	if err == nil {
		t.Fatal("expected an error for an unrecognised flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("error should name the problem, got %q", err)
	}
	if _, err := store.Get("mcp-token/github/PRIVATE_KEY"); err == nil {
		t.Fatal("nothing must be stored when the arguments are rejected")
	}
}

func TestRunRegister_MistypedStdinDoesNotDiscardThePipedSecret(t *testing.T) {
	store := keystore.NewMemoryStore()
	err := runRegister([]string{"github", "PRIVATE_KEY", "--stdinn"}, store, strings.NewReader("the-real-secret"))
	if err == nil {
		t.Fatal("a one-character typo in --stdin must be an error, not a stored value")
	}
	if _, err := store.Get("mcp-token/github/PRIVATE_KEY"); err == nil {
		t.Fatal("the flag text must not be stored as the secret")
	}
}

// A PEM body starts with "-----BEGIN ...". The historical
// `register <service> <ENV_KEY> "$(cat key.pem)"` form has to keep working, so
// the flag check must not treat it as a flag.
func TestRunRegister_PemValueIsNotMistakenForAFlag(t *testing.T) {
	store := keystore.NewMemoryStore()
	pem := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----"
	if err := runRegister([]string{"github", "PRIVATE_KEY", pem}, store, strings.NewReader("")); err != nil {
		t.Fatalf("a PEM value must still be accepted positionally: %v", err)
	}
	got, err := store.Get("mcp-token/github/PRIVATE_KEY")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != pem {
		t.Fatalf("stored %q, want the pem unchanged", got)
	}
}

// The escape hatch: a value that really does start with "--".
func TestRunRegister_DoubleDashPassesALiteralFlagLikeValue(t *testing.T) {
	store := keystore.NewMemoryStore()
	if err := runRegister([]string{"github", "TOKEN", "--", "--literal"}, store, strings.NewReader("")); err != nil {
		t.Fatalf("-- should allow a flag-shaped value: %v", err)
	}
	got, _ := store.Get("mcp-token/github/TOKEN")
	if got != "--literal" {
		t.Fatalf("stored %q, want %q", got, "--literal")
	}
}

func TestRunList_UnknownFlagIsRejectedRatherThanReportingNoKeys(t *testing.T) {
	store := keystore.NewMemoryStore()
	if err := store.Set("mcp-token/github/PRIVATE_KEY", "x"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var out bytes.Buffer
	err := runList([]string{"--json"}, store, &out)
	if err == nil {
		t.Fatal("expected an error; reporting \"(no keys registered)\" while a key exists is the bug")
	}
	if strings.Contains(out.String(), "no keys registered") {
		t.Fatalf("must not print the misleading empty report, got %q", out.String())
	}
}

func TestRunConvert_UnknownFlagIsRejectedRatherThanReportingNothingToDo(t *testing.T) {
	store := keystore.NewMemoryStore()
	if err := store.Set("mcp-launcher/github/PRIVATE_KEY", "x"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var out bytes.Buffer
	err := runConvert([]string{"--dry-run"}, store, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("expected an error; \"(no legacy keys to convert)\" while a legacy key exists is the bug")
	}
	if _, err := store.Get("mcp-launcher/github/PRIVATE_KEY"); err != nil {
		t.Fatalf("the legacy key must be untouched: %v", err)
	}
}

func TestRunDelete_UnknownFlagIsRejected(t *testing.T) {
	store := keystore.NewMemoryStore()
	if err := store.Set("mcp-token/github/PRIVATE_KEY", "x"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var out bytes.Buffer
	if err := runDelete([]string{"--quiet", "github", "PRIVATE_KEY"}, store, strings.NewReader(""), &out); err == nil {
		t.Fatal("expected an error for an unrecognised flag")
	}
	if _, err := store.Get("mcp-token/github/PRIVATE_KEY"); err != nil {
		t.Fatalf("the key must be untouched: %v", err)
	}
}

// --all is still recognised in any position, and the service name is now the
// only positional argument left after parsing.
func TestRunDelete_AllStillWorksInAnyPosition(t *testing.T) {
	for _, args := range [][]string{
		{"--all", "github", "--force"},
		{"--force", "--all", "github"},
		{"github", "--all", "--force"},
	} {
		store := keystore.NewMemoryStore()
		if err := store.Set("mcp-token/github/PRIVATE_KEY", "x"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		var out bytes.Buffer
		if err := runDelete(args, store, strings.NewReader(""), &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if _, err := store.Get("mcp-token/github/PRIVATE_KEY"); err == nil {
			t.Fatalf("%v: key should have been deleted", args)
		}
	}
}

func TestParseFlags_DoubleDashStopsFlagProcessing(t *testing.T) {
	flags, positional, err := parseFlags([]string{"--force", "--", "--all", "svc"}, "--force", "--all")
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !flags["--force"] {
		t.Fatal("--force before -- should be recognised")
	}
	if flags["--all"] {
		t.Fatal("--all after -- must be positional, not a flag")
	}
	want := []string{"--all", "svc"}
	if len(positional) != len(want) {
		t.Fatalf("positional = %q, want %q", positional, want)
	}
	for i := range want {
		if positional[i] != want[i] {
			t.Fatalf("positional = %q, want %q", positional, want)
		}
	}
}

// A lone "-" and a single-dash argument are not long flags; they stay
// positional so that values like "-" (conventionally stdin) are untouched.
func TestParseFlags_SingleDashIsPositional(t *testing.T) {
	_, positional, err := parseFlags([]string{"-", "-x"}, "--force")
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if len(positional) != 2 {
		t.Fatalf("positional = %q, want both arguments kept", positional)
	}
}

// convert's --force must keep working in any position, the same way delete's
// --all/--force do -- the rejection tests above only cover the error path.
func TestRunConvert_ForceStillWorksInAnyPosition(t *testing.T) {
	for _, args := range [][]string{
		{"--force"},
		{"--force", "github"},
		{"github", "--force"},
	} {
		store := keystore.NewMemoryStore()
		if err := store.Set("mcp-launcher/github/PRIVATE_KEY", "x"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		var out bytes.Buffer
		// An empty reader is the point: with --force honoured there is no
		// confirmation prompt to read, so this only succeeds if the flag was
		// recognised.
		if err := runConvert(args, store, strings.NewReader(""), &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if _, err := store.Get("mcp-token/github/PRIVATE_KEY"); err != nil {
			t.Fatalf("%v: key should have been converted: %v (output %q)", args, err, out.String())
		}
		if _, err := store.Get("mcp-launcher/github/PRIVATE_KEY"); err == nil {
			t.Fatalf("%v: legacy key should have been removed", args)
		}
	}
}
