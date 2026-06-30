package refresher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

type fakeFetcher struct {
	token  string
	expiry time.Time
	err    error
}

func (f fakeFetcher) FetchToken(context.Context) (string, time.Time, error) {
	return f.token, f.expiry, f.err
}

func TestToken_ReturnsFreshlyMintedToken(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	r := NewWithFetcher(keystore.NewMemoryStore(), fakeFetcher{token: "ghs_fresh", expiry: exp}, config.TokenSource{Type: "github_app"}, "mcp-token/test/GITHUB_TOKEN")
	tok, gotExp, err := r.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "ghs_fresh" {
		t.Errorf("token = %q, want ghs_fresh", tok)
	}
	if !gotExp.Equal(exp) {
		t.Errorf("expiry = %v, want %v", gotExp, exp)
	}
}

func TestToken_PropagatesFetcherError(t *testing.T) {
	want := errors.New("boom")
	r := NewWithFetcher(keystore.NewMemoryStore(), fakeFetcher{err: want}, config.TokenSource{Type: "github_app"}, "k")
	if _, _, err := r.Token(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

// TestToken_DoesNotTouchKeystore documents that the on-demand path does not
// write the cached token key that RunOnce maintains.
func TestToken_DoesNotTouchKeystore(t *testing.T) {
	store := keystore.NewMemoryStore()
	r := NewWithFetcher(store, fakeFetcher{token: "ghs_x", expiry: time.Now().Add(time.Hour)}, config.TokenSource{Type: "github_app"}, "mcp-token/test/GITHUB_TOKEN")
	if _, _, err := r.Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if _, err := store.Get("mcp-token/test/GITHUB_TOKEN"); err == nil {
		t.Error("Token should not write the cached token key")
	}
}
