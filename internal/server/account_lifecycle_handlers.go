package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	user, err := a.store.UserByID(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("load account delete user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "删除失败", "暂时无法删除账号.")
		return
	}
	if user.PasswordHash != "" {
		limitKey := "password:" + strconv.FormatInt(user.ID, 10)
		if !a.auth.AllowLogin(limitKey) {
			a.renderMessage(w, r, http.StatusTooManyRequests, "删除失败", "尝试次数过多, 请稍后再试.")
			return
		}
		if !auth.VerifyPassword(user.PasswordHash, r.FormValue("password")) {
			a.auth.RecordLoginFailure(limitKey)
			a.renderMessage(w, r, http.StatusBadRequest, "删除失败", "当前密码不正确.")
			return
		}
		a.auth.RecordLoginSuccess(limitKey)
	} else if !a.checkSecondFactor(r.Context(), user, r.FormValue("totp_code"), r.FormValue("recovery_code")) {
		a.renderMessage(w, r, http.StatusBadRequest, "删除失败", "验证码或恢复码不正确.")
		return
	}
	if r.FormValue("confirm") != "1" {
		a.renderMessage(w, r, http.StatusBadRequest, "删除失败", "请勾选确认删除账号.")
		return
	}
	if _, err := a.store.DeleteUser(r.Context(), user.ID, user.ID, "self"); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			a.renderMessage(w, r, http.StatusConflict, "删除失败", "必须保留至少一个启用的管理员.")
			return
		}
		if errors.Is(err, store.ErrRevokeDisabled) {
			a.renderDeleteDisabled(w, r)
			return
		}
		slog.Error("delete own account failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "删除失败", "暂时无法删除账号.")
		return
	}
	a.auth.ClearSessionCookie(w)
	a.redirect(w, r, "/login?notice=account_deleted", http.StatusSeeOther)
}

func (a *App) handleAccountExport(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, "")
	if !ok {
		return
	}
	data, err := a.store.ExportUserData(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("export account data failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "导出失败", "暂时无法导出账号数据.")
		return
	}
	filename := fmt.Sprintf("account-%d-%s.json", session.User.ID, time.Now().Format("20060102"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		slog.Error("write account export failed", "err", err)
	}
}

func (a *App) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户编号不正确.")
		return
	}
	if id == session.User.ID {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "不能通过后台删除当前登录账号.")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	switch {
	case reason == "":
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "删除原因不能为空.")
		return
	case utf8.RuneCountInString(reason) > 300:
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "删除原因不能超过 300 个字符.")
		return
	}
	if _, err := a.store.DeleteUser(r.Context(), id, session.User.ID, reason); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.renderMessage(w, r, http.StatusNotFound, "用户不存在", "没有找到这个用户.")
		case errors.Is(err, store.ErrLastAdmin):
			a.renderMessage(w, r, http.StatusConflict, "操作失败", "必须保留至少一个启用的管理员.")
		case errors.Is(err, store.ErrRevokeDisabled):
			a.renderDeleteDisabled(w, r)
		default:
			slog.Error("delete user failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法删除用户.")
		}
		return
	}
	a.redirect(w, r, "/admin/users?notice=user_deleted", http.StatusSeeOther)
}
