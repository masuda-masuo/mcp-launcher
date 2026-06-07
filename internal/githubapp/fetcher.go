package githubapp

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

// TokenFetcher is the interface for fetching installation access tokens.
type TokenFetcher interface {
	FetchToken(ctx context.Context) (token string, expiry time.Time, err error)
}

type Fetcher struct {
	store      keystore.Store
	source     config.TokenSource
	httpClient *http.Client
}

func NewFetcher(store keystore.Store, source config.TokenSource) *Fetcher {
	return &Fetcher{
		store:      store,
		source:     source,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func (f *Fetcher) FetchToken(ctx context.Context) (token string, expiry time.Time, err error) {
	appIDStr, err := f.store.Get(f.source.AppIDKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("getting app id: %w", err)
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("invalid app id %q: %w", appIDStr, err)
	}

	privateKeyPEM, err := f.store.Get(f.source.PrivateKeyKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("getting private key: %w", err)
	}

	installationID, err := f.store.Get(f.source.InstallationIDKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("getting installation id: %w", err)
	}

	jwtToken, err := f.generateJWT(appID, privateKeyPEM)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("generating jwt: %w", err)
	}

	token, expiresAtStr, err := f.callInstallationAPI(ctx, jwtToken, installationID)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("calling github api: %w", err)
	}

	expiry, err = time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("parsing expires_at %q: %w", expiresAtStr, err)
	}

	return token, expiry, nil
}

func (f *Fetcher) generateJWT(appID int64, privateKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS1 as fallback
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("parsing private key: %w", err)
		}
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		// Backdate iat by 60 seconds to tolerate clock skew between
		// the local machine and GitHub's servers.
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(rsaKey)
}

func (f *Fetcher) callInstallationAPI(ctx context.Context, jwtToken, installationID string) (token, expiresAt string, err error) {
	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", installationID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("github api returned status %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", "", fmt.Errorf("parsing response: %w", err)
	}

	if tr.Token == "" {
		return "", "", fmt.Errorf("token is empty in response")
	}

	return tr.Token, tr.ExpiresAt, nil
}
