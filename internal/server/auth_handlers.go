package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/i18n"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if session := auth.SessionFromContext(r.Context()); session != nil {
		a.redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, r, r.URL.Query().Get("next"), "", http.StatusOK)
}

func (a *App) renderLogin(w http.ResponseWriter, r *http.Request, next, message string, status int) {
	count, err := a.store.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "登录失败", "暂时无法读取用户信息.")
		return
	}
	data := loginData{
		baseData:         a.baseData(r, "登录"),
		Next:             safeNext(next),
		HasUsers:         count > 0,
		MailEnabled:      a.mailEnabled(),
		RegistrationMode: a.registrationMode(),
	}
	data.CSRF = a.auth.LoginCSRF(w, r)
	data.Error = i18n.Message(a.uiLang(), message)
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, status, "login", data)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		a.renderLogin(w, r, "", "登录请求格式不正确.", http.StatusBadRequest)
		return
	}
	next := safeNext(r.FormValue("next"))
	if !a.auth.CheckLoginCSRF(r) {
		a.renderLogin(w, r, next, "登录校验失败, 请重试.", http.StatusForbidden)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || len(username) > 100 || password == "" || len(password) > 128 {
		a.renderLogin(w, r, next, "用户名或密码错误.", http.StatusUnauthorized)
		return
	}
	limitKey := strings.ToLower(username) + "|" + a.auth.ClientIP(r)
	if !a.auth.AllowLogin(limitKey) {
		a.renderLogin(w, r, next, "尝试次数过多, 请稍后再试.", http.StatusTooManyRequests)
		return
	}

	user, err := a.store.UserByName(r.Context(), username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		slog.Error("load user failed", "err", err)
		a.renderLogin(w, r, next, "登录失败, 请稍后再试.", http.StatusInternalServerError)
		return
	}
	if !auth.VerifyLogin(user, password) || user.Disabled {
		a.auth.RecordLoginFailure(limitKey)
		a.renderLogin(w, r, next, "用户名或密码错误.", http.StatusUnauthorized)
		return
	}
	if user.TOTPEnabled {
		totpKey := "totp|" + strconv.FormatInt(user.ID, 10)
		if !a.auth.AllowLogin(totpKey) {
			a.renderLogin(w, r, next, "验证尝试过多, 请稍后再试.", http.StatusTooManyRequests)
			return
		}
		code := r.FormValue("totp_code")
		recoveryCode := r.FormValue("recovery_code")
		if !a.checkSecondFactor(r.Context(), user, code, recoveryCode) {
			a.auth.RecordLoginFailure(limitKey)
			a.auth.RecordLoginFailure(totpKey)
			message := "请输入两步验证码或恢复码."
			if strings.TrimSpace(code) != "" || strings.TrimSpace(recoveryCode) != "" {
				message = "验证码或恢复码不正确."
			}
			a.renderLogin(w, r, next, message, http.StatusUnauthorized)
			return
		}
	}
	a.auth.RecordLoginSuccess(limitKey)

	if _, err := a.auth.CreateSession(r.Context(), w, r, user); err != nil {
		slog.Error("create session failed", "err", err)
		a.renderLogin(w, r, next, "登录失败, 请稍后再试.", http.StatusInternalServerError)
		return
	}
	_ = a.store.LogAudit(r.Context(), user.ID, "auth.login", "local", "")
	a.redirect(w, r, next, http.StatusSeeOther)
}

func (a *App) handleVerifyPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.auth.ReadTOTPTicket(r); !ok {
		a.renderLogin(w, r, "/", "请先登录.", http.StatusUnauthorized)
		return
	}
	data := verifyData{baseData: a.baseData(r, "两步验证")}
	data.CSRF = a.auth.LoginCSRF(w, r)
	a.renderUI(w, r, http.StatusOK, "verify", data)
}

