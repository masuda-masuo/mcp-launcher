//go:build windows

package keystore

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

func (o *osStore) List(prefix string) ([]string, error) {
	cmd := exec.Command("cmdkey", "/list")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("keystore list: cmdkey: %w", err)
	}

	prefixMatch := "target=" + service + "/"
	var keys []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), "target:") {
			continue
		}
		// Line format: "Target: LegacyGeneric:target=mcp-launcher/KEYNAME"
		idx := strings.Index(line, prefixMatch)
		if idx < 0 {
			continue
		}
		key := line[idx+len(prefixMatch):]
		// Remove trailing type info after potential newlines/spaces
		key = strings.TrimRight(key, " \r")
		// cmdkey /list returns only the relative key (e.g. "github/APP_ID"),
		// not the full key. Reconstruct with service prefix.
		fullKey := service + "/" + key
		if strings.HasPrefix(fullKey, prefix) {
			keys = append(keys, fullKey)
		}
	}

	if len(keys) == 0 {
		return nil, nil
	}
	sort.Strings(keys)
	return keys, nil
}
