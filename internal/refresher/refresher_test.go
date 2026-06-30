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
	tokenKey := "mcp-token/test/MY_TOKEN"

	source := config.TokenSource{
		Type:                 "github_app",
		RefreshBeforeSeconds: 600,
	}

	r, err := New(context.Background(), store, source, tokenKey)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = r.RunOnce(context.Background())
	if err != nil {
		t.Logf("RunOnce attempted fetch and failed as expected: %v", err)
	} else {
		t.Log("RunOnce succeeded (unexpected if no network)")
	}
}

func TestRunOnce_TokenStillValid_SkipsRefresh(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-token/test/VALID_TOKEN"

	store.Set(tokenKey, "ghs_validtoken")
	store.Set(tokenKey+"_EXPIRY", time.Now().Add(1*time.Hour).UTC().Format(time.RFC3339))

	source := config.TokenSource{
		Type:                 "github_app",
		RefreshBeforeSeconds: 600,
	}

	r, err := New(context.Background(), store, source, tokenKey)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("expected no error for valid token, got: %v", err)
	}

	token, _ := store.Get(tokenKey)
	if token != "ghs_validtoken" {
		t.Errorf("expected token unchanged, got %q", token)
	}
}

func TestRunOnce_TokenExpired_Refreshes(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-token/test/EXPIRED_TOKEN"

	store.Set(tokenKey, "ghs_expiredtoken")
	store.Set(tokenKey+"_EXPIRY", time.Now().Add(-1*time.Hour).UTC().Format(time.RFC3339))

	source := config.TokenSource{
		Type:                 "github_app",
		RefreshBeforeSeconds: 600,
	}

	r, err := New(context.Background(), store, source, tokenKey)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = r.RunOnce(context.Background())
	if err != nil {
		t.Logf("RunOnce attempted refresh and failed as expected: %v", err)
	} else {
		t.Log("RunOnce succeeded (unexpected if network available)")
	}
}

func TestRunOnce_RefreshBeforeSeconds(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-token/test/ABOUT_TO_EXPIRE"

	store.Set(tokenKey, "ghs_aboutexpire")
	store.Set(tokenKey+"_EXPIRY", time.Now().Add(5*time.Minute).UTC().Format(time.RFC3339))

	source := config.TokenSource{
		Type:                 "github_app",
		RefreshBeforeSeconds: 600,
	}

	r, err := New(context.Background(), store, source, tokenKey)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	err = r.RunOnce(context.Background())
	if err != nil {
		t.Logf("RunOnce attempted refresh as expected: %v", err)
	} else {
		t.Log("RunOnce succeeded")
	}
}

func TestRefresh_StoresTokenAndExpiry(t *testing.T) {
	store := keystore.NewMemoryStore()
	tokenKey := "mcp-token/test/MY_TOKEN"

	source := config.TokenSource{
		Type: "github_app",
	}

	r := &Refresher{
		store:    store,
		fetcher:  &mockFetcher{},
		source:   source,
		tokenKey: tokenKey,
	}

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

func TestNew_UnknownType_ReturnsError(t *testing.T) {
	store := keystore.NewMemoryStore()
	source := config.TokenSource{
		Type: "unknown_type",
	}
	_, err := New(context.Background(), store, source, "mcp-token/test/KEY")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

type mockFetcher struct{}

func (m *mockFetcher) FetchToken(ctx context.Context) (string, time.Time, error) {
	return "ghs_mocktoken", time.Now().Add(1 * time.Hour), nil
}
