package gitsource

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/EggyFly/Wiki-X/internal/content"
)

const DefaultMaxFileSize int64 = 1 << 20

type Source struct {
	Root        string
	MaxFileSize int64
}

func New(root string) *Source {
	return &Source{Root: root, MaxFileSize: DefaultMaxFileSize}
}

func (s *Source) SetMaxFileSize(size int64) {
	if size > 0 {
		s.MaxFileSize = size
	}
}

type frontMeta struct {
	Title          string   `yaml:"title"`
	Description    string   `yaml:"description"`
	Slug           string   `yaml:"slug"`
	Draft          bool     `yaml:"draft"`
	Updated        string   `yaml:"updated"`
	Tags           []string `yaml:"tags"`
	TranslationKey string   `yaml:"translation_key"`
}

func (s *Source) Load(ctx context.Context) ([]*content.Page, error) {
	info, err := os.Stat(s.Root)
	if err != nil {
		return nil, fmt.Errorf("content dir %s: %w", s.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("content dir %s is not a directory", s.Root)
	}

	var pages []*content.Page
	seen := make(map[string]string)
	err = filepath.WalkDir(s.Root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if filePath != s.Root && strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				return fmt.Errorf("content %s: symlink is not allowed", filePath)
			}
			return nil
		}
		if !entry.Type().IsRegular() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if fileInfo.Size() > s.MaxFileSize {
			return fmt.Errorf("content %s is too large: %d bytes (limit %d)", filePath, fileInfo.Size(), s.MaxFileSize)
		}
		page, err := s.readPage(filePath, fileInfo.ModTime())
		if err != nil {
			return err
		}
		routeKey := strings.ToLower(page.Route)
		if other, ok := seen[routeKey]; ok {
			return fmt.Errorf("route %s is used by %s and %s", page.Route, other, page.Key)
		}
		seen[routeKey] = page.Key
		pages = append(pages, page)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Route < pages[j].Route
	})
	return pages, nil
}

func (s *Source) readPage(filePath string, modTime time.Time) (*content.Page, error) {
	relPath, err := filepath.Rel(s.Root, filePath)
	if err != nil {
		return nil, err
	}
	relPath = filepath.ToSlash(relPath)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}
	if int64(len(data)) > s.MaxFileSize {
		return nil, fmt.Errorf("%s: file is too large: %d bytes (limit %d)", relPath, len(data), s.MaxFileSize)
	}
	return ParseDocument(relPath, data, modTime)
}

func ParseDocument(relPath string, data []byte, modTime time.Time) (*content.Page, error) {
	relPath = filepath.ToSlash(strings.TrimPrefix(relPath, "/"))
	sum := sha256.Sum256(data)

	meta, body, err := splitFrontMatter(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relPath, err)
	}

	var route string
	if strings.TrimSpace(meta.Slug) != "" {
		route, err = routeFromSlug(meta.Slug)
	} else {
		route, err = routeFromFile(relPath)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", relPath, err)
	}

	title := strings.TrimSpace(meta.Title)
	if title == "" {
		title = titleFromFile(relPath)
	}
	updated := strings.TrimSpace(meta.Updated)
	if updated == "" {
		updated = modTime.Format("2006-01-02")
	}

	tags := make([]string, 0, len(meta.Tags))
	for _, tag := range meta.Tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}

	return &content.Page{
		Key:            relPath,
		Route:          route,
		SourcePath:     relPath,
		Title:          title,
		Description:    strings.TrimSpace(meta.Description),
		Body:           string(body),
		Draft:          meta.Draft,
		Updated:        updated,
		Tags:           tags,
		Version:        hex.EncodeToString(sum[:]),
		TranslationKey: strings.TrimSpace(meta.TranslationKey),
	}, nil
}

type outputMeta struct {
	Title          string   `yaml:"title"`
	Description    string   `yaml:"description,omitempty"`
	Slug           string   `yaml:"slug,omitempty"`
	Draft          bool     `yaml:"draft,omitempty"`
	Updated        string   `yaml:"updated,omitempty"`
	Tags           []string `yaml:"tags,omitempty"`
	TranslationKey string   `yaml:"translation_key,omitempty"`
}

