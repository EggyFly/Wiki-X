package assets

import (
	"testing"
	"testing/fstest"
)

func TestFindHashed(t *testing.T) {
	fsys := fstest.MapFS{
		"app~1234abcd.css":   {Data: []byte("x")},
		"app~deadbeef.js":    {Data: []byte("x")},
		"style~00000000.css": {Data: []byte("x")},
	}
	if got := FindHashed(fsys, "app", ".css"); got != "app~1234abcd.css" {
		t.Fatalf("css = %q", got)
	}
	if got := FindHashed(fsys, "app", ".js"); got != "app~deadbeef.js" {
		t.Fatalf("js = %q", got)
	}
	if got := FindHashed(fsys, "missing", ".css"); got != "missing.css" {
		t.Fatalf("fallback = %q", got)
	}
	if !IsHashed("app~1234abcd.css") || IsHashed("app.css") || IsHashed("app~xyz.css") {
		t.Fatal("IsHashed failed")
	}
}
