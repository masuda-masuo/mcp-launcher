package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
	"github.com/masuda-masuo/mcp-launcher/internal/refresher"
)

// version is stamped at release time via -ldflags "-X main.version=...".
// mcp-token is released on its own tag namespace (mcp-token/vX.Y.Z) so it can
// version independently of the launcher; see release.yml (issue #25).
var version = "dev"

// defaultFetchTimeout bounds the GitHub API call that mints the installation
// token. Override it with the MCP_TOKEN_FETCH_TIMEOUT env var (a Go duration
// such as "45s") when the API is slow, without recompiling (issue #25 review).
const defaultFetchTimeout = 30 * time.Second

// fetchTimeoutEnv is the env var that overrides defaultFetchTimeout.
const fetchTimeoutEnv = "MCP_TOKEN_FETCH_TIMEOUT"

func main() {
	args := os.Args[1:]
	if len(args) == 1 {
		switch args[0] {
		case "version", "-v", "--version":
			fmt.Println(version)
			return
		case "-h", "--help":
			usage(os.Stdout)
			return
		}
	}
	if len(args) >= 1 && args[0] == "register" {
		if err := runRegister(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) != 1 {
		usage(os.Stderr)
		os.Exit(2)
	}

	store, err := keystore.NewOSStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: initializing keystore: %v\n", err)
		os.Exit(1)
	}
	if err := run(args[0], store, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "Usage: mcp-token <service>\n")
	fmt.Fprintf(w, "       mcp-token register <service> <ENV_KEY> <value>\n")
	fmt.Fprintf(w, "       mcp-token version\n\n")
	fmt.Fprintf(w, "Mint:  mcp-token <service> prints a fresh short-lived token for <service>\n")
	fmt.Fprintf(w, "       using the token_source in launcher.json (issue #25).\n\n")
	fmt.Fprintf(w, "Register: mcp-token register stores a secret in the OS keystore\n")
	fmt.Fprintf(w, "       under mcp-token/<service>/<ENV_KEY>.\n\n")
	fmt.Fprintf(w, "Env:\n")
	fmt.Fprintf(w, "  MCP_TOKEN_FETCH_TIMEOUT  GitHub API timeout as a Go duration (default 30s).\n")
}

// resolveFetchTimeout returns the API timeout, applying the
// MCP_TOKEN_FETCH_TIMEOUT override when it parses to a positive Go duration and
// falling back to defaultFetchTimeout (with a warning to w) otherwise.
func resolveFetchTimeout(env string, w io.Writer) time.Duration {
	if env == "" {
		return defaultFetchTimeout
	}
	if d, err := time.ParseDuration(env); err == nil && d > 0 {
		return d
	}
	fmt.Fprintf(w, "warning: ignoring invalid %s %q; using %s\n", fetchTimeoutEnv, env, defaultFetchTimeout)
	return defaultFetchTimeout
}

// run resolves the service config, mints a fresh token via the shared refresher,
// and writes it to out. The store is injected so the logic is unit-testable with
// an in-memory keystore.
func run(serviceName string, store keystore.Store, out io.Writer) error {
	cfgPath := config.DefaultPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	svc, ok := cfg[serviceName]
	if !ok {
		return fmt.Errorf("service %q not found in %s", serviceName, cfgPath)
	}
	if svc.TokenSource == nil {
		return fmt.Errorf("service %q has no token_source; nothing to mint", serviceName)
	}
	if svc.TokenSource.Type != "github_app" {
		return fmt.Errorf("token_source.type %q is not supported by mcp-token (only github_app)", svc.TokenSource.Type)
	}
	tokenKey, ok := svc.EnvKeys[svc.TokenSource.TargetEnvKey]
	if !ok {
		return fmt.Errorf("token_source.target_env_key %q not found in env_keys for service %q", svc.TokenSource.TargetEnvKey, serviceName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), resolveFetchTimeout(os.Getenv(fetchTimeoutEnv), os.Stderr))
	defer cancel()

	r, err := refresher.New(ctx, store, *svc.TokenSource, tokenKey)
	if err != nil {
		return fmt.Errorf("building token fetcher: %w", err)
	}
	token, _, err := r.Token(ctx)
	if err != nil {
		return fmt.Errorf("minting token: %w", err)
	}
	fmt.Fprintln(out, token)
	return nil
}

func runRegister(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: mcp-token register <service> <ENV_KEY> <value>")
	}
	service, envKey, value := args[0], args[1], args[2]
	storeKey := "mcp-token/" + service + "/" + envKey

	store, err := keystore.NewOSStore()
	if err != nil {
		return fmt.Errorf("initializing keystore: %w", err)
	}

	if err := store.Set(storeKey, value); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}

	fmt.Printf("✓ Registered %s → keystore key: %q\n", envKey, storeKey)
	fmt.Printf("  Add to launcher.json under service %q:\n", service)
	fmt.Printf("  \"env_keys\": { %q: %q }\n", envKey, storeKey)
	return nil
}
