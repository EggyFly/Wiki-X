package server

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) registrationMode() string {
	if a.cfg == nil {
		return "off"
	}
	mode := strings.ToLower(strings.TrimSpace(a.cfg.Registration.Mode))
	if mode == "" {
		return "off"
	}
	return mode
}

func (a *App) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	mode := a.registrationMode()
	if mode == "off" {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "站点没有开放注册.")
		return
	}
	if auth.SessionFromContext(r.Context()) != nil {
		a.redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	count, err := a.store.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "注册失败", "暂时无法读取用户信息.")
		return
	}
	if count == 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "请先由管理员创建第一个账号.")
		return
	}
	data := registerData{
		baseData:    a.baseData(r, "注册"),
		Mode:        mode,
		MailEnabled: a.mailEnabled(),
		Sent:        r.URL.Query().Get("sent") == "1",
	}
	data.CSRF = a.auth.LoginCSRF(w, r)
	a.renderUI(w, r, http.StatusOK, "register", data)
}

func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	mode := a.registrationMode()
	if mode == "off" {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "站点没有开放注册.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "请求格式不正确.")
		return
	}
	if !a.auth.CheckLoginCSRF(r) {
		a.renderMessage(w, r, http.StatusForbidden, "注册失败", "请求校验失败.")
		return
	}
	count, err := a.store.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "注册失败", "暂时无法读取用户信息.")
		return
	}
	if count == 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "请先由管理员创建第一个账号.")
		return
	}
	name := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm_password")
	switch {
	case name == "":
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "用户名不能为空.")
		return
	case utf8.RuneCountInString(name) > 64:
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "用户名不能超过 64 个字符.")
		return
	case strings.ContainsFunc(name, unicode.IsControl):
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "用户名不合法.")
		return
	case email == "" || !validEmail(email) || len(email) > 320:
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "邮箱格式不正确.")
		return
	case utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 128:
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "密码长度必须为 8 到 128 个字符.")
		return
	case password != confirm:
		a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "两次输入的新密码不一致.")
		return
	}
	if !a.allowRate(r.Context(), "register-ip:"+a.auth.ClientIP(r), 5, time.Hour) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "注册失败", "请求太频繁, 请稍后再试.")
		return
	}
	if !a.allowRate(r.Context(), "register-email:"+auth.HashEmailToken(strings.ToLower(email)), 3, time.Hour) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "注册失败", "请求太频繁, 请稍后再试.")
		return
	}

	inviteHash := ""
	if mode == "invite" {
		code := normalizeInviteCode(r.FormValue("invite_code"))
		if code == "" {
			a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "邀请码无效或已用完.")
			return
		}
		inviteHash = auth.HashEmailToken(code)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		slog.Error("hash register password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "注册失败", "暂时无法创建账号.")
		return
	}
	group := "user"
	if a.cfg != nil && strings.TrimSpace(a.cfg.Registration.DefaultGroup) != "" {
		group = strings.TrimSpace(a.cfg.Registration.DefaultGroup)
	}
	user, err := a.store.RegisterUser(r.Context(), store.RegisterInput{
		Name:         name,
		Email:        email,
		PasswordHash: hash,
		Group:        group,
		InviteHash:   inviteHash,
		Pending:      mode == "open",
		Detail:       mode,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrInviteInvalid):
			a.renderMessage(w, r, http.StatusBadRequest, "注册失败", "邀请码无效或已用完.")
		case errors.Is(err, store.ErrUserExists):
			a.renderMessage(w, r, http.StatusConflict, "注册失败", "用户名已存在.")
		case errors.Is(err, store.ErrEmailExists):
			a.renderMessage(w, r, http.StatusConflict, "注册失败", "邮箱已被其他账号使用.")
		case errors.Is(err, store.ErrBadGroup):
			slog.Error("register default group missing", "group", group)
			a.renderMessage(w, r, http.StatusInternalServerError, "注册失败", "暂时无法创建账号.")
		default:
			slog.Error("register user failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "注册失败", "暂时无法创建账号.")
		}
		return
	}
	if a.mailEnabled() {
		if err := a.sendVerifyEmail(r.Context(), user); err != nil {
			slog.Error("send register verify email failed", "err", err, "user", user.ID)
		}
	}
	if mode == "open" {
		a.redirect(w, r, "/register?sent=1", http.StatusSeeOther)
		return
	}
	a.redirect(w, r, "/login?notice=registered", http.StatusSeeOther)
}

