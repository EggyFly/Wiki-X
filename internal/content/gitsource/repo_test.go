package gitsource

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRepoClone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	var calls [][]string
	run := func(ctx context.Context, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	if err := ensureRepo(context.Background(), repoOptions{
		Dir: dir, URL: "https://example.com/wiki.git", Branch: "main", Depth: 1,
	}, run); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	joined := strings.Join(calls[0], " ")
	for _, want := range []string{"clone", "--depth 1", "--branch main", "https://example.com/wiki.git", dir} {
		if !strings.Contains(joined, want) {
			t.Fatalf("clone args missing %q: %s", want, joined)
		}
	}
}

func TestEnsureRepoPull(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	run := func(ctx context.Context, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	if err := ensureRepo(context.Background(), repoOptions{Dir: dir, Pull: true}, run); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "-C "+dir+" pull --ff-only" {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestEnsureRepoRejectsNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, args ...string) error {
		t.Fatal("git should not run")
		return nil
	}
	err := ensureRepo(context.Background(), repoOptions{Dir: dir, URL: "https://example.com/wiki.git"}, run)
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureRepoRequiresURL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	run := func(ctx context.Context, args ...string) error {
		t.Fatal("git should not run")
		return nil
	}
	err := ensureRepo(context.Background(), repoOptions{Dir: dir}, run)
	if err == nil || !strings.Contains(err.Error(), "git url is empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestRedactGitOutput(t *testing.T) {
	message := "fatal: unable to access 'https://user:secret@example.com/repo/': 403"
	got := redactGitOutput(message)
	if strings.Contains(got, "secret") || !strings.Contains(got, "https://***@example.com/repo/") {
		t.Fatalf("redacted = %q", got)
	}
	if redactGitOutput("fatal: repository 'https://example.com/repo/' not found") != "fatal: repository 'https://example.com/repo/' not found" {
		t.Fatal("url without userinfo should stay")
	}
}

func TestEnsureRepoCloneFailureCleansDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "content")
	run := func(ctx context.Context, args ...string) error {
		return errors.New("boom")
	}
	if err := ensureRepo(context.Background(), repoOptions{Dir: dir, URL: "https://example.com/wiki.git"}, run); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("partial dir should be removed, err = %v", err)
	}
}
