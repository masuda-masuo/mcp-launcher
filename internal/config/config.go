package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type TokenSource struct {
	Type                 string `json:"type"`
	AppIDKey             string `json:"app_id_key"`
	PrivateKeyKey        string `json:"private_key_key"`
	InstallationIDKey    string `json:"installation_id_key"`
	TargetEnvKey         string `json:"target_env_key"`
	RefreshBeforeSeconds int    `json:"refresh_before_seconds"`
}

type ServiceConfig struct {
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	EnvKeys               map[string]string `json:"env_keys"`
	TokenSource           *TokenSource      `json:"token_source,omitempty"`
	CheckIntervalSeconds  int               `json:"check_interval_seconds,omitempty"`
	DrainTimeoutSeconds   int               `json:"drain_timeout_seconds,omitempty"`
}

type Config map[string]ServiceConfig

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open config file %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("could not parse config file %q: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c Config) validate() error {
	for name, svc := range c {
		if svc.Command == "" {
			return fmt.Errorf("service %q: missing required field 'command'", name)
		}
		if len(svc.EnvKeys) == 0 {
			return fmt.Errorf("service %q: missing required field 'env_keys'", name)
		}
	}
	return nil
}
