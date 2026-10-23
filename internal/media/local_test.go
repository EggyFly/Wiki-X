package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("audio")
	if err := store.Put(context.Background(), "a.mp3", "audio/mpeg", data); err != nil {
		t.Fatal(err)
	}
	file, err := store.Open("a.mp3")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := os.Stat(filepath.Join(dir, "a.mp3")); err != nil {
		t.Fatal(err)
	}
	u, err := store.URL(context.Background(), "a.mp3")
	if err != nil || u != "" {
		t.Fatalf("url = %q, %v", u, err)
	}
	if err := store.Delete(context.Background(), "a.mp3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.mp3")); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestLocalStoreRejectsTraversal(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../x", "a/b", "..", ""} {
		if err := store.Put(context.Background(), name, "audio/mpeg", nil); err == nil {
			t.Fatalf("%q should be rejected", name)
		}
	}
}