func (a *App) handleVerify(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "验证失败", "请求格式不正确.")
		return
	}
	if !a.auth.CheckLoginCSRF(r) {
		a.renderMessage(w, r, http.StatusForbidden, "验证失败", "请求校验失败.")
		return
	}
	userID, ok := a.auth.ReadTOTPTicket(r)
	if !ok {
		a.renderLogin(w, r, "/", "验证已过期, 请重新登录.", http.StatusUnauthorized)
		return
	}
	limitKey := "totp|" + strconv.FormatInt(userID, 10)
	if !a.auth.AllowLogin(limitKey) {
		a.renderLogin(w, r, "/", "验证尝试过多, 请稍后再试.", http.StatusTooManyRequests)
		return
	}
	user, err := a.store.UserByID(r.Context(), userID)
	if err != nil || user.Disabled || !user.TOTPEnabled {
		a.auth.RecordLoginFailure(limitKey)
		a.renderLogin(w, r, "/", "验证已过期, 请重新登录.", http.StatusUnauthorized)
		return
	}
	if !a.checkSecondFactor(r.Context(), user, r.FormValue("totp_code"), r.FormValue("recovery_code")) {
		a.auth.RecordLoginFailure(limitKey)
		a.renderLogin(w, r, "/", "验证码或恢复码不正确.", http.StatusUnauthorized)
		return
	}
	a.auth.RecordLoginSuccess(limitKey)
	a.auth.ClearTOTPTicket(w)
	if _, err := a.auth.CreateSession(r.Context(), w, r, user); err != nil {
		slog.Error("create verified session failed", "err", err)
		a.renderLogin(w, r, "/", "登录失败, 请稍后再试.", http.StatusInternalServerError)
		return
	}
	_ = a.store.LogAudit(r.Context(), user.ID, "auth.login", "github", "totp")
	a.redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		a.redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "退出失败", "请求校验失败.")
		return
	}
	_ = a.store.LogAudit(r.Context(), session.User.ID, "auth.logout", "local", "")
	a.auth.Logout(r.Context(), w, r)
	a.redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	if !a.auth.GitHubEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "GitHub 登录未配置.")
		return
	}
	state, err := a.auth.NewOAuthState(w)
	if err != nil {
		a.renderLogin(w, r, "/", "GitHub 登录初始化失败.", http.StatusInternalServerError)
		return
	}
	a.redirect(w, r, a.auth.GitHubAuthURL(state), http.StatusFound)
}

func (a *App) handleGitHubLink(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, ""); !ok {
		return
	}
	if !a.auth.GitHubEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "GitHub 登录未配置.")
		return
	}
	state, err := a.auth.NewLinkOAuthState(w)
	if err != nil {
		a.renderMessage(w, r, http.StatusInternalServerError, "绑定失败", "无法初始化 GitHub 授权.")
		return
	}
	a.redirect(w, r, a.auth.GitHubAuthURL(state), http.StatusFound)
}

func (a *App) handleAccount(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	data, err := a.buildAccountData(r, session)
	if err != nil {
		slog.Error("load account data failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取账号信息.")
		return
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "account", data)
}

func (a *App) buildAccountData(r *http.Request, session *store.SessionData) (accountData, error) {
	identities, err := a.store.ListIdentities(r.Context(), session.User.ID)
	if err != nil {
		return accountData{}, err
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		return accountData{}, err
	}
	hasGitHub := false
	for _, identity := range identities {
		if identity.Provider == "github" {
			hasGitHub = true
			break
		}
	}
	count, err := a.store.CountRecoveryCodes(r.Context(), session.User.ID)
	if err != nil {
		return accountData{}, err
	}
	return accountData{
		baseData:         a.baseData(r, "账号"),
		Identities:       identities,
		HasGitHub:        hasGitHub,
		HasPassword:      user.PasswordHash != "",
		EmailVerified:    user.EmailVerified,
		MailEnabled:      a.mailEnabled(),
		TOTPEnabled:      user.TOTPEnabled,
		TOTPSecret:       user.TOTPSecret,
		OTPAuthURL:       auth.OTPAuthURL(a.totpIssuer(), user.Name, user.TOTPSecret),
		RecoveryCodes:    count,
		NewRecoveryCodes: nil,
	}, nil
}