func (a *App) handleRegisterVerifyPage(w http.ResponseWriter, r *http.Request) {
	if a.registrationMode() == "off" {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "站点没有开放注册.")
		return
	}
	data := registerVerifyData{
		baseData: a.baseData(r, "重发验证邮件"),
		Sent:     r.URL.Query().Get("sent") == "1",
	}
	data.CSRF = a.auth.LoginCSRF(w, r)
	a.renderUI(w, r, http.StatusOK, "register_verify", data)
}

func (a *App) handleRegisterVerify(w http.ResponseWriter, r *http.Request) {
	if a.registrationMode() == "off" {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "站点没有开放注册.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "发送失败", "请求格式不正确.")
		return
	}
	if !a.auth.CheckLoginCSRF(r) {
		a.renderMessage(w, r, http.StatusForbidden, "发送失败", "请求校验失败.")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" || !validEmail(email) || len(email) > 320 {
		a.renderMessage(w, r, http.StatusBadRequest, "发送失败", "邮箱格式不正确.")
		return
	}
	if !a.allowRate(r.Context(), "register-verify-ip:"+a.auth.ClientIP(r), 5, time.Hour) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "发送失败", "请求太频繁, 请稍后再试.")
		return
	}
	if !a.allowRate(r.Context(), "register-verify-email:"+auth.HashEmailToken(strings.ToLower(email)), 3, time.Hour) {
		a.redirect(w, r, "/register/verify?sent=1", http.StatusSeeOther)
		return
	}
	user, err := a.store.UserByEmail(r.Context(), email)
	if err == nil && !user.EmailVerified && a.mailEnabled() {
		if err := a.sendVerifyEmail(r.Context(), user); err != nil {
			slog.Error("resend register verify email failed", "err", err, "user", user.ID)
		}
	}
	a.redirect(w, r, "/register/verify?sent=1", http.StatusSeeOther)
}

func (a *App) handleAdminInvites(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	invites, err := a.store.ListInvites(r.Context(), 200)
	if err != nil {
		slog.Error("list invites failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取邀请码.")
		return
	}
	data := adminInvitesData{
		baseData: a.baseData(r, "邀请码"),
		Invites:  invites,
		Mode:     a.registrationMode(),
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_invites", data)
}

func (a *App) handleAdminInviteCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	label := strings.TrimSpace(r.FormValue("label"))
	if utf8.RuneCountInString(label) > 100 {
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "备注不能超过 100 个字符.")
		return
	}
	maxUses := 1
	if value, err := strconv.Atoi(r.FormValue("max_uses")); err == nil && value >= 1 && value <= 100 {
		maxUses = value
	}
	var expiresAt *time.Time
	if days, err := strconv.Atoi(r.FormValue("expires_days")); err == nil && days > 0 {
		if days > 3650 {
			days = 3650
		}
		at := time.Now().AddDate(0, 0, days)
		expiresAt = &at
	}
	code, err := newInviteCode()
	if err != nil {
		slog.Error("generate invite code failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "创建失败", "暂时无法创建邀请码.")
		return
	}
	if err := a.store.CreateInvite(r.Context(), auth.HashEmailToken(normalizeInviteCode(code)), label, maxUses, expiresAt, session.User.ID); err != nil {
		slog.Error("create invite failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "创建失败", "暂时无法创建邀请码.")
		return
	}
	invites, err := a.store.ListInvites(r.Context(), 200)
	if err != nil {
		slog.Error("list invites after create failed", "err", err)
	}
	data := adminInvitesData{
		baseData: a.baseData(r, "邀请码"),
		Invites:  invites,
		Mode:     a.registrationMode(),
		NewCode:  code,
	}
	data.Notice = a.noticeText("invite_created")
	a.renderUI(w, r, http.StatusOK, "admin_invites", data)
}

func (a *App) handleAdminInviteDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "邀请码编号不正确.")
		return
	}
	if err := a.store.DeleteInvite(r.Context(), id, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "邀请码不存在", "没有找到这个邀请码.")
			return
		}
		if errors.Is(err, store.ErrRevokeDisabled) {
			a.renderDeleteDisabled(w, r)
			return
		}
		slog.Error("delete invite failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法删除邀请码.")
		return
	}
	a.redirect(w, r, "/admin/invites?notice=invite_deleted", http.StatusSeeOther)
}

func newInviteCode() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	text := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	text = strings.ToUpper(text)
	parts := make([]string, 0, 4)
	for i := 0; i < len(text); i += 4 {
		parts = append(parts, text[i:i+4])
	}
	return strings.Join(parts, "-"), nil
}

func normalizeInviteCode(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ReplaceAll(raw, " ", "")
	return raw
}
