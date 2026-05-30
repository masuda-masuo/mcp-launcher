package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
	"github.com/masuda-masuo/mcp-launcher/internal/refresher"
)

const defaultConfigPath = "launcher.json"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: mcp-launcher <service>\n")
		fmt.Fprintf(os.Stderr, "       mcp-launcher register <service> <ENV_KEY> <value>\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "register":
		if err := runRegister(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		if err := runLaunch(os.Args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func runLaunch(serviceName string) error {
	configPath := configFilePath()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	svc, ok := cfg[serviceName]
	if !ok {
		return fmt.Errorf("service %q not found in %s", serviceName, configPath)
	}

	store, err := keystore.NewOSStore()
	if err != nil {
		return fmt.Errorf("initializing keystore: %w", err)
	}

	// Phase 2: Token refresh before launch
	if svc.TokenSource != nil {
		tokenKey, ok := svc.EnvKeys[svc.TokenSource.TargetEnvKey]
		if !ok {
			return fmt.Errorf(
				"token_source.target_env_key %q not found in env_keys for service %q",
				svc.TokenSource.TargetEnvKey, serviceName,
			)
		}

		r := refresher.New(store, *svc.TokenSource, tokenKey)
		if err := r.RunOnce(context.Background()); err != nil {
			// Log warning but continue with existing token (fail-open)
			fmt.Fprintf(os.Stderr, "warning: token refresh failed: %v (continuing with existing token)\n", err)
		}
	}

	// Phase 2: Child process restart loop
	if svc.RestartIntervalSeconds > 0 {
		// Use signal.NotifyContext so Ctrl+C / SIGTERM triggers graceful shutdown
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer stop()
		return runChildWithRestart(ctx, svc, store, serviceName)
	}

	// Single run (default)
	env, err := buildEnv(svc, store, serviceName)
	if err != nil {
		return err
	}
	return runChildOnce(svc, env)
}

func buildEnv(svc config.ServiceConfig, store keystore.Store, serviceName string) ([]string, error) {
	env := os.Environ()
	for envKey, storeKey := range svc.EnvKeys {
		value, err := store.Get(storeKey)
		if err != nil {
			if keystore.IsNotFound(err) {
				return nil, fmt.Errorf(
					"secret %q not found in keystore — run: mcp-launcher register %s %s <value>",
					storeKey, serviceName, envKey,
				)
			}
			return nil, fmt.Errorf("retrieving secret %q: %w", storeKey, err)
		}
		env = append(env, envKey+"="+value)
	}
	return env, nil
}

func runChildOnce(svc config.ServiceConfig, env []string) error {
	args := append([]string{svc.Command}, svc.Args...)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func runChildWithRestart(ctx context.Context, svc config.ServiceConfig, store keystore.Store, serviceName string) error {
	interval := time.Duration(svc.RestartIntervalSeconds) * time.Second
	const backoff = 500 * time.Millisecond

	for {
		// Rebuild env to pick up refreshed tokens from keystore
		currentEnv, err := buildEnv(svc, store, serviceName)
		if err != nil {
			return fmt.Errorf("building env: %w", err)
		}

		args := append([]string{svc.Command}, svc.Args...)
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = currentEnv
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting child process: %w", err)
		}

		waitDone := make(chan error, 1)
		go func() {
			waitDone <- cmd.Wait()
		}()

		select {
		case <-ctx.Done():
			// Graceful shutdown: try graceful stop first, then force kill
			_ = cmd.Process.Signal(os.Interrupt)
			select {
			case <-waitDone:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-waitDone
			}
			fmt.Fprintf(os.Stderr, "info: mcp-launcher shutting down\n")
			return ctx.Err()

		case err := <-waitDone:
			// Child exited on its own — restart with backoff to prevent tight loop
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: child process exited with error: %v (restarting after backoff)\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "info: child process exited (restarting after backoff)\n")
			}
			time.Sleep(backoff)

		case <-time.After(interval):
			// Restart interval reached — kill and restart
			if err := cmd.Process.Kill(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to kill child process: %v\n", err)
			}
			<-waitDone
			fmt.Fprintf(os.Stderr, "info: restart interval reached, restarting child process\n")
		}

		// Refresh token before restart
		if svc.TokenSource != nil {
			tokenKey, ok := svc.EnvKeys[svc.TokenSource.TargetEnvKey]
			if ok {
				r := refresher.New(store, *svc.TokenSource, tokenKey)
				if err := r.RunOnce(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "warning: token refresh failed: %v (continuing)\n", err)
				}
			}
		}
	}
}

func runRegister(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: mcp-launcher register <service> <ENV_KEY> <value>")
	}
	service, envKey, value := args[0], args[1], args[2]
	storeKey := "mcp-launcher/" + service + "/" + envKey

	store, err := keystore.NewOSStore()
	if err != nil {
		return fmt.Errorf("initializing keystore: %w", err)
	}

	if err := store.Set(storeKey, value); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}

	fmt.Printf("\u2713 Registered %s \u2192 keystore key: %q\n", envKey, storeKey)
	fmt.Printf("  Add to launcher.json under service %q:\n", service)
	fmt.Printf("  \"env_keys\": { %q: %q }\n", envKey, storeKey)
	return nil
}

func configFilePath() string {
	if p := os.Getenv("MCP_LAUNCHER_CONFIG"); p != "" {
		return p
	}
	return defaultConfigPath
}
