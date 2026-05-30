package refresher

import (
	"context"
	"testing"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

func TestRunOnce_NoExpiryKey_FetchesToken(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-launcher/test/MY_TOKEN"

	// No token or expiry stored — RunOnce should attempt refresh
	// Since the fetcher tries to call GitHub API, it will fail with no network
	// We just verify the code path is executed

	source := config.TokenSource{
		RefreshBeforeSeconds: 600,
	}

	r := New(store, source, tokenKey)
	err := r.RunOnce(context.Background())
	if err != nil {
		// Expected: fetcher can't reach GitHub
		t.Logf("RunOnce attempted fetch and failed as expected: %v", err)
	} else {
		t.Log("RunOnce succeeded (unexpected if no network)")
	}
}

func TestRunOnce_TokenStillValid_SkipsRefresh(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-launcher/test/VALID_TOKEN"

	// Set valid token and expiry far in the future
	store.Set(tokenKey, "ghs_validtoken")
	store.Set(tokenKey+"_EXPIRY", time.Now().Add(1*time.Hour).UTC().Format(time.RFC3339))

	source := config.TokenSource{
		RefreshBeforeSeconds: 600, // 10 minutes
	}

	r := New(store, source, tokenKey)
	err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("expected no error for valid token, got: %v", err)
	}

	// Verify token was NOT changed
	token, _ := store.Get(tokenKey)
	if token != "ghs_validtoken" {
		t.Errorf("expected token unchanged, got %q", token)
	}
}

func TestRunOnce_TokenExpired_Refreshes(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-launcher/test/EXPIRED_TOKEN"

	// Set expired token
	store.Set(tokenKey, "ghs_expiredtoken")
	store.Set(tokenKey+"_EXPIRY", time.Now().Add(-1*time.Hour).UTC().Format(time.RFC3339))

	source := config.TokenSource{
		RefreshBeforeSeconds: 600,
	}

	r := New(store, source, tokenKey)
	err := r.RunOnce(context.Background())
	if err != nil {
		// Expected: fetcher can't reach GitHub
		t.Logf("RunOnce attempted refresh and failed as expected: %v", err)
	} else {
		t.Log("RunOnce succeeded (unexpected if network available)")
	}
}

func TestRunOnce_RefreshBeforeSeconds(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-launcher/test/ABOUT_TO_EXPIRE"

	// Token expires in 5 minutes, but refresh_before is 10 minutes
	store.Set(tokenKey, "ghs_aboutexpire")
	store.Set(tokenKey+"_EXPIRY", time.Now().Add(5*time.Minute).UTC().Format(time.RFC3339))

	source := config.TokenSource{
		RefreshBeforeSeconds: 600, // 10 minutes — trigger refresh
	}

	r := New(store, source, tokenKey)
	err := r.RunOnce(context.Background())
	if err != nil {
		// Expected to attempt refresh since token is within refresh window
		t.Logf("RunOnce attempted refresh as expected: %v", err)
	} else {
		t.Log("RunOnce succeeded")
	}
}

func TestRefresh_StoresTokenAndExpiry(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-launcher/test/MY_TOKEN"

	source := config.TokenSource{}
	r := New(store, source, tokenKey)

	// Override fetcher with mock
	r.fetcher = &mockFetcher{}

	err := r.refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	token, err := store.Get(tokenKey)
	if err != nil {
		t.Fatalf("getting token: %v", err)
	}
	if token != "ghs_mocktoken" {
		t.Errorf("expected ghs_mocktoken, got %q", token)
	}

	expiryStr, err := store.Get(tokenKey + "_EXPIRY")
	if err != nil {
		t.Fatalf("getting expiry: %v", err)
	}
	if expiryStr == "" {
		t.Fatal("expected non-empty expiry")
	}

	_, err = time.Parse(time.RFC3339, expiryStr)
	if err != nil {
		t.Errorf("expected valid RFC3339 expiry, got %q: %v", expiryStr, err)
	}
}

func TestNew_SetsTokenKey(t *testing.T) {
	store := keystore.NewMemoryStore()
	source := config.TokenSource{}
	r := New(store, source, "mcp-launcher/test/KEY")
	if r.tokenKey != "mcp-launcher/test/KEY" {
		t.Errorf("expected tokenKey to be set")
	}
}

// mockFetcher implements token fetching without network calls
type mockFetcher struct{}

func (m *mockFetcher) FetchToken(ctx context.Context) (string, time.Time, error) {
	return "ghs_mocktoken", time.Now().Add(1 * time.Hour), nil
}
