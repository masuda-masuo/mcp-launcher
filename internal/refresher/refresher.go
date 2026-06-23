package refresher

import (
	"context"
	"fmt"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/awssts"
	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/githubapp"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

// TokenFetcher is the interface for fetching tokens.
type TokenFetcher interface {
	FetchToken(ctx context.Context) (token string, expiry time.Time, err error)
}

type Refresher struct {
	store    keystore.Store
	fetcher  TokenFetcher
	source   config.TokenSource
	tokenKey string
}

func New(ctx context.Context, store keystore.Store, source config.TokenSource, tokenKey string) (*Refresher, error) {
	var fetcher TokenFetcher
	var err error

	switch source.Type {
	case "github_app":
		fetcher = githubapp.NewFetcher(store, source)
	case "aws_sts":
		// Extract the target_env_key to use as prefix for multi-credential storage
		targetEnvKey := source.TargetEnvKey
		fetcher, err = awssts.NewFetcher(ctx, store, source.RoleARNKey, source.RoleSessionName, targetEnvKey, source.DurationSeconds)
		if err != nil {
			return nil, fmt.Errorf("creating aws_sts fetcher: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown token source type: %q", source.Type)
	}

	return &Refresher{
		store:    store,
		fetcher:  fetcher,
		source:   source,
		tokenKey: tokenKey,
	}, nil
}

func (r *Refresher) RunOnce(ctx context.Context) error {
	expiryKey := r.tokenKey + "_EXPIRY"
	expiryStr, err := r.store.Get(expiryKey)
	if err != nil {
		if keystore.IsNotFound(err) {
			// No token yet — fetch one
			return r.refresh(ctx)
		}
		return fmt.Errorf("getting expiry: %w", err)
	}

	expiry, err := time.Parse(time.RFC3339, expiryStr)
	if err != nil {
		return fmt.Errorf("parsing expiry %q: %w", expiryStr, err)
	}

	refreshBefore := time.Duration(r.source.RefreshBeforeSeconds) * time.Second
	if time.Until(expiry) > refreshBefore {
		// Token is still valid
		return nil
	}

	return r.refresh(ctx)
}

func (r *Refresher) refresh(ctx context.Context) error {
	token, expiry, err := r.fetcher.FetchToken(ctx)
	if err != nil {
		return fmt.Errorf("fetching token: %w", err)
	}

	if err := r.store.Set(r.tokenKey, token); err != nil {
		return fmt.Errorf("storing token: %w", err)
	}

	expiryKey := r.tokenKey + "_EXPIRY"
	if err := r.store.Set(expiryKey, expiry.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("storing expiry: %w", err)
	}

	return nil
}

// NewWithFetcher constructs a Refresher around an already-built fetcher,
// bypassing the token_source type switch in New. This is what makes the
// token-minting path unit-testable with a fake fetcher (issue #25).
func NewWithFetcher(store keystore.Store, fetcher TokenFetcher, source config.TokenSource, tokenKey string) *Refresher {
	return &Refresher{
		store:    store,
		fetcher:  fetcher,
		source:   source,
		tokenKey: tokenKey,
	}
}

// Token mints a fresh token via the configured fetcher and returns it,
// bypassing the keystore-cached token/expiry that RunOnce maintains. It is the
// on-demand path used by the mcp-token CLI to emit a short-lived credential to
// stdout (issue #25): the long-lived secret (e.g. a GitHub App private key)
// stays in the keystore and only the freshly minted token leaves the process.
func (r *Refresher) Token(ctx context.Context) (string, time.Time, error) {
	return r.fetcher.FetchToken(ctx)
}
