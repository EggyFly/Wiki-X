package theme

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

type Theme struct {
	page     *template.Template
	notFound *template.Template
	assets   fs.FS
	version  string
}

func Load(fsys fs.FS, root string) (*Theme, error) {
	root = strings.Trim(root, "/")
	if root == "." {
		root = ""
	}
	name := func(file string) string {
		if root == "" {
			return file
		}
		return path.Join(root, file)
	}
	funcs := template.FuncMap{
		"active": isActive,
	}

	page, err := template.New("layout.html").Funcs(funcs).ParseFS(fsys, name("layout.html"), name("page.html"))
	if err != nil {
		return nil, fmt.Errorf("parse theme templates: %w", err)
	}
	notFound, err := template.New("layout.html").Funcs(funcs).ParseFS(fsys, name("layout.html"), name("404.html"))
	if err != nil {
		return nil, fmt.Errorf("parse theme 404 templates: %w", err)
	}
	assets, err := fs.Sub(fsys, name("assets"))
	if err != nil {
		return nil, fmt.Errorf("theme assets: %w", err)
	}
	version, err := hashTheme(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("hash theme: %w", err)
	}

	return &Theme{page: page, notFound: notFound, assets: assets, version: version}, nil
}

func LoadDir(dir string) (*Theme, error) {
	return Load(os.DirFS(dir), ".")
}

func (t *Theme) RenderPage(w io.Writer, data any) error {
	return t.page.ExecuteTemplate(w, "layout.html", data)
}

func (t *Theme) RenderNotFound(w io.Writer, data any) error {
	return t.notFound.ExecuteTemplate(w, "layout.html", data)
}

func (t *Theme) Assets() fs.FS {
	return t.assets
}

func (t *Theme) Version() string {
	return t.version
}

func hashTheme(fsys fs.FS, root string) (string, error) {
	walkRoot := root
	if walkRoot == "" {
		walkRoot = "."
	}
	hash := sha256.New()
	err := fs.WalkDir(fsys, walkRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in theme: %s", filePath)
		}
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return err
		}
		hash.Write([]byte(filePath))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func isActive(current, href string) bool {
	if href == "/" {
		return current == "/"
	}
	base := strings.TrimSuffix(href, "/")
	return current == href || strings.HasPrefix(current, base+"/")
}
