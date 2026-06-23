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

// fetchTimeout bounds the GitHub API call that mints the installation token.
const fetchTimeout = 30 * time.Second

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
	fmt.Fprintf(w, "       mcp-token version\n\n")
	fmt.Fprintf(w, "Mints a fresh short-lived token for <service> using the credentials in\n")
	fmt.Fprintf(w, "the OS keystore (the service token_source in launcher.json) and prints it\n")
	fmt.Fprintf(w, "to stdout. Intended as a GITHUB_TOKEN_COMMAND provider for clients that\n")
	fmt.Fprintf(w, "run outside mcp-launcher, e.g. a streamable-http MCP daemon (issue #25).\n")
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

	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
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
