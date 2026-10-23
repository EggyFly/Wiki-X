package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Local struct {
	dir string
}

func NewLocal(dir string) (*Local, error) {
	if dir == "" {
		return nil, errors.New("local media dir is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	return &Local{dir: dir}, nil
}

func (s *Local) Put(ctx context.Context, name, contentType string, data []byte) error {
	if err := safeName(name); err != nil {
		return err
	}
	target := filepath.Join(s.dir, name)
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		out.Close()
		_ = os.Remove(target)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(target)
		return err
	}
	return nil
}

func (s *Local) Delete(ctx context.Context, name string) error {
	if err := safeName(name); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(s.dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Local) URL(ctx context.Context, name string) (string, error) {
	if err := safeName(name); err != nil {
		return "", err
	}
	return "", nil
}

func (s *Local) Open(name string) (*os.File, error) {
	if err := safeName(name); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(s.dir, name))
}

func safeName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("invalid media name %q", name)
	}
	return nil
}
