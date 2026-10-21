package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/i18n"
	"github.com/EggyFly/Wiki-X/internal/store"
)

const (
	emailVerifyTTL = 24 * time.Hour
	emailResetTTL  = 30 * time.Minute
)

func (a *App) mailEnabled() bool {
	return a.mailer != nil
}

func (a *App) handleForgotPage(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	data := forgotData{baseData: a.baseData(r, "找回密码")}
	data.CSRF = a.auth.LoginCSRF(w, r)
	data.Sent = r.URL.Query().Get("sent") == "1"
	a.renderUI(w, r, http.StatusOK, "forgot", data)
}

func (a *App) handleForgot(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "找回失败", "请求格式不正确.")
		return
	}
	if !a.auth.CheckLoginCSRF(r) {
		a.renderMessage(w, r, http.StatusForbidden, "找回失败", "请求校验失败.")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" || !validEmail(email) || len(email) > 320 {
		a.renderMessage(w, r, http.StatusBadRequest, "找回失败", "邮箱格式不正确.")
		return
	}
	ipKey := a.auth.ClientIP(r)
	if !a.allowRate(r.Context(), "mail-forgot-ip:"+ipKey, 10, 15*time.Minute) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "找回失败", "请求太频繁, 请稍后再试.")
		return
	}
	if !a.allowRate(r.Context(), "mail-forgot-email:"+auth.HashEmailToken(strings.ToLower(email)), 3, 15*time.Minute) {
		a.redirect(w, r, "/forgot?sent=1", http.StatusSeeOther)
		return
	}
	user, err := a.store.UserByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		slog.Error("load user by email failed", "err", err)
	}
	if err == nil && !user.Disabled && user.EmailVerified {
		if err := a.sendResetEmail(r.Context(), user); err != nil {
			slog.Error("send reset email failed", "err", err, "user", user.ID)
		}
	}
	a.redirect(w, r, "/forgot?sent=1", http.StatusSeeOther)
}

func (a *App) handleResetPage(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if !validEmailToken(token) {
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "链接无效或已过期.")
		return
	}
	data := resetData{baseData: a.baseData(r, "重置密码"), Token: token}
	data.CSRF = a.auth.LoginCSRF(w, r)
	a.renderUI(w, r, http.StatusOK, "reset", data)
}

func (a *App) handleReset(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "请求格式不正确.")
		return
	}
	if !a.auth.CheckLoginCSRF(r) {
		a.renderMessage(w, r, http.StatusForbidden, "重置失败", "请求校验失败.")
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	password := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	if !validEmailToken(token) {
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "链接无效或已过期.")
		return
	}
	if !a.allowRate(r.Context(), "mail-reset-ip:"+a.auth.ClientIP(r), 10, 15*time.Minute) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "重置失败", "请求太频繁, 请稍后再试.")
		return
	}
	switch {
	case utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 128:
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "密码长度必须为 8 到 128 个字符.")
		return
	case password != confirm:
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "两次输入的新密码不一致.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		slog.Error("hash reset password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "重置失败", "暂时无法重置密码.")
		return
	}
	userID, err := a.store.ConsumeEmailToken(r.Context(), store.EmailTokenReset, auth.HashEmailToken(token))
	if errors.Is(err, store.ErrNotFound) {
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "链接无效或已过期.")
		return
	}
	if err != nil {
		slog.Error("consume reset token failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "重置失败", "暂时无法重置密码.")
		return
	}
	user, err := a.store.UserByID(r.Context(), userID)
	if err != nil || user.Disabled {
		a.renderMessage(w, r, http.StatusBadRequest, "重置失败", "链接无效或已过期.")
		return
	}
	if err := a.store.UpdatePassword(r.Context(), user.ID, hash, "user.password_reset", user.ID); err != nil {
		slog.Error("update reset password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "重置失败", "暂时无法重置密码.")
		return
	}
	_ = a.store.SetEmailVerified(r.Context(), user.ID, true)
	a.redirect(w, r, "/login?notice=password_reset", http.StatusSeeOther)
}

func (a *App) handleVerifyEmailPage(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if !validEmailToken(token) {
		a.renderMessage(w, r, http.StatusBadRequest, "验证失败", "链接无效或已过期.")
		return
	}
	data := verifyEmailData{baseData: a.baseData(r, "验证邮箱"), Token: token}
	data.CSRF = a.auth.LoginCSRF(w, r)
	a.renderUI(w, r, http.StatusOK, "verify_email", data)
}

func (a *App) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "验证失败", "请求格式不正确.")
		return
	}
	if !a.auth.CheckLoginCSRF(r) {
		a.renderMessage(w, r, http.StatusForbidden, "验证失败", "请求校验失败.")
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	if !validEmailToken(token) {
		a.renderMessage(w, r, http.StatusBadRequest, "验证失败", "链接无效或已过期.")
		return
	}
	userID, err := a.store.ConsumeEmailToken(r.Context(), store.EmailTokenVerify, auth.HashEmailToken(token))
	if errors.Is(err, store.ErrNotFound) {
		a.renderMessage(w, r, http.StatusBadRequest, "验证失败", "链接无效或已过期.")
		return
	}
	if err != nil {
		slog.Error("consume verify token failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "验证失败", "暂时无法验证邮箱.")
		return
	}
	activated, err := a.store.CompleteEmailVerification(r.Context(), userID)
	if err != nil {
		slog.Error("complete email verification failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "验证失败", "暂时无法验证邮箱.")
		return
	}
	detail := ""
	if activated {
		detail = "signup"
	}
	_ = a.store.LogAudit(r.Context(), userID, "user.email_verify", "", detail)
	if session := auth.SessionFromContext(r.Context()); session != nil && session.User.ID == userID {
		a.redirect(w, r, "/account?notice=email_verified", http.StatusSeeOther)
		return
	}
	a.redirect(w, r, "/login?notice=email_verified", http.StatusSeeOther)
}

func (a *App) handleAccountEmail(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" || !validEmail(email) || len(email) > 320 {
		a.renderMessage(w, r, http.StatusBadRequest, "保存失败", "邮箱格式不正确.")
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load account user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "保存失败", "暂时无法读取账号信息.")
		return
	}
	if user.PasswordHash != "" {
		limitKey := "password:" + strconv.FormatInt(user.ID, 10)
		if !a.auth.AllowLogin(limitKey) {
			a.renderMessage(w, r, http.StatusTooManyRequests, "保存失败", "尝试次数过多, 请稍后再试.")
			return
		}
		if !auth.VerifyPassword(user.PasswordHash, r.FormValue("password")) {
			a.auth.RecordLoginFailure(limitKey)
			a.renderMessage(w, r, http.StatusBadRequest, "保存失败", "当前密码不正确.")
			return
		}
		a.auth.RecordLoginSuccess(limitKey)
	}
	if strings.EqualFold(email, user.Email) {
		if user.EmailVerified {
			a.redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
	} else if err := a.store.UpdateEmail(r.Context(), user.ID, email); err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			a.renderMessage(w, r, http.StatusConflict, "保存失败", "邮箱已被其他账号使用.")
			return
		}
		slog.Error("update account email failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "保存失败", "暂时无法保存邮箱.")
		return
	}
	_ = a.store.LogAudit(r.Context(), user.ID, "user.email_change", user.Name, email)
	updated, err := a.store.UserByID(r.Context(), user.ID)
	if err == nil {
		user = updated
	}
	if err := a.sendVerifyEmail(r.Context(), user); err != nil {
		slog.Error("send verify email failed", "err", err, "user", user.ID)
		a.redirect(w, r, "/account?notice=email_send_failed", http.StatusSeeOther)
		return
	}
	a.redirect(w, r, "/account?notice=email_saved", http.StatusSeeOther)
}

