package server

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EggyFly/Wiki-X/internal/assets"
	"github.com/EggyFly/Wiki-X/internal/site"
)

func (a *App) servePage(w http.ResponseWriter, r *http.Request) {
	cleanPath := path.Clean(r.URL.Path)
	if strings.HasSuffix(r.URL.Path, "/") && cleanPath != "/" {
		cleanPath += "/"
	}
	if cleanPath != r.URL.Path {
		redirect(w, r, cleanPath)
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/") && a.site.HasRoute(r.URL.Path+"/") {
		redirect(w, r, r.URL.Path+"/")
		return
	}

	skin := a.site.DefaultSkin()
	if cookie, err := r.Cookie("wiki_x_skin"); err == nil && a.site.HasSkin(cookie.Value) {
		skin = cookie.Value
	}
	data, err := a.site.RenderSkin(r.Context(), r.URL.Path, skin)
	if errors.Is(err, site.ErrNotFound) {
		writeHTML(w, r, http.StatusNotFound, a.site.NotFoundSkin(skin), a.site.Switchable())
		return
	}
	if err != nil {
		slog.Error("render page failed", "path", r.URL.Path, "err", err)
		writePlain(w, r, http.StatusInternalServerError, "internal server error\n")
		return
	}
	writeHTML(w, r, http.StatusOK, data, a.site.Switchable())
}

func (a *App) handleSkin(w http.ResponseWriter, r *http.Request) {
	skin := strings.TrimSpace(r.URL.Query().Get("name"))
	if !a.site.HasSkin(skin) {
		http.NotFound(w, r)
		return
	}
	secure := false
	if a.cfg != nil {
		secure = a.cfg.Auth.SecureCookies
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "wiki_x_skin",
		Value:    skin,
		Path:     "/",
		MaxAge:   31536000,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}

func (a *App) handleThemeAsset(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/assets/")
	skin := a.site.DefaultSkin()
	if first, tail, ok := strings.Cut(rest, "/"); ok && a.site.HasSkin(first) {
		skin = first
		rest = tail
	}
	if rest == "" || !fs.ValidPath(rest) {
		http.NotFound(w, r)
		return
	}
	file, err := a.site.AssetsFor(skin).Open(rest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("v") != "" || assets.IsHashed(path.Base(r.URL.Path)) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, rest, info.ModTime(), seeker)
}

func redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, "ok\n")
	}
}

func writeHTML(w http.ResponseWriter, r *http.Request, status int, data []byte, switchable bool) {
	etag := etagFor(data)
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("ETag", etag)
	vary := "Accept-Encoding"
	if switchable {
		vary += ", Cookie"
	}
	header.Set("Vary", vary)
	if status == http.StatusNotFound {
		header.Set("Cache-Control", "public, max-age=60")
	} else {
		header.Set("Cache-Control", "public, max-age=60, s-maxage=300, stale-while-revalidate=60")
	}

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	useGzip := acceptsGzip(r.Header.Get("Accept-Encoding")) && len(data) >= 512
	if useGzip {
		header.Set("Content-Encoding", "gzip")
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if useGzip {
		if err := writeGzip(w, data); err != nil {
			slog.Warn("gzip write failed", "path", r.URL.Path, "err", err)
		}
		return
	}
	_, _ = w.Write(data)
}

func writePlain(w http.ResponseWriter, r *http.Request, status int, text string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, text)
	}
}

func etagFor(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func etagMatches(header, etag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "W/")
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		for _, attr := range fields[1:] {
			attr = strings.TrimSpace(attr)
			if !strings.HasPrefix(strings.ToLower(attr), "q=") {
				continue
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(attr[2:]), 64)
			if err == nil && q <= 0 {
				return false
			}
		}
		return true
	}
	return false
}

var gzipPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

func writeGzip(w io.Writer, data []byte) error {
	writer := gzipPool.Get().(*gzip.Writer)
	writer.Reset(w)
	_, err := writer.Write(data)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	gzipPool.Put(writer)
	return err
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

type logWriter struct {
	http.ResponseWriter
	status int
}

func (w *logWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *logWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func writeLog(next http.Handler, access bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/-/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		lw := &logWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lw, r)
		if !access && lw.status < 500 {
			return
		}

		level := slog.LevelInfo
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			level = slog.LevelDebug
		}
		if lw.status >= 500 {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "http request", "method", r.Method, "path", r.URL.Path, "status", lw.status, "dur", time.Since(start).Round(time.Millisecond))
	})
}

func cacheAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" || assets.IsHashed(path.Base(r.URL.Path)) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}

type onlyFilesFS struct {
	fs.FS
}

func (o onlyFilesFS) Open(name string) (fs.File, error) {
	file, err := o.FS.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.IsDir() {
		file.Close()
		return nil, fs.ErrNotExist
	}
	return file, nil
}
