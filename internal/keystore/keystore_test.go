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
