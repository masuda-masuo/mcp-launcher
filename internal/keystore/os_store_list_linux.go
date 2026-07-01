//go:build linux

package keystore

import (
	"fmt"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
)

func (o *osStore) List(prefix string) ([]string, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("keystore list: dbus: %w", err)
	}

	obj := conn.Object("org.freedesktop.secrets", "/org/freedesktop/secrets")

	attrs := map[string]string{"service": service}
	var unlocked, locked []dbus.ObjectPath
	call := obj.Call("org.freedesktop.secrets.Service.SearchItems", 0, attrs)
	if call.Err != nil {
		return nil, fmt.Errorf("keystore list: search: %w", call.Err)
	}
	if err := call.Store(&unlocked, &locked); err != nil {
		return nil, fmt.Errorf("keystore list: decode: %w", err)
	}

	var keys []string
	for _, p := range append(unlocked, locked...) {
			item := conn.Object("org.freedesktop.secrets", p)
		var itemAttrs map[string]string
		err := item.Call("org.freedesktop.DBus.Properties.Get", 0,
			"org.freedesktop.secrets.Item", "Attributes").Store(&itemAttrs)
		if err != nil {
			continue
		}
		user, ok := itemAttrs["user"]
		if !ok {
			continue
		}
		if strings.HasPrefix(user, prefix) {
			keys = append(keys, user)
		}
	}

	sort.Strings(keys)
	return keys, nil
}
