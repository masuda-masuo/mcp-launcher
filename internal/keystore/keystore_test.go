package keystore

import "testing"

func TestMemoryStore_SetAndGet(t *testing.T) {
	s := NewMemoryStore()

	if err := s.Set("mykey", "myvalue"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := s.Get("mykey")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != "myvalue" {
		t.Errorf("expected %q, got %q", "myvalue", got)
	}
}

func TestMemoryStore_GetNotFound(t *testing.T) {
	s := NewMemoryStore()

	_, err := s.Get("missing")
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if !IsNotFound(err) {
		t.Errorf("expected ErrNotFound, got %T: %v", err, err)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	s := NewMemoryStore()
	s.Set("key", "value")

	if err := s.Delete("key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := s.Get("key")
	if !IsNotFound(err) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestMemoryStore_Overwrite(t *testing.T) {
	s := NewMemoryStore()
	s.Set("key", "first")
	s.Set("key", "second")

	got, err := s.Get("key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != "second" {
		t.Errorf("expected %q after overwrite, got %q", "second", got)
	}
}

func TestMemoryStore_List(t *testing.T) {
	s := NewMemoryStore()
	s.Set("mcp-token/github/APP_ID", "12345")
	s.Set("mcp-token/github/PRIVATE_KEY", "abc")
	s.Set("mcp-token/csb/TOKEN", "xyz")
	s.Set("other/key", "val")

	t.Run("all mcp-token", func(t *testing.T) {
		keys, err := s.List("mcp-token/")
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(keys) != 3 {
			t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
		}
	})

	t.Run("filter by service", func(t *testing.T) {
		keys, err := s.List("mcp-token/github/")
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
		}
		if keys[0] != "mcp-token/github/APP_ID" {
			t.Errorf("expected APP_ID first, got %q", keys[0])
		}
		if keys[1] != "mcp-token/github/PRIVATE_KEY" {
			t.Errorf("expected PRIVATE_KEY second, got %q", keys[1])
		}
	})

	t.Run("empty prefix", func(t *testing.T) {
		keys, err := s.List("mcp-token/nonexistent/")
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(keys) != 0 {
			t.Fatalf("expected 0 keys, got %d", len(keys))
		}
	})
}

func TestMemoryStore_ListSorted(t *testing.T) {
	s := NewMemoryStore()
	s.Set("mcp-token/z/KEY", "1")
	s.Set("mcp-token/a/KEY", "2")
	s.Set("mcp-token/m/KEY", "3")

	keys, err := s.List("mcp-token/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "mcp-token/a/KEY" {
		t.Errorf("expected alphabetical order, got %q first", keys[0])
	}
	if keys[1] != "mcp-token/m/KEY" {
		t.Errorf("expected alphabetical order, got %q second", keys[1])
	}
	if keys[2] != "mcp-token/z/KEY" {
		t.Errorf("expected alphabetical order, got %q third", keys[2])
	}
}
