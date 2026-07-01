//go:build windows

package keystore

import (
	"fmt"
	"sort"
	"strings"

	"github.com/danieljoos/wincred"
)

func (o *osStore) List(prefix string) ([]string, error) {
	creds, err := wincred.CredEnumerateW("mcp-launcher/*", 0)
	if err != nil {
		return nil, fmt.Errorf("keystore list: enumerate: %w", err)
	}

	var keys []string
	for _, cred := range creds {
		targetName := cred.TargetName
		parts := strings.SplitN(targetName, "/", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[1]
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	sort.Strings(keys)
	return keys, nil
}