func FormatDocument(page *content.Page) ([]byte, error) {
	meta := outputMeta{
		Title:          strings.TrimSpace(page.Title),
		Description:    strings.TrimSpace(page.Description),
		Slug:           strings.Trim(page.Route, "/"),
		Draft:          page.Draft,
		Updated:        strings.TrimSpace(page.Updated),
		Tags:           page.Tags,
		TranslationKey: strings.TrimSpace(page.TranslationKey),
	}
	head, err := yaml.Marshal(meta)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(head)
	buf.WriteString("---\n\n")
	buf.WriteString(page.Body)
	if !strings.HasSuffix(page.Body, "\n") {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func FileForRoute(route string) string {
	route = strings.Trim(route, "/")
	if route == "" {
		return "index.md"
	}
	return route + ".md"
}

func splitFrontMatter(data []byte) (frontMeta, []byte, error) {
	var meta frontMeta
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(data, []byte("\n"))
	if len(lines) == 0 || strings.TrimSpace(string(lines[0])) != "---" {
		return meta, data, nil
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return meta, nil, errors.New("front matter is not closed")
	}
	if err := decodeFrontMatter(bytes.Join(lines[1:end], []byte("\n")), &meta); err != nil {
		return meta, nil, fmt.Errorf("front matter: %w", err)
	}
	body := bytes.Join(lines[end+1:], []byte("\n"))
	body = bytes.TrimPrefix(body, []byte("\n"))
	return meta, body, nil
}

const (
	maxFrontMatterNodes = 1000
	maxFrontMatterDepth = 20
)

func decodeFrontMatter(data []byte, meta *frontMeta) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root.Kind == 0 {
		return nil
	}
	count := 0
	if err := checkYAMLNode(&root, 0, &count); err != nil {
		return err
	}
	target := &root
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		target = root.Content[0]
	}
	return target.Decode(meta)
}

func checkYAMLNode(node *yaml.Node, depth int, count *int) error {
	if node == nil {
		return nil
	}
	if depth > maxFrontMatterDepth {
		return errors.New("front matter is too deep")
	}
	*count++
	if *count > maxFrontMatterNodes {
		return errors.New("front matter is too complex")
	}
	if node.Kind == yaml.AliasNode {
		return errors.New("front matter aliases are not allowed")
	}
	for _, child := range node.Content {
		if err := checkYAMLNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func RouteForFile(relPath string) (string, error) {
	return routeFromFile(relPath)
}

func routeFromFile(relPath string) (string, error) {
	noExt := strings.TrimSuffix(relPath, path.Ext(relPath))
	parts := strings.Split(noExt, "/")
	if len(parts) > 0 && parts[len(parts)-1] == "index" {
		parts = parts[:len(parts)-1]
	}
	return buildRoute(parts)
}

func routeFromSlug(slug string) (string, error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if slug == "" {
		return "/", nil
	}
	return buildRoute(strings.Split(slug, "/"))
}

func buildRoute(parts []string) (string, error) {
	if len(parts) == 0 {
		return "/", nil
	}
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if err := checkSegment(part); err != nil {
			return "", err
		}
		clean = append(clean, part)
	}
	return "/" + strings.Join(clean, "/") + "/", nil
}

func checkSegment(part string) error {
	if part == "" || part == "." || part == ".." {
		return fmt.Errorf("bad route segment %q", part)
	}
	if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
		return fmt.Errorf("bad route segment %q", part)
	}
	if isWindowsReserved(part) {
		return fmt.Errorf("route segment %q is reserved on windows", part)
	}
	for _, r := range part {
		if unicode.IsControl(r) || strings.ContainsRune(`\:*?"<>|#%`, r) {
			return fmt.Errorf("bad character %q in route segment", r)
		}
	}
	return nil
}

func isWindowsReserved(part string) bool {
	name := strings.ToUpper(part)
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) {
		return name[3] >= '1' && name[3] <= '9'
	}
	return false
}

func titleFromFile(relPath string) string {
	base := path.Base(relPath)
	base = strings.TrimSuffix(base, path.Ext(base))
	if base == "index" {
		dir := path.Dir(relPath)
		if dir == "." {
			return "Home"
		}
		base = path.Base(dir)
	}
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}
