package storage

import (
	"context"
	"errors"
	"testing"
)

func TestMemory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := NewMemory()
	data := []byte("hello")
	if err := m.Put(ctx, "a", "text/plain", data); err != nil {
		t.Fatal(err)
	}
	data[0] = 'j'
	got, err := m.Get(ctx, "a")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Get = %q, %v; want hello", got, err)
	}
	if err := m.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v, want ErrNotFound", err)
	}
}