const totpSetupWindow = 30 * time.Minute

func (a *App) parseGuard(w http.ResponseWriter, r *http.Request, session *store.SessionData) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	err := r.ParseForm()
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			a.renderMessage(w, r, http.StatusRequestEntityTooLarge, "操作失败", "提交内容过大.")
			return false
		}
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return false
	}
	if !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return false
	}
	return true
}

func (a *App) checkSecondFactor(ctx context.Context, user *store.User, totpCode, recoveryCode string) bool {
	code := strings.TrimSpace(totpCode)
	if code != "" {
		if step, valid := auth.ValidateTOTP(user.TOTPSecret, code); valid && a.consumeTOTPCode(ctx, user, step) {
			return true
		}
	}
	normalized := auth.NormalizeRecoveryCode(recoveryCode)
	if normalized == "" {
		return false
	}
	used, err := a.store.ConsumeRecoveryCode(ctx, user.ID, auth.HashRecoveryCode(normalized))
	if err != nil {
		slog.Error("consume recovery code failed", "err", err, "user", user.ID)
		return false
	}
	if !used {
		return false
	}
	_ = a.store.LogAudit(ctx, user.ID, "user.recovery_code_use", user.Name, "")
	return true
}

func (a *App) consumeTOTPCode(ctx context.Context, user *store.User, step int64) bool {
	fresh, err := a.store.ConsumeTOTP(ctx, user.ID, step)
	if err != nil {
		slog.Error("consume totp code failed", "err", err, "user", user.ID)
		return false
	}
	return fresh
}

func (a *App) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load totp setup user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法读取账号信息.")
		return
	}
	if user.TOTPEnabled {
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "两步验证已开启, 请先关闭后再生成新密钥.")
		return
	}
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		slog.Error("generate totp secret failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法开启两步验证.")
		return
	}
	if err := a.store.SetTOTPSecret(r.Context(), user.ID, secret); err != nil {
		slog.Error("set totp secret failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法开启两步验证.")
		return
	}
	_ = a.store.LogAudit(r.Context(), user.ID, "user.totp_setup", user.Name, "")
	a.redirect(w, r, "/account?notice=totp_setup", http.StatusSeeOther)
}

func (a *App) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load totp user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法读取账号信息.")
		return
	}
	if user.TOTPEnabled {
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "两步验证已经开启.")
		return
	}
	if user.TOTPSecret == "" {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "请先生成两步验证密钥.")
		return
	}
	if time.Since(user.TOTPSecretSetAt) > totpSetupWindow {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "两步验证密钥已过期, 请重新生成.")
		return
	}
	step, valid := auth.ValidateTOTP(user.TOTPSecret, r.FormValue("totp_code"))
	if !valid {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "两步验证码不正确或已过期.")
		return
	}
	if !a.consumeTOTPCode(r.Context(), user, step) {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "这个验证码已经用过, 请输入新的验证码.")
		return
	}
	if err := a.store.EnableTOTP(r.Context(), user.ID); err != nil {
		slog.Error("enable totp failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法开启两步验证.")
		return
	}
	if _, err := a.auth.CreateSession(r.Context(), w, r, user); err != nil {
		slog.Error("create session after totp enable failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "两步验证已开启, 请重新登录.")
		return
	}
	_ = a.store.LogAudit(r.Context(), user.ID, "user.totp_enable", user.Name, "")
	a.redirect(w, r, "/account?notice=totp_enabled", http.StatusSeeOther)
}

