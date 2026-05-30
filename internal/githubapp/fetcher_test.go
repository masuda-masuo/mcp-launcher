package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

func generateTestKey(t *testing.T) (pemStr string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	derBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: derBytes,
	}
	return string(pem.EncodeToMemory(block))
}

func TestGenerateJWT_ValidToken(t *testing.T) {
	privateKeyPEM := generateTestKey(t)
	store := keystore.NewMemoryStore()
	source := config.TokenSource{}
	fetcher := NewFetcher(store, source)

	jwtToken, err := fetcher.generateJWT(42, privateKeyPEM)
	if err != nil {
		t.Fatalf("generateJWT failed: %v", err)
	}
	if jwtToken == "" {
		t.Fatal("expected non-empty JWT")
	}
}

func TestGenerateJWT_InvalidPEM(t *testing.T) {
	store := keystore.NewMemoryStore()
	source := config.TokenSource{}
	fetcher := NewFetcher(store, source)

	_, err := fetcher.generateJWT(1, "not-a-valid-pem")
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}

func TestFetchToken_StoreErrors(t *testing.T) {
	store := keystore.NewMemoryStore()
	source := config.TokenSource{
		AppIDKey:          "mcp-launcher/test/APP_ID",
		PrivateKeyKey:     "mcp-launcher/test/PRIVATE_KEY",
		InstallationIDKey: "mcp-launcher/test/INSTALLATION_ID",
	}
	fetcher := NewFetcher(store, source)

	// AppID missing
	_, _, err := fetcher.FetchToken(context.Background())
	if err == nil {
		t.Fatal("expected error for missing app id, got nil")
	}
}

func TestCallInstallationAPI_WithMockServer(t *testing.T) {
	privateKeyPEM := generateTestKey(t)
	store := keystore.NewMemoryStore()
	source := config.TokenSource{}
	fetcher := NewFetcher(store, source)

	jwtToken, err := fetcher.generateJWT(1, privateKeyPEM)
	if err != nil {
		t.Fatalf("generateJWT: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer "+jwtToken {
			t.Errorf("unexpected auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"token":"ghs_testtoken","expires_at":"2026-05-28T23:59:59Z"}`))
	}))
	defer server.Close()

	fetcher.httpClient = server.Client()

	// Use mock server URL
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/access_tokens", nil)
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := fetcher.httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCallInstallationAPI_NonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer server.Close()

	client := server.Client()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/access_tokens", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestNewFetcher_SetsTimeout(t *testing.T) {
	store := keystore.NewMemoryStore()
	source := config.TokenSource{}
	fetcher := NewFetcher(store, source)
	if fetcher.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", fetcher.httpClient.Timeout)
	}
}
