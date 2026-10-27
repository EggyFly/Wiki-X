package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/EggyFly/Wiki-X/frontend"
	"github.com/EggyFly/Wiki-X/internal/site"
)

const manifestName = ".wiki_x-build.json"

type buildManifest struct {
	Files []string `json:"files"`
}

func Run(ctx context.Context, s *site.Site, outDir string) error {
	if strings.TrimSpace(outDir) == "" {
		return errors.New("output dir is empty")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}

	generated := make(map[string]struct{})
	writePage := func(rel string, data []byte) error {
		if err := writeFile(outDir, rel, data); err != nil {
			return err
		}
		generated[filepath.ToSlash(rel)] = struct{}{}
		return nil
	}

	for _, route := range s.Routes() {
		data, err := s.Render(ctx, route)
		if err != nil {
			return fmt.Errorf("render page %s: %w", route, err)
		}
		rel, err := pageFile(route)
		if err != nil {
			return err
		}
		if err := writePage(rel, data); err != nil {
			return fmt.Errorf("write page %s: %w", route, err)
		}
	}
	if err := writePage("404.html", s.NotFound()); err != nil {
		return fmt.Errorf("write 404 page: %w", err)
	}
	baseURL := s.Info().BaseURL
	if baseURL != "" {
		sitemap, err := s.Sitemap(ctx, baseURL)
		if err != nil {
			return fmt.Errorf("build sitemap: %w", err)
		}
		if err := writePage("sitemap.xml", sitemap); err != nil {
			return fmt.Errorf("write sitemap: %w", err)
		}
		feed, err := s.Feed(ctx, baseURL, 50)
		if err != nil {
			return fmt.Errorf("build feed: %w", err)
		}
		if err := writePage("feed.xml", feed); err != nil {
			return fmt.Errorf("write feed: %w", err)
		}
	}
	if err := writePage("robots.txt", s.Robots(baseURL)); err != nil {
		return fmt.Errorf("write robots.txt: %w", err)
	}
	skin := s.DefaultSkin()
	if err := copyAssets(s.AssetsFor(skin), outDir, generated, path.Join("assets", skin)); err != nil {
		return err
	}
	if err := copyUIAssets(s.Info(), outDir, generated); err != nil {
		return err
	}
	if media := s.Media(); media != nil {
		if err := copyMedia(media, outDir, generated); err != nil {
			return err
		}
	}
	if err := cleanupOld(outDir, generated); err != nil {
		return err
	}
	return writeManifest(outDir, generated)
}

func pageFile(route string) (string, error) {
	if route == "/" {
		return "index.html", nil
	}
	if !strings.HasPrefix(route, "/") || !strings.HasSuffix(route, "/") {
		return "", fmt.Errorf("bad route %q", route)
	}
	parts := strings.Split(strings.Trim(route, "/"), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("bad route %q", route)
		}
	}
	return path.Join(append(parts, "index.html")...), nil
}

func writeFile(outDir, rel string, data []byte) error {
	target, err := safeJoin(outDir, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, data, 0o644)
}

func copyAssets(assets fs.FS, outDir string, generated map[string]struct{}, assetRoot string) error {
	return fs.WalkDir(assets, ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in theme assets: %s", rel)
		}
		targetRel := path.Join(assetRoot, rel)
		if err := copyAsset(assets, outDir, rel, targetRel); err != nil {
			return fmt.Errorf("copy asset %s: %w", rel, err)
		}
		generated[targetRel] = struct{}{}
		return nil
	})
}

func copyUIAssets(info site.SiteInfo, outDir string, generated map[string]struct{}) error {
	assets, err := fs.Sub(frontend.FS, "dist/ui")
	if err != nil {
		return fmt.Errorf("open ui assets: %w", err)
	}
	for _, name := range []string{info.DiagramsAsset, info.MermaidAsset, info.KatexAsset} {
		if name == "" {
			continue
		}
		targetRel := path.Join("-", "assets", name)
		if err := copyAsset(assets, outDir, name, targetRel); err != nil {
			return fmt.Errorf("copy ui asset %s: %w", name, err)
		}
		generated[targetRel] = struct{}{}
	}
	return nil
}

func copyMedia(media fs.FS, outDir string, generated map[string]struct{}) error {
	return fs.WalkDir(media, ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink is not allowed in content media: %s", rel)
		}
		targetRel := path.Join("media", rel)
		if err := copyAsset(media, outDir, rel, targetRel); err != nil {
			return fmt.Errorf("copy media %s: %w", rel, err)
		}
		generated[targetRel] = struct{}{}
		return nil
	})
}

func copyAsset(assets fs.FS, outDir, sourceRel, targetRel string) error {
	src, err := assets.Open(sourceRel)
	if err != nil {
		return err
	}
	defer src.Close()

	target, err := safeJoin(outDir, targetRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	dst, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func cleanupOld(outDir string, generated map[string]struct{}) error {
	data, err := os.ReadFile(filepath.Join(outDir, manifestName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read build manifest: %w", err)
	}

	var old buildManifest
	if err := json.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("parse build manifest: %w", err)
	}
	dirs := make(map[string]struct{})
	for _, rel := range old.Files {
		if _, ok := generated[filepath.ToSlash(rel)]; ok {
			continue
		}
		target, err := safeJoin(outDir, rel)
		if err != nil {
			return fmt.Errorf("bad path in build manifest %s: %w", rel, err)
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove old file %s: %w", rel, err)
		}
		dirs[filepath.Dir(target)] = struct{}{}
	}
	return removeEmptyDirs(outDir, dirs)
}

func removeEmptyDirs(outDir string, dirs map[string]struct{}) error {
	if len(dirs) == 0 {
		return nil
	}
	list := make([]string, 0, len(dirs))
	for dir := range dirs {
		list = append(list, dir)
	}
	sort.Slice(list, func(i, j int) bool {
		return len(list[i]) > len(list[j])
	})

	rootAbs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	for _, dir := range list {
		for {
			dirAbs, err := filepath.Abs(dir)
			if err != nil || dirAbs == rootAbs {
				break
			}
			inside, err := filepath.Rel(rootAbs, dirAbs)
			if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				break
			}
			if err := os.Remove(dirAbs); err != nil {
				break
			}
			dir = filepath.Dir(dirAbs)
		}
	}
	return nil
}

func writeManifest(outDir string, generated map[string]struct{}) error {
	files := make([]string, 0, len(generated))
	for rel := range generated {
		files = append(files, rel)
	}
	sort.Strings(files)

	data, err := json.MarshalIndent(buildManifest{Files: files}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(outDir, manifestName), data, 0o644)
}

func safeJoin(outDir, rel string) (string, error) {
	rel = filepath.FromSlash(rel)
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", fmt.Errorf("absolute path %q", rel)
	}
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes output dir: %q", rel)
	}

	rootAbs, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootAbs, clean)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	inside, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", fmt.Errorf("path escapes output dir: %q", rel)
	}
	return targetAbs, nil
}