func (a *App) handleAccountEmailResend(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.mailEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "邮件服务未配置.")
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load account user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "发送失败", "暂时无法读取账号信息.")
		return
	}
	if user.Email == "" || user.EmailVerified {
		a.redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	if !a.allowRate(r.Context(), "mail-verify-user:"+strconv.FormatInt(user.ID, 10), 3, 15*time.Minute) {
		a.renderMessage(w, r, http.StatusTooManyRequests, "发送失败", "请求太频繁, 请稍后再试.")
		return
	}
	if err := a.sendVerifyEmail(r.Context(), user); err != nil {
		slog.Error("send verify email failed", "err", err, "user", user.ID)
		a.redirect(w, r, "/account?notice=email_send_failed", http.StatusSeeOther)
		return
	}
	a.redirect(w, r, "/account?notice=email_verify_sent", http.StatusSeeOther)
}

func (a *App) sendVerifyEmail(ctx context.Context, user *store.User) error {
	return a.sendAccountMail(ctx, user, store.EmailTokenVerify, emailVerifyTTL, "verify-email", "mail_verify_subject", "mail_verify_body")
}

func (a *App) sendResetEmail(ctx context.Context, user *store.User) error {
	return a.sendAccountMail(ctx, user, store.EmailTokenReset, emailResetTTL, "reset", "mail_reset_subject", "mail_reset_body")
}

func (a *App) sendAccountMail(ctx context.Context, user *store.User, kind string, ttl time.Duration, path, subjectKey, bodyKey string) error {
	if a.mailer == nil || user.Email == "" || a.cfg.Site.BaseURL == "" {
		return errors.New("mail is not available")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	token, hash, err := auth.NewEmailToken()
	if err != nil {
		return err
	}
	if err := a.store.CreateEmailToken(ctx, user.ID, kind, hash, time.Now().Add(ttl)); err != nil {
		return err
	}
	lang := a.uiLang()
	link := a.cfg.Site.BaseURL + "/" + path + "?token=" + url.QueryEscape(token)
	duration := fmt.Sprintf(i18n.Text(lang, "mail_duration_minutes"), int(ttl.Minutes()))
	if ttl >= time.Hour {
		duration = fmt.Sprintf(i18n.Text(lang, "mail_duration_hours"), int(ttl.Hours()))
	}
	subject := i18n.Text(lang, subjectKey)
	body := fmt.Sprintf(i18n.Text(lang, bodyKey), user.Name, a.cfg.Site.Title, duration, link)
	return a.mailer.Send(ctx, user.Email, subject, body)
}

func validEmailToken(token string) bool {
	if len(token) < 32 || len(token) > 128 {
		return false
	}
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func (a *App) allowRate(ctx context.Context, key string, limit int, window time.Duration) bool {
	if a.site == nil {
		return true
	}
	allowed, err := a.site.Allow(ctx, key, limit, window)
	if err != nil {
		slog.Warn("rate limit check failed, using local fallback", "key", key, "err", err)
		if a.limitFallback == nil {
			return true
		}
		allowed, err = a.limitFallback.Allow(ctx, key, limit, window)
		if err != nil {
			return true
		}
	}
	return allowed
}
