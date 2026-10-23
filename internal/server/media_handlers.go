package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/EggyFly/Wiki-X/internal/auth"
	mediastore "github.com/EggyFly/Wiki-X/internal/media"
	"github.com/EggyFly/Wiki-X/internal/store"
)

var mediaNamePattern = regexp.MustCompile(`^([a-f0-9]{32}|pr-[a-f0-9]{16})\.(png|jpg|gif|webp|wav|mp3|ogg|flac)$`)

var mediaContentTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".wav":  "audio/wav",
	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
	".flac": "audio/flac",
}

func mediaContentType(name string) string {
	return mediaContentTypes[strings.ToLower(filepath.Ext(name))]
}

func (a *App) handleUpload(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightUpload)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "没有上传权限"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.cfg.Media.MaxSize+1<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "上传请求格式不正确或文件过大"})
		return
	}
	if !a.auth.CheckCSRF(r, session) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "请求校验失败"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有收到文件"})
		return
	}
	defer file.Close()
	switch strings.ToLower(filepath.Ext(header.Filename)) {
	case ".bnk", ".wem", ".bank", ".fsb":
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "不支持 Wwise/FMOD bank 文件, 请先导出为 wav/mp3/ogg/flac"})
		return
	}
	if header.Size > a.cfg.Media.MaxSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文件超过大小限制"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, a.cfg.Media.MaxSize+1))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "读取文件失败"})
		return
	}
	if int64(len(data)) > a.cfg.Media.MaxSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "文件超过大小限制"})
		return
	}
	contentType, ext := detectMedia(data)
	if ext == "" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "只支持 png, jpeg, gif, webp, wav, mp3, ogg, flac"})
		return
	}

	name, err := randomMediaName(ext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "生成文件名失败"})
		return
	}
	if a.mediaStore == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "媒体存储未配置"})
		return
	}
	if err := a.mediaStore.Put(r.Context(), name, contentType, data); err != nil {
		slog.Error("save media failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存文件失败"})
		return
	}
	if _, err := a.store.CreateMedia(r.Context(), name, contentType, int64(len(data)), session.User.ID); err != nil {
		_ = a.mediaStore.Delete(r.Context(), name)
		slog.Error("create media record failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存文件失败"})
		return
	}

	url := "/media/" + name
	markdown := "![图片](" + url + ")"
	if strings.HasPrefix(contentType, "audio/") {
		markdown = "[音频](" + url + ")"
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"url":      url,
		"markdown": markdown,
	})
}

func (a *App) handleMedia(w http.ResponseWriter, r *http.Request) {
	if a.cfg != nil && a.cfg.Content.Source == "postgres" {
		a.handleUploadedMedia(w, r)
		return
	}
	a.handleGitMedia(w, r)
}

func (a *App) handleGitMedia(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/media/")
	if !fs.ValidPath(name) || name == "." {
		http.NotFound(w, r)
		return
	}
	media := a.site.Media()
	if media == nil {
		http.NotFound(w, r)
		return
	}
	file, err := media.Open(name)
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
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if contentType := mediaContentType(name); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, name, info.ModTime(), seeker)
}

func (a *App) handleUploadedMedia(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/media/")
	if !mediaNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	media, err := a.store.GetMedia(r.Context(), name)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("get media failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if a.mediaStore == nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if target, err := a.mediaStore.URL(r.Context(), media.FileName); err != nil {
		slog.Error("media url failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	} else if target != "" {
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	local, ok := a.mediaStore.(*mediastore.Local)
	if !ok {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	file, err := local.Open(media.FileName)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", media.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, media.FileName, media.CreatedAt, file)
}

func detectMedia(data []byte) (string, string) {
	if len(data) >= 12 && (string(data[0:4]) == "RIFF" || string(data[0:4]) == "RF64") && string(data[8:12]) == "WAVE" {
		return "audio/wav", ".wav"
	}
	if len(data) >= 4 && string(data[0:4]) == "fLaC" {
		return "audio/flac", ".flac"
	}
	if len(data) >= 4 && string(data[0:4]) == "OggS" {
		return "audio/ogg", ".ogg"
	}
	if len(data) >= 3 && string(data[0:3]) == "ID3" {
		return "audio/mpeg", ".mp3"
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 {
		return "audio/mpeg", ".mp3"
	}
	contentType := http.DetectContentType(data)
	ext := ""
	switch contentType {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}
	if ext == "" && len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		contentType = "image/webp"
		ext = ".webp"
	}
	return contentType, ext
}

func randomMediaName(ext string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw) + ext, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
