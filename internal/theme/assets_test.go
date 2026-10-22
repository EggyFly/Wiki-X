package theme

import (
	"testing"
)

func TestHashedAssetNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "layout.html", "<!doctype html><body>{{template \"main\" .}}</body>")
	writeFile(t, dir, "page.html", "{{define \"main\"}}page{{end}}")
	writeFile(t, dir, "404.html", "{{define \"main\"}}404{{end}}")
	writeFile(t, dir, "assets/style~1234abcd.css", "body{}")
	writeFile(t, dir, "assets/favicon~deadbeef.svg", "<svg/>")

	themePack, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if themePack.StyleAsset() != "style~1234abcd.css" || themePack.FaviconAsset() != "favicon~deadbeef.svg" {
		t.Fatalf("assets = %q, %q", themePack.StyleAsset(), themePack.FaviconAsset())
	}
}
