package keystore

import (
	"fmt"

	gokeyring "github.com/zalando/go-keyring"
)

const service = "mcp-launcher"

type osStore struct{}

func NewOSStore() (Store, error) {
	return &osStore{}, nil
}

func (o *osStore) Get(key string) (string, error) {
	value, err := gokeyring.Get(service, key)
	if err == gokeyring.ErrNotFound {
		return "", &ErrNotFound{Key: key}
	}
	if err != nil {
		return "", fmt.Errorf("keystore get %q: %w", key, err)
	}
	return value, nil
}

func (o *osStore) Set(key, value string) error {
	if err := gokeyring.Set(service, key, value); err != nil {
		return fmt.Errorf("keystore set %q: %w", key, err)
	}
	return nil
}

func (o *osStore) Delete(key string) error {
	if err := gokeyring.Delete(service, key); err != nil {
		if err == gokeyring.ErrNotFound {
			return &ErrNotFound{Key: key}
		}
		return fmt.Errorf("keystore delete %q: %w", key, err)
	}
	return nil
}