func (a *App) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load totp user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法读取账号信息.")
		return
	}
	if !user.TOTPEnabled {
		a.redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	if user.PasswordHash != "" {
		if !auth.VerifyPassword(user.PasswordHash, r.FormValue("password")) {
			a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "当前密码不正确.")
			return
		}
	} else if !a.checkSecondFactor(r.Context(), user, r.FormValue("totp_code"), r.FormValue("recovery_code")) {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "验证码或恢复码不正确.")
		return
	}
	if err := a.store.DisableTOTP(r.Context(), user.ID); err != nil {
		slog.Error("disable totp failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法关闭两步验证.")
		return
	}
	if _, err := a.auth.CreateSession(r.Context(), w, r, user); err != nil {
		slog.Error("create session after totp disable failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "两步验证已关闭, 请重新登录.")
		return
	}
	_ = a.store.LogAudit(r.Context(), user.ID, "user.totp_disable", user.Name, "")
	a.redirect(w, r, "/account?notice=totp_disabled", http.StatusSeeOther)
}

func (a *App) handleRecoveryGenerate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load recovery user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法读取账号信息.")
		return
	}
	if !user.TOTPEnabled {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "请先开启两步验证.")
		return
	}
	if user.PasswordHash != "" {
		limitKey := "password:" + strconv.FormatInt(user.ID, 10)
		if !a.auth.AllowLogin(limitKey) {
			a.renderMessage(w, r, http.StatusTooManyRequests, "操作失败", "尝试次数过多, 请稍后再试.")
			return
		}
		if !auth.VerifyPassword(user.PasswordHash, r.FormValue("password")) {
			a.auth.RecordLoginFailure(limitKey)
			a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "当前密码不正确.")
			return
		}
		a.auth.RecordLoginSuccess(limitKey)
	} else if !a.checkSecondFactor(r.Context(), user, r.FormValue("totp_code"), r.FormValue("recovery_code")) {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "验证码或恢复码不正确.")
		return
	}
	codes, err := auth.GenerateRecoveryCodes(auth.RecoveryCodeCount)
	if err != nil {
		slog.Error("generate recovery codes failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法生成恢复码.")
		return
	}
	hashes := make([]string, 0, len(codes))
	formatted := make([]string, 0, len(codes))
	for _, code := range codes {
		hashes = append(hashes, auth.HashRecoveryCode(code))
		formatted = append(formatted, auth.FormatRecoveryCode(code))
	}
	if err := a.store.ReplaceRecoveryCodes(r.Context(), user.ID, hashes, user.ID); err != nil {
		slog.Error("replace recovery codes failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法生成恢复码.")
		return
	}
	data, err := a.buildAccountData(r, session)
	if err != nil {
		slog.Error("load account after recovery generate failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "恢复码已生成, 请重新打开账号页查看.")
		return
	}
	data.NewRecoveryCodes = formatted
	data.Notice = a.noticeText("recovery_codes_generated")
	a.renderUI(w, r, http.StatusOK, "account", data)
}

