package gitsource

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type repoOptions struct {
	Dir    string
	URL    string
	Branch string
	Depth  int
	Pull   bool
}

type gitRunner func(ctx context.Context, args ...string) error

func EnsureRepo(ctx context.Context, dir, repoURL, branch string, depth int, pull bool) error {
	return ensureRepo(ctx, repoOptions{Dir: dir, URL: repoURL, Branch: branch, Depth: depth, Pull: pull}, runGit)
}

func ensureRepo(ctx context.Context, opts repoOptions, run gitRunner) error {
	dir := strings.TrimSpace(opts.Dir)
	if dir == "" {
		return fmt.Errorf("content dir is empty")
	}
	info, err := os.Stat(dir)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat content dir: %w", err)
	}
	if exists {
		if !info.IsDir() {
			return fmt.Errorf("content dir %s is not a directory", dir)
		}
		if isGitRepo(dir) {
			if opts.Pull {
				return run(ctx, "-C", dir, "pull", "--ff-only")
			}
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read content dir: %w", err)
		}
		if len(entries) > 0 {
			return fmt.Errorf("content dir %s is not empty and is not a git repository", dir)
		}
	}

	if strings.TrimSpace(opts.URL) == "" {
		return fmt.Errorf("git url is empty")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("create content parent dir: %w", err)
	}
	args := []string{"clone"}
	if opts.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(opts.Depth))
	}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}
	args = append(args, "--", opts.URL, dir)
	if err := run(ctx, args...); err != nil {
		if !exists {
			_ = os.RemoveAll(dir)
		}
		return err
	}
	return nil
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func runGit(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "git", args...)
	env := os.Environ()
	filtered := env[:0]
	for _, item := range env {
		if strings.HasPrefix(item, "GIT_TERMINAL_PROMPT=") {
			continue
		}
		filtered = append(filtered, item)
	}
	command.Env = append(filtered, "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		action := "git"
		for _, arg := range args {
			if arg == "clone" || arg == "pull" {
				action = "git " + arg
				break
			}
		}
		message := redactGitOutput(strings.TrimSpace(string(output)))
		if message == "" {
			return fmt.Errorf("%s: %w", action, err)
		}
		return fmt.Errorf("%s: %w: %s", action, err, message)
	}
	return nil
}

var gitURLUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/\s@]+@`)

func redactGitOutput(message string) string {
	return gitURLUserinfo.ReplaceAllString(message, "$1***@")
}
