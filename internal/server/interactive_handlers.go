package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/extension"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleAdminInteractive(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	page, size := adminPage(r, 50)
	nodes, total, err := a.store.ListInteractiveNodes(r.Context(), size, (page-1)*size)
	if err != nil {
		slog.Error("list interactive nodes failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取互动节点.")
		return
	}
	data := adminInteractiveData{
		baseData: a.baseData(r, "互动节点"),
		Nodes:    nodes,
		Kinds:    extension.Kinds(),
		Pager:    newPager(page, totalPages(total, size), size),
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	if editID, err := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64); err == nil && editID > 0 {
		edit, err := a.store.GetInteractiveNodeAny(r.Context(), editID)
		if err != nil {
			a.renderMessage(w, r, http.StatusNotFound, "不存在", "没有找到这个互动节点.")
			return
		}
		data.Edit = edit
		encoded, _ := json.MarshalIndent(edit.Config, "", "  ")
		data.ConfigJSON = string(encoded)
	}
	if data.ConfigJSON == "" {
		data.ConfigJSON = `{"kind":"card"}`
	}
	a.renderUI(w, r, http.StatusOK, "admin_interactive", data)
}

func (a *App) handleAdminInteractiveSave(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	config := map[string]string{}
	rawConfig := strings.TrimSpace(r.FormValue("config"))
	if rawConfig != "" {
		if err := json.Unmarshal([]byte(rawConfig), &config); err != nil {
			a.renderInteractiveError(w, r, "配置必须是 JSON 对象, 值必须是字符串.")
			return
		}
	}
	input := store.InteractiveInput{
		ID:          id,
		NodeType:    r.FormValue("node_type"),
		Key:         r.FormValue("key"),
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Config:      config,
		Enabled:     r.FormValue("enabled") == "1",
		ActorID:     session.User.ID,
	}
	if _, err := a.store.SaveInteractiveNode(r.Context(), input); err != nil {
		switch {
		case errors.Is(err, store.ErrBadInteractive):
			a.renderInteractiveError(w, r, "互动节点的类型、标识、标题或配置不合法.")
		case errors.Is(err, store.ErrInteractiveExists):
			a.renderInteractiveError(w, r, "同类型下已经有这个标识.")
		case errors.Is(err, store.ErrNotFound):
			a.renderMessage(w, r, http.StatusNotFound, "不存在", "没有找到这个互动节点.")
		default:
			slog.Error("save interactive node failed", "err", err)
			a.renderInteractiveError(w, r, "暂时无法保存互动节点.")
		}
		return
	}
	a.redirect(w, r, "/admin/interactive?notice=interactive_saved", http.StatusSeeOther)
}

func (a *App) handleAdminInteractiveDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "互动节点编号不正确.")
		return
	}
	if err := a.store.DeleteInteractiveNode(r.Context(), id, session.User.ID); err != nil {
		if errors.Is(err, store.ErrRevokeDisabled) {
			a.renderDeleteDisabled(w, r)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "不存在", "没有找到这个互动节点.")
			return
		}
		slog.Error("delete interactive node failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法删除互动节点.")
		return
	}
	a.redirect(w, r, "/admin/interactive?notice=interactive_deleted", http.StatusSeeOther)
}

func (a *App) renderInteractiveError(w http.ResponseWriter, r *http.Request, message string) {
	page, size := adminPage(r, 50)
	nodes, total, _ := a.store.ListInteractiveNodes(r.Context(), size, (page-1)*size)
	data := adminInteractiveData{
		baseData:   a.baseData(r, "互动节点"),
		Nodes:      nodes,
		Kinds:      extension.Kinds(),
		Pager:      newPager(page, totalPages(total, size), size),
		Error:      message,
		ConfigJSON: strings.TrimSpace(r.FormValue("config")),
	}
	data.Edit = &store.InteractiveNode{
		ID:          parseIntDefault(r.FormValue("id")),
		NodeType:    r.FormValue("node_type"),
		Key:         r.FormValue("key"),
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Enabled:     r.FormValue("enabled") == "1",
	}
	a.renderUI(w, r, http.StatusBadRequest, "admin_interactive", data)
}

func (a *App) handleInteractivePage(w http.ResponseWriter, r *http.Request, nodeType string) {
	key := strings.TrimSpace(r.PathValue("key"))
	node, err := a.store.GetInteractiveNode(r.Context(), nodeType, key)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("get interactive node failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	data := interactiveData{
		baseData: a.baseData(r, node.Title),
		Node:     node,
		Rendered: extension.Render(extension.Node{
			Type: node.NodeType, Key: node.Key, Title: node.Title,
			Description: node.Description, Config: node.Config,
		}),
	}
	a.renderUI(w, r, http.StatusOK, "interactive", data)
}

func (a *App) handleWidgetPage(w http.ResponseWriter, r *http.Request) {
	a.handleInteractivePage(w, r, "widget")
}

func (a *App) handleGamePage(w http.ResponseWriter, r *http.Request) {
	a.handleInteractivePage(w, r, "game")
}

func parseIntDefault(value string) int64 {
	id, _ := strconv.ParseInt(value, 10, 64)
	return id
}
