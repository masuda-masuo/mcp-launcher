package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	path := filepath.Join("testdata", "valid.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	svc, ok := cfg["github"]
	if !ok {
		t.Fatal("expected 'github' service in config")
	}
	if svc.Command != "github-mcp-server" {
		t.Errorf("expected command 'github-mcp-server', got %q", svc.Command)
	}
	if svc.EnvKeys["GITHUB_TOKEN"] != "mcp-token/github/GITHUB_TOKEN" {
		t.Errorf("unexpected env_key value: %q", svc.EnvKeys["GITHUB_TOKEN"])
	}
	if svc.CheckIntervalSeconds != 60 {
		t.Errorf("expected check_interval_seconds 60, got %d", svc.CheckIntervalSeconds)
	}
	// aws service should have default 0 (no check interval)
	awsSvc := cfg["aws"]
	if awsSvc.CheckIntervalSeconds != 0 {
		t.Errorf("expected aws check_interval_seconds 0, got %d", awsSvc.CheckIntervalSeconds)
	}
}

func TestLoad_MultipleServices(t *testing.T) {
	path := filepath.Join("testdata", "valid.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg) != 2 {
		t.Errorf("expected 2 services, got %d", len(cfg))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("nonexistent.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_MissingCommand(t *testing.T) {
	path := filepath.Join("testdata", "missing_command.json")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing command, got nil")
	}
}

func TestLoad_MissingEnvKeys(t *testing.T) {
	path := filepath.Join("testdata", "missing_env_keys.json")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing env_keys, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	f, err := os.CreateTemp("", "launcher-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("{invalid json}")
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}
