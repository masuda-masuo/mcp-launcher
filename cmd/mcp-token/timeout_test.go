package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestResolveFetchTimeout_Default(t *testing.T) {
	if got := resolveFetchTimeout("", &bytes.Buffer{}); got != defaultFetchTimeout {
		t.Errorf("got %s, want %s", got, defaultFetchTimeout)
	}
}

func TestResolveFetchTimeout_Override(t *testing.T) {
	if got := resolveFetchTimeout("5s", &bytes.Buffer{}); got != 5*time.Second {
		t.Errorf("got %s, want 5s", got)
	}
}

func TestResolveFetchTimeout_InvalidFallsBack(t *testing.T) {
	var w bytes.Buffer
	if got := resolveFetchTimeout("nonsense", &w); got != defaultFetchTimeout {
		t.Errorf("got %s, want default %s", got, defaultFetchTimeout)
	}
	if !strings.Contains(w.String(), "ignoring invalid") {
		t.Errorf("expected warning, got %q", w.String())
	}
}

func TestResolveFetchTimeout_NonPositiveFallsBack(t *testing.T) {
	if got := resolveFetchTimeout("0s", &bytes.Buffer{}); got != defaultFetchTimeout {
		t.Errorf("got %s, want default", got)
	}
}
