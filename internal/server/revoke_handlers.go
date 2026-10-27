package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type adminRevokedData struct {
	baseData
	Records []*store.RevokedRecord
}

func (a *App) handleAdminRevoked(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	records, err := a.store.ListRevokedRecords(r.Context(), 200)
	if err != nil {
		slog.Error("list revoked records failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取已撤销记录.")
		return
	}
	a.renderUI(w, r, http.StatusOK, "admin_revoked", adminRevokedData{
		baseData: a.baseData(r, "已撤销记录"),
		Records:  records,
	})
}

func (a *App) handleAdminRevokedRestore(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	id := strings.TrimSpace(r.FormValue("id"))
	if kind == "" || id == "" || len(id) > 128 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "撤销记录编号不正确.")
		return
	}
	if err := a.store.RestoreRevokedRecord(r.Context(), kind, id, session.User.ID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.renderMessage(w, r, http.StatusNotFound, "记录不存在", "没有找到这条已撤销记录.")
		case errors.Is(err, store.ErrEditConflict):
			a.renderMessage(w, r, http.StatusConflict, "恢复失败", "同名记录已经存在.")
		default:
			slog.Error("restore revoked record failed", "err", err, "kind", kind, "id", id)
			a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法恢复这条记录.")
		}
		return
	}
	switch kind {
	case "entity", "entity_type", "entity_field":
		a.reloadEntities(r.Context())
	}
	a.redirect(w, r, "/admin/revoked?notice=revoked_restored", http.StatusSeeOther)
}
