package assets

import (
	"io/fs"
	"strings"
)

func FindHashed(fsys fs.FS, base, ext string) string {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return base + ext
	}
	prefix := base + "~"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ext) {
			continue
		}
		hash := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ext)
		if validHash(hash) {
			return name
		}
	}
	return base + ext
}

func IsHashed(name string) bool {
	index := strings.LastIndexByte(name, '~')
	if index <= 0 {
		return false
	}
	rest := name[index+1:]
	dot := strings.LastIndexByte(rest, '.')
	if dot != 8 {
		return false
	}
	return validHash(rest[:dot])
}

func validHash(value string) bool {
	if len(value) != 8 {
		return false
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return false
	}
	return true
}
