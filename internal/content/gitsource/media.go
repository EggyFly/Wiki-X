package gitsource

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var allowedMediaExt = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".gif":  true,
	".webp": true,
	".avif": true,
	".mp4":  true,
	".webm": true,
}

type mediaFS struct {
	root string
}

func (s *Source) MediaFS() fs.FS {
	return &mediaFS{root: s.Root}
}

func (m *mediaFS) Open(name string) (fs.File, error) {
	full, err := m.path(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return nil, fs.ErrNotExist
	}
	return os.Open(full)
}

func (m *mediaFS) Stat(name string) (fs.FileInfo, error) {
	if name == "." {
		root, err := m.dirPath(".")
		if err != nil {
			return nil, err
		}
		return os.Stat(root)
	}
	if !fs.ValidPath(name) {
		return nil, fs.ErrNotExist
	}
	if full, err := m.path(name); err == nil {
		return os.Stat(full)
	}
	if full, err := m.dirPath(name); err == nil {
		return os.Stat(full)
	}
	return nil, fs.ErrNotExist
}

func (m *mediaFS) ReadDir(name string) ([]fs.DirEntry, error) {
	full, err := m.dirPath(name)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	result := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if !entry.IsDir() && !allowedMediaExt[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

func (m *mediaFS) path(name string) (string, error) {
	if !fs.ValidPath(name) || name == "." || !validMediaPath(name) {
		return "", fs.ErrNotExist
	}
	full := filepath.Join(m.root, filepath.FromSlash(name))
	rootAbs, err := filepath.Abs(m.root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fs.ErrNotExist
	}
	real, err := filepath.EvalSymlinks(fullAbs)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(rootReal, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", fs.ErrNotExist
	}
	return real, nil
}

func (m *mediaFS) dirPath(name string) (string, error) {
	if name == "." {
		rootAbs, err := filepath.Abs(m.root)
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(rootAbs)
	}
	if !fs.ValidPath(name) || !validMediaDir(name) {
		return "", fs.ErrNotExist
	}
	full := filepath.Join(m.root, filepath.FromSlash(name))
	rootAbs, err := filepath.Abs(m.root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fs.ErrNotExist
	}
	real, err := filepath.EvalSymlinks(fullAbs)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(rootReal, real)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", fs.ErrNotExist
	}
	return real, nil
}

func validMediaPath(name string) bool {
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return allowedMediaExt[strings.ToLower(filepath.Ext(name))]
}

func validMediaDir(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
