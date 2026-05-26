package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/masuda-masuo/mcp-launcher/internal/config"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
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

	env := os.Environ()
	for envKey, storeKey := range svc.EnvKeys {
		value, err := store.Get(storeKey)
		if err != nil {
			if keystore.IsNotFound(err) {
				return fmt.Errorf(
					"secret %q not found in keystore — run: mcp-launcher register %s %s <value>",
					storeKey, serviceName, envKey,
				)
			}
			return fmt.Errorf("retrieving secret %q: %w", storeKey, err)
		}
		env = append(env, envKey+"="+value)
	}

	args := append([]string{svc.Command}, svc.Args...)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
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
