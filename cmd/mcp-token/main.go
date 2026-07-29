package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
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
		store, err := keystore.NewOSStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: initializing keystore: %v\n", err)
			os.Exit(1)
		}
		if err := runRegister(args[1:], store, os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) >= 1 && args[0] == "convert" {
		store, err := keystore.NewOSStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: initializing keystore: %v\n", err)
			os.Exit(1)
		}
		if err := runConvert(args[1:], store, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) >= 1 && args[0] == "list" {
		store, err := keystore.NewOSStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: initializing keystore: %v\n", err)
			os.Exit(1)
		}
		if err := runList(args[1:], store, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(args) >= 1 && args[0] == "delete" {
		store, err := keystore.NewOSStore()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: initializing keystore: %v\n", err)
			os.Exit(1)
		}
		if err := runDelete(args[1:], store, os.Stdin, os.Stdout); err != nil {
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
	fmt.Fprintf(w, "       mcp-token register <service> <ENV_KEY> --stdin\n")
	fmt.Fprintf(w, "       mcp-token list [<service>]\n")
	fmt.Fprintf(w, "       mcp-token delete <service> <KEY>\n")
	fmt.Fprintf(w, "       mcp-token delete --all <service>\n")
	fmt.Fprintf(w, "       mcp-token convert [<service>]\n")
	fmt.Fprintf(w, "       mcp-token version\n\n")
	fmt.Fprintf(w, "Mint:  mcp-token <service> prints a fresh short-lived token for <service>\n")
	fmt.Fprintf(w, "       using the token_source in launcher.json (issue #25).\n\n")
	fmt.Fprintf(w, "Register: mcp-token register stores a secret in the OS keystore\n")
	fmt.Fprintf(w, "       under mcp-token/<service>/<ENV_KEY>. Use --stdin to supply\n")
	fmt.Fprintf(w, "       the secret via standard input (keeps it off the command line).\n\n")
	fmt.Fprintf(w, "List:    mcp-token list prints all registered keys.\n")
	fmt.Fprintf(w, "       mcp-token list <service> prints keys for a specific service.\n\n")
	fmt.Fprintf(w, "Delete:  mcp-token delete <service> <KEY> deletes a single key.\n")
	fmt.Fprintf(w, "       mcp-token delete --all <service> deletes all keys for a service.\n")
	fmt.Fprintf(w, "       Add --force to skip confirmation prompt.\n\n")
	fmt.Fprintf(w, "Convert: mcp-token convert migrates keys from mcp-launcher/ to\n")
	fmt.Fprintf(w, "       mcp-token/ prefix. Supports --force to skip confirmation.\n")
	fmt.Fprintf(w, "       mcp-token convert <service> converts only that service.\n\n")
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

func runRegister(args []string, store keystore.Store, in io.Reader) error {
	// --stdin may appear anywhere; everything else is positional.
	useStdin := false
	positional := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--stdin" {
			useStdin = true
			continue
		}
		positional = append(positional, a)
	}

	var service, envKey, value string
	if useStdin {
		if len(positional) > 2 {
			return fmt.Errorf("cannot provide both a positional value and --stdin")
		}
		if len(positional) != 2 {
			return fmt.Errorf("usage: mcp-token register <service> <ENV_KEY> --stdin")
		}
		service, envKey = positional[0], positional[1]

		data, err := io.ReadAll(in)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		// Trailing newlines are stripped so that piping a pem file stores the
		// same bytes as the historical `register ... "$(cat pem)"` call --
		// command substitution strips them too, and a value that differs by a
		// single byte silently breaks every mint against the stored key.
		value = strings.TrimRight(string(data), "\n")
		if value == "" {
			return fmt.Errorf("stdin is empty; refusing to register an empty secret")
		}
	} else {
		if len(positional) != 3 {
			return fmt.Errorf("usage: mcp-token register <service> <ENV_KEY> <value>")
		}
		service, envKey, value = positional[0], positional[1], positional[2]
	}

	storeKey := "mcp-token/" + service + "/" + envKey
	if err := store.Set(storeKey, value); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}

	fmt.Printf("✓ Registered %s → keystore key: %q\n", envKey, storeKey)
	fmt.Printf("  Add to launcher.json under service %q:\n", service)
	fmt.Printf("  \"env_keys\": { %q: %q }\n", envKey, storeKey)
	return nil
}

