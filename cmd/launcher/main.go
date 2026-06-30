package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"time"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
	"github.com/masuda-masuo/mcp-launcher/internal/mcpproxy"
	"github.com/masuda-masuo/mcp-launcher/internal/refresher"
)

const (
	warmUpTimeout = 5 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: mcp-launcher <service>\n")
		fmt.Fprintf(os.Stderr, "       mcp-launcher version\n")
		fmt.Fprintf(os.Stderr, "\nUse mcp-token register <service> <ENV_KEY> <value> for keystore registration.\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version", "-v", "--version":
		fmt.Println(version)
	case "register":
		fmt.Fprintf(os.Stderr, "warning: mcp-launcher register is deprecated; use mcp-token register instead\n")
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
	configPath := config.DefaultPath()

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

	if svc.TokenSource != nil {
		tokenKey, ok := svc.EnvKeys[svc.TokenSource.TargetEnvKey]
		if !ok {
			return fmt.Errorf(
				"token_source.target_env_key %q not found in env_keys for service %q",
				svc.TokenSource.TargetEnvKey, serviceName,
			)
		}

		warmCtx, cancel := context.WithTimeout(context.Background(), warmUpTimeout)
		defer cancel()
		r, err := refresher.New(warmCtx, store, *svc.TokenSource, tokenKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: creating refresher failed: %v (will retry on first check interval)\n", err)
		} else if err := r.RunOnce(warmCtx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: pre-launch token refresh skipped: %v (will retry on first check interval)\n", err)
		}
	}

	if svc.CheckIntervalSeconds > 0 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
		defer stop()
		return runChildWithRestart(ctx, svc, store, serviceName)
	}

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
					"secret %q not found in keystore — run: mcp-token register %s %s <value>",
					storeKey, serviceName, envKey,
				)
			}
			return nil, fmt.Errorf("retrieving secret %q: %w", storeKey, err)
		}
		env = append(env, envKey+"="+value)
	}
	return env, nil
}

// resolveCommand resolves command using exec.LookPath, which searches PATH.
// On Windows where PATH may be incomplete (e.g., launched from Claude Desktop),
// this ensures the executable is found using the current process's environment
// before falling back to the original command.
func resolveCommand(command string) string {
	if lp, err := exec.LookPath(command); err == nil {
		return lp
	}
	fmt.Fprintf(os.Stderr, "warning: command %q not found in PATH (will use as-is)\n", command)
	return command
}

func runChildOnce(svc config.ServiceConfig, env []string) error {
	cmd := exec.Command(resolveCommand(svc.Command), svc.Args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func runChildWithRestart(ctx context.Context, svc config.ServiceConfig, store keystore.Store, serviceName string) error {
	var (
		tokenStoreKey  string
		expiryStoreKey string
		hasToken       bool
	)
	if svc.TokenSource != nil {
		if k, ok := svc.EnvKeys[svc.TokenSource.TargetEnvKey]; ok {
			tokenStoreKey = k
			expiryStoreKey = k + "_EXPIRY"
			hasToken = true
		}
	}

	var tokMu sync.Mutex
	spawnedToken := ""

	refresh := func(ctx context.Context) error {
		if !hasToken {
			return nil
		}
		r, err := refresher.New(ctx, store, *svc.TokenSource, tokenStoreKey)
		if err != nil {
			return fmt.Errorf("creating refresher: %w", err)
		}
		return r.RunOnce(ctx)
	}

	spawn := func(context.Context) (mcpproxy.Child, error) {
		env, err := buildEnv(svc, store, serviceName)
		if err != nil {
			return nil, err
		}
		if hasToken {
			if tok, err := store.Get(tokenStoreKey); err == nil {
				tokMu.Lock()
				spawnedToken = tok
				tokMu.Unlock()
			}
		}

		cmd := exec.Command(resolveCommand(svc.Command), svc.Args...)
		cmd.Env = env
		cmd.Stderr = os.Stderr
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("stdin pipe: %w", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("stdout pipe: %w", err)
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("starting child process: %w", err)
		}
		return &execChild{cmd: cmd, stdin: stdin, stdout: stdout}, nil
	}

	restartReason := func() (bool, string) {
		if !hasToken {
			return false, ""
		}
		if expiryStr, err := store.Get(expiryStoreKey); err == nil {
			if expiry, perr := time.Parse(time.RFC3339, expiryStr); perr == nil {
				refreshBefore := time.Duration(svc.TokenSource.RefreshBeforeSeconds) * time.Second
				if time.Until(expiry) <= refreshBefore {
					return true, "token near expiry"
				}
			}
		}
		if tok, err := store.Get(tokenStoreKey); err == nil {
			tokMu.Lock()
			stale := spawnedToken != "" && tok != spawnedToken
			tokMu.Unlock()
			if stale {
				return true, "keystore token rotated"
			}
		}
		return false, ""
	}

	p := mcpproxy.New(mcpproxy.Options{
		ClientIn:      os.Stdin,
		ClientOut:     os.Stdout,
		Spawn:         spawn,
		Refresh:       refresh,
		RestartReason: restartReason,
		CheckInterval: time.Duration(svc.CheckIntervalSeconds) * time.Second,
		DrainTimeout:  time.Duration(svc.DrainTimeoutSeconds) * time.Second,
		Logf:          func(format string, a ...any) { fmt.Fprintf(os.Stderr, "info: "+format+"\n", a...) },
	})
	return p.Run(ctx)
}

type execChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (c *execChild) Stdin() io.Writer  { return c.stdin }
func (c *execChild) Stdout() io.Reader { return c.stdout }

func (c *execChild) Kill() error {
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
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