func (a *App) handleTOTPQR(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil || user.TOTPSecret == "" || user.TOTPEnabled {
		http.NotFound(w, r)
		return
	}
	png, err := qrcode.Encode(auth.OTPAuthURL(a.totpIssuer(), user.Name, user.TOTPSecret), qrcode.Medium, 240)
	if err != nil {
		slog.Error("generate totp qr failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(png)
}

func (a *App) handleAccountPassword(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "修改失败", "请求校验失败.")
		return
	}
	current := r.FormValue("current_password")
	password := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	switch {
	case utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 128:
		a.renderMessage(w, r, http.StatusBadRequest, "修改失败", "密码长度必须为 8 到 128 个字符.")
		return
	case password != confirm:
		a.renderMessage(w, r, http.StatusBadRequest, "修改失败", "两次输入的新密码不一致.")
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load password user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "修改失败", "暂时无法读取账号信息.")
		return
	}
	if user.PasswordHash != "" {
		limitKey := "password:" + strconv.FormatInt(user.ID, 10)
		if !a.auth.AllowLogin(limitKey) {
			a.renderMessage(w, r, http.StatusTooManyRequests, "修改失败", "尝试次数过多, 请稍后再试.")
			return
		}
		if !auth.VerifyPassword(user.PasswordHash, current) {
			a.auth.RecordLoginFailure(limitKey)
			a.renderMessage(w, r, http.StatusBadRequest, "修改失败", "当前密码不正确.")
			return
		}
		a.auth.RecordLoginSuccess(limitKey)
		if current == password {
			a.renderMessage(w, r, http.StatusBadRequest, "修改失败", "新密码不能和当前密码相同.")
			return
		}
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		slog.Error("hash new password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "修改失败", "暂时无法修改密码.")
		return
	}
	if err := a.store.UpdatePassword(r.Context(), user.ID, hash, "user.password_change", user.ID); err != nil {
		slog.Error("update password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "修改失败", "暂时无法修改密码.")
		return
	}
	if _, err := a.auth.CreateSession(r.Context(), w, r, user); err != nil {
		slog.Error("create session after password change failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "密码已修改", "密码已修改, 请重新登录.")
		return
	}
	a.redirect(w, r, "/account?notice=password_changed", http.StatusSeeOther)
}

func (a *App) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if !a.auth.GitHubEnabled() {
		a.renderMessage(w, r, http.StatusNotFound, "未启用", "GitHub 登录未配置.")
		return
	}
	kind, ok := a.auth.CheckOAuthState(w, r)
	if !ok {
		a.renderLogin(w, r, "/", "GitHub 登录状态校验失败, 请重试.", http.StatusBadRequest)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		a.renderLogin(w, r, "/", "GitHub 未返回授权码.", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	profile, err := a.auth.CompleteGitHub(ctx, code)
	if err != nil {
		slog.Error("github oauth failed", "err", err)
		a.renderLogin(w, r, "/", "GitHub 登录失败, 请稍后再试.", http.StatusBadGateway)
		return
	}

	if kind == "link" {
		session := auth.SessionFromContext(r.Context())
		if session == nil {
			a.renderLogin(w, r, "/", "请先登录后再绑定 GitHub.", http.StatusUnauthorized)
			return
		}
		if err := a.store.LinkIdentity(ctx, session.User.ID, "github", profile.Subject, profile.Login, profile.Email); err != nil {
			if errors.Is(err, store.ErrIdentityTaken) {
				a.renderMessage(w, r, http.StatusConflict, "绑定失败", "这个 GitHub 身份已经绑定到其他账号.")
				return
			}
			slog.Error("link github identity failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "绑定失败", "暂时无法绑定 GitHub 账号.")
			return
		}
		_ = a.store.LogAudit(ctx, session.User.ID, "identity.link", "github", profile.Login)
		a.redirect(w, r, "/account?notice=linked", http.StatusSeeOther)
		return
	}

	user, err := a.store.FindOrCreateGitHubUser(ctx, profile.Subject, profile.Login, profile.Email)
	if err != nil {
		slog.Error("github user failed", "err", err)
		a.renderLogin(w, r, "/", "GitHub 账号创建失败.", http.StatusInternalServerError)
		return
	}
	if user.Disabled {
		a.renderLogin(w, r, "/", "账号已被禁用.", http.StatusForbidden)
		return
	}
	if user.TOTPEnabled {
		if err := a.auth.IssueTOTPTicket(w, user.ID); err != nil {
			slog.Error("issue totp ticket failed", "err", err)
			a.renderLogin(w, r, "/", "登录失败, 请稍后再试.", http.StatusInternalServerError)
			return
		}
		a.redirect(w, r, "/verify", http.StatusSeeOther)
		return
	}
	if _, err := a.auth.CreateSession(ctx, w, r, user); err != nil {
		slog.Error("create github session failed", "err", err)
		a.renderLogin(w, r, "/", "登录失败, 请稍后再试.", http.StatusInternalServerError)
		return
	}
	_ = a.store.LogAudit(ctx, user.ID, "auth.login", "github", "")
	a.redirect(w, r, "/", http.StatusSeeOther)
}
