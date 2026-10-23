package gitsource

import (
	"io/fs"
	"testing"
)

func TestMediaFS(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: Home\n---\n\nBody\n")
	writeFile(t, root, "images/a.png", "png")
	writeFile(t, root, "audio/theme.mp3", "mp3")
	writeFile(t, root, "images/note.md", "not media")
	writeFile(t, root, ".hidden.png", "hidden")

	media := New(root).MediaFS()
	file, err := media.Open("images/a.png")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	audio, err := media.Open("audio/theme.mp3")
	if err != nil {
		t.Fatal(err)
	}
	audio.Close()

	for _, name := range []string{"index.md", "images/note.md", ".hidden.png", "../outside.png"} {
		if _, err := media.Open(name); err == nil {
			t.Fatalf("%s should not be served", name)
		}
	}

	count := 0
	if err := fs.WalkDir(media, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			count++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("media files = %d", count)
	}
}
