//go:build !linux && !windows && !darwin

package keystore

import "fmt"

func (o *osStore) List(prefix string) ([]string, error) {
	return nil, fmt.Errorf("keystore: List not supported on this platform")
}
