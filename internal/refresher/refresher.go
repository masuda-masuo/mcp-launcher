package refresher

import (
	"context"
	"fmt"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/githubapp"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

type Refresher struct {
	store    keystore.Store
	fetcher  githubapp.TokenFetcher
	source   config.TokenSource
	tokenKey string
}

func New(store keystore.Store, source config.TokenSource, tokenKey string) *Refresher {
	return &Refresher{
		store:    store,
		fetcher:  githubapp.NewFetcher(store, source),
		source:   source,
		tokenKey: tokenKey,
	}
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
