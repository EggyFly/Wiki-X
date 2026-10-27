package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleAdminPages(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightReview); !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "all", "active", "deleted":
	default:
		status = "all"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}
	page, size := adminPage(r, 50)
	pages, total, err := a.store.ListAdminPages(r.Context(), status, query, size, (page-1)*size)
	if err != nil {
		slog.Error("list admin pages failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取页面列表.")
		return
	}
	data := adminPagesData{
		baseData: a.baseData(r, "页面管理"),
		Pages:    pages,
		Status:   status,
		Query:    query,
		Pager:    newPager(page, totalPages(total, size), size),
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_pages", data)
}

func (a *App) handleAdminPageDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "路由不合法.")
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	switch {
	case reason == "":
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "删除原因不能为空.")
		return
	case utf8.RuneCountInString(reason) > 500:
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "删除原因不能超过 500 个字符.")
		return
	}
	if err := a.store.DeletePage(r.Context(), route, reason, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面, 或者它已经被删除.")
			return
		}
		if errors.Is(err, store.ErrRevokeDisabled) {
			a.renderDeleteDisabled(w, r)
			return
		}
		slog.Error("delete page failed", "err", err, "route", route)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法删除页面.")
		return
	}
	a.removePage(r.Context(), route)
	a.invalidateRoute(r.Context(), route)
	a.syncSearchDocument(r.Context(), route)
	a.redirect(w, r, "/admin/pages?status=all&notice=page_deleted", http.StatusSeeOther)
}

func (a *App) handleAdminPageRename(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	oldRoute := strings.TrimSpace(r.FormValue("route"))
	newRoute := strings.TrimSpace(r.FormValue("new_route"))
	if err := site.ValidateRoute(oldRoute); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "原路由不合法.")
		return
	}
	if err := site.ValidateRoute(newRoute); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "新路由必须以 / 开头和结尾, 并且不能使用系统保留路径.")
		return
	}
	if strings.EqualFold(oldRoute, newRoute) {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "新旧路由不能相同.")
		return
	}
	if err := a.store.RenamePage(r.Context(), oldRoute, newRoute, session.User.ID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面, 或者它已经被删除.")
		case errors.Is(err, store.ErrRouteExists):
			a.renderMessage(w, r, http.StatusConflict, "移动失败", "新路由已经被其他页面占用, 大小写不同的同名路由也算冲突.")
		default:
			slog.Error("rename page failed", "err", err, "route", oldRoute, "new_route", newRoute)
			a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法移动页面.")
		}
		return
	}
	a.removePage(r.Context(), oldRoute)
	a.reloadPage(r.Context(), newRoute)
	a.invalidateRoute(r.Context(), oldRoute)
	a.invalidateRoute(r.Context(), newRoute)
	a.syncSearchDocument(r.Context(), oldRoute)
	a.syncSearchDocument(r.Context(), newRoute)
	a.redirect(w, r, "/admin/pages?status=all&notice=page_renamed", http.StatusSeeOther)
}

func (a *App) handleAdminPageRestore(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightReview)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	route := strings.TrimSpace(r.FormValue("route"))
	if err := site.ValidateRoute(route); err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "路由不合法.")
		return
	}
	if err := a.store.RestorePage(r.Context(), route, session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "页面不存在", "没有找到这个页面, 或者它没有被删除.")
			return
		}
		if errors.Is(err, store.ErrEditConflict) {
			a.renderMessage(w, r, http.StatusConflict, "恢复失败", "这个路由已经被另一个活动页面占用.")
			return
		}
		slog.Error("restore page failed", "err", err, "route", route)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法恢复页面.")
		return
	}
	a.reloadPage(r.Context(), route)
	a.invalidateRoute(r.Context(), route)
	a.syncSearchDocument(r.Context(), route)
	a.redirect(w, r, "/admin/pages?status=all&notice=page_restored", http.StatusSeeOther)
}
