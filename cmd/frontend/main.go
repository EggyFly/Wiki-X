package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/js"
	"github.com/tdewolff/minify/v2/svg"
)

var generatedPattern = regexp.MustCompile(`~[0-9a-f]{8}\.(css|js|svg)$`)

type job struct {
	source    string
	targetDir string
}

func main() {
	check := flag.Bool("check", false, "verify generated assets without writing")
	rootFlag := flag.String("root", "", "repository root")
	flag.Parse()

	root := strings.TrimSpace(*rootFlag)
	if root == "" {
		var err error
		root, err = findRoot()
		if err != nil {
			fatal(err)
		}
	}

	jobs, err := collectJobs(root)
	if err != nil {
		fatal(err)
	}
	outputs := make(map[string][]byte, len(jobs))
	expected := make(map[string]map[string]struct{})
	for _, item := range jobs {
		data, name, err := minifyFile(item.source)
		if err != nil {
			fatal(fmt.Errorf("%s: %w", item.source, err))
		}
		target := filepath.Join(item.targetDir, name)
		outputs[target] = data
		if expected[item.targetDir] == nil {
			expected[item.targetDir] = make(map[string]struct{})
		}
		expected[item.targetDir][name] = struct{}{}
	}

	if *check {
		if err := verify(outputs, expected); err != nil {
			fatal(err)
		}
		fmt.Printf("frontend assets are up to date (%d files)\n", len(outputs))
		return
	}
	if err := write(outputs, expected); err != nil {
		fatal(err)
	}
	fmt.Printf("frontend assets generated (%d files)\n", len(outputs))
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("cannot find repository root")
		}
		dir = parent
	}
}

func collectJobs(root string) ([]job, error) {
	dist := filepath.Join(root, "frontend", "dist")
	var jobs []job
	if err := appendJobs(&jobs, filepath.Join(root, "web", "ui"), filepath.Join(dist, "ui")); err != nil {
		return nil, err
	}
	themeRoot := filepath.Join(root, "web", "themes")
	entries, err := os.ReadDir(themeRoot)
	if err != nil {
		return nil, fmt.Errorf("read web/themes: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source := filepath.Join(themeRoot, entry.Name())
		target := filepath.Join(dist, "themes", entry.Name())
		if err := appendJobs(&jobs, source, target); err != nil {
			return nil, err
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].source < jobs[j].source })
	return jobs, nil
}

func appendJobs(jobs *[]job, sourceDir, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, source)
		if err != nil {
			return err
		}
		*jobs = append(*jobs, job{source: source, targetDir: filepath.Dir(filepath.Join(targetDir, rel))})
		return nil
	})
}

func minifyFile(file string) ([]byte, string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, "", err
	}
	ext := strings.ToLower(filepath.Ext(file))
	switch ext {
	case ".css":
		data, err = minifier().Bytes("text/css", data)
	case ".js":
		data, err = minifier().Bytes("application/javascript", data)
	case ".svg":
		data, err = minifier().Bytes("image/svg+xml", data)
	}
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])[:8]
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)) + "~" + hash + ext
	return data, name, nil
}

func minifier() *minify.M {
	m := minify.New()
	m.AddFunc("text/css", css.Minify)
	m.AddFunc("application/javascript", js.Minify)
	m.AddFunc("image/svg+xml", svg.Minify)
	return m
}

func write(outputs map[string][]byte, expected map[string]map[string]struct{}) error {
	for dir, names := range expected {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !generatedPattern.MatchString(entry.Name()) {
				continue
			}
			if _, ok := names[entry.Name()]; ok {
				continue
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	for target, data := range outputs {
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func verify(outputs map[string][]byte, expected map[string]map[string]struct{}) error {
	var problems []string
	for target, want := range outputs {
		got, err := os.ReadFile(target)
		if errors.Is(err, os.ErrNotExist) {
			problems = append(problems, "missing "+target)
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			problems = append(problems, "outdated "+target)
		}
	}
	for dir, names := range expected {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || !generatedPattern.MatchString(entry.Name()) {
				continue
			}
			if _, ok := names[entry.Name()]; !ok {
				problems = append(problems, "stale "+filepath.Join(dir, entry.Name()))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("frontend assets need regeneration:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "frontend build:", err)
	os.Exit(1)
}