func runDelete(args []string, store keystore.Store, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mcp-token delete <service> <KEY | --all>")
	}

	// Extract --force from any position
	force := false
	var filtered []string
	for _, a := range args {
		if a == "--force" {
			force = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered

	// --all <service> [--force]: delete all keys for a service
	if args[0] == "--all" {
		if len(args) < 2 {
			return fmt.Errorf("usage: mcp-token delete --all <service>")
		}
		service := args[1]

		// Collect keys for both current and legacy prefixes
		prefixes := []string{"mcp-token/" + service + "/", "mcp-launcher/" + service + "/"}
		var toDelete []string
		for _, prefix := range prefixes {
			ks, err := store.List(prefix)
			if err != nil {
				return fmt.Errorf("listing keys for %q: %w", service, err)
			}
			toDelete = append(toDelete, ks...)
		}

		if len(toDelete) == 0 {
			fmt.Fprintln(out, "(no keys registered for "+service+")")
			return nil
		}

		// --force skips confirmation
		if !force {
			fmt.Fprintf(out, "Delete %d key(s) for service %q? (y/N): ", len(toDelete), service)
			var answer string
			if _, err := fmt.Fscanln(in, &answer); err != nil {
				return fmt.Errorf("reading confirmation: %w", err)
			}
			if answer != "y" && answer != "Y" {
				fmt.Fprintln(out, "cancelled")
				return nil
			}
		}

		for _, k := range toDelete {
			if err := store.Delete(k); err != nil {
				return fmt.Errorf("deleting %q: %w", k, err)
			}
		}
		fmt.Fprintf(out, "✓ Deleted %d key(s) for service %q\n", len(toDelete), service)
		return nil
	}

	// <service> <KEY>: single key deletion
	if len(args) != 2 {
		return fmt.Errorf("usage: mcp-token delete <service> <KEY | --all>")
	}
	service, key := args[0], args[1]
	storeKey := "mcp-token/" + service + "/" + key

	if err := store.Delete(storeKey); err != nil {
		if !keystore.IsNotFound(err) {
			return fmt.Errorf("deleting %q: %w", storeKey, err)
		}
		// Not found under mcp-token/, try legacy prefix
		legacyKey := "mcp-launcher/" + service + "/" + key
		if err2 := store.Delete(legacyKey); err2 != nil {
			return fmt.Errorf("key %q not found for service %q (checked %q and %q)", key, service, storeKey, legacyKey)
		}
		fmt.Fprintf(out, "✓ Deleted %s\n", legacyKey)
		return nil
	}

	fmt.Fprintf(out, "✓ Deleted %s\n", storeKey)
	return nil
}

func runConvert(args []string, store keystore.Store, in io.Reader, out io.Writer) error {
	// --force from any position
	force := false
	var filtered []string
	for _, a := range args {
		if a == "--force" {
			force = true
		} else {
			filtered = append(filtered, a)
		}
	}
	args = filtered
	if len(args) > 1 {
		return fmt.Errorf("usage: mcp-token convert [<service>]")
	}
	prefix := "mcp-launcher/"
	if len(args) > 0 {
		prefix = "mcp-launcher/" + args[0] + "/"
	}

	allOld, err := store.List(prefix)
	if err != nil {
		return fmt.Errorf("listing legacy keys: %w", err)
	}
	if len(allOld) == 0 {
		fmt.Fprintln(out, "(no legacy keys to convert)")
		return nil
	}
	if !force {
		fmt.Fprintf(out, "Convert %d key(s) from mcp-launcher/ to mcp-token/ prefix? (y/N): ", len(allOld))
		var answer string
		if _, err := fmt.Fscanln(in, &answer); err != nil {
			return fmt.Errorf("reading confirmation: %w", err)
		}
		if answer != "y" && answer != "Y" {
			fmt.Fprintln(out, "cancelled")
			return nil
		}
	}
	converted := 0
	for _, oldKey := range allOld {
		val, err := store.Get(oldKey)
		if err != nil {
			fmt.Fprintf(out, "skipping %q: %v\n", oldKey, err)
			continue
		}
		newKey := "mcp-token/" + oldKey[len("mcp-launcher/"):]
		// Check for existing key with different value before overwriting
		if existing, err := store.Get(newKey); err == nil {
			if existing != val {
				fmt.Fprintf(out, "skipping %q → %q: target already exists with different value\n", oldKey, newKey)
				continue
			}
			// Same value already at new key, just delete the old one
			_ = store.Delete(oldKey)
			fmt.Fprintf(out, "✓ %s → %s (already present, cleaned up)\n", oldKey, newKey)
			continue
		}
		if err := store.Set(newKey, val); err != nil {
			fmt.Fprintf(out, "error writing %q: %v\n", newKey, err)
			continue
		}
		if err := store.Delete(oldKey); err != nil {
			fmt.Fprintf(out, "warning: wrote %q but could not delete %q: %v\n", newKey, oldKey, err)
			continue
		}
		fmt.Fprintf(out, "✓ %s → %s\n", oldKey, newKey)
		converted++
	}
	fmt.Fprintf(out, "\nConverted %d key(s)\n", converted)
	return nil
}

func runList(args []string, store keystore.Store, out io.Writer) error {
	// Search for both current (mcp-token/) and legacy (mcp-launcher/) prefixes
	// for backward compatibility (issue #27 naming unification).
	prefixes := []string{"mcp-token/"}
	if len(args) > 0 {
		prefixes = []string{"mcp-token/" + args[0] + "/", "mcp-launcher/" + args[0] + "/"}
	} else {
		prefixes = []string{"mcp-token/", "mcp-launcher/"}
	}

	seen := make(map[string]bool)
	var keys []string
	for _, prefix := range prefixes {
		ks, err := store.List(prefix)
		if err != nil {
			return fmt.Errorf("listing keys: %w", err)
		}
		for _, k := range ks {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}

	if len(keys) == 0 {
		fmt.Fprintln(out, "(no keys registered)")
		return nil
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintln(out, k)
	}
	return nil
}
