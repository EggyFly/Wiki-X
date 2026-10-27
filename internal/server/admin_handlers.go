package server

import (
	"encoding/base64"
	"encoding/csv"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	page, size := adminPage(r, 50)
	total, err := a.store.CountUsers(r.Context())
	if err != nil {
		slog.Error("count users failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取用户列表.")
		return
	}
	pager := newPager(page, totalPages(total, size), size)
	users, err := a.store.ListUsers(r.Context(), size, (pager.Page-1)*size)
	if err != nil {
		slog.Error("list users failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取用户列表.")
		return
	}
	groups, err := a.store.ListGroups(r.Context())
	if err != nil {
		slog.Error("list groups failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取用户组.")
		return
	}
	rows := make([]adminUserRow, 0, len(users))
	for _, user := range users {
		row := adminUserRow{User: user, IsSelf: user.ID == session.User.ID}
		for _, group := range groups {
			row.Choices = append(row.Choices, groupChoice{
				Name:     group.Name,
				Selected: slices.Contains(user.Groups, group.Name),
			})
		}
		rows = append(rows, row)
	}
	data := adminUsersData{
		baseData: a.baseData(r, "用户管理"),
		Users:    rows,
		Pager:    pager,
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_users", data)
}

func (a *App) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	switch {
	case name == "":
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "用户名不能为空.")
		return
	case utf8.RuneCountInString(name) > 64:
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "用户名不能超过 64 个字符.")
		return
	case strings.ContainsFunc(name, unicode.IsControl):
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "用户名不合法.")
		return
	case email != "" && !validEmail(email):
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "邮箱格式不正确.")
		return
	case utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 128:
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "密码长度必须为 8 到 128 个字符.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		slog.Error("hash user password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "创建失败", "暂时无法创建用户.")
		return
	}
	user, err := a.store.CreateUser(r.Context(), name, email, hash, false)
	if err != nil {
		if errors.Is(err, store.ErrUserExists) {
			a.renderMessage(w, r, http.StatusConflict, "创建失败", "用户名已存在.")
			return
		}
		if errors.Is(err, store.ErrEmailExists) {
			a.renderMessage(w, r, http.StatusConflict, "创建失败", "邮箱已被其他账号使用.")
			return
		}
		slog.Error("create user failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "创建失败", "暂时无法创建用户.")
		return
	}
	if a.mailEnabled() && user.Email != "" {
		if err := a.sendVerifyEmail(r.Context(), user); err != nil {
			slog.Error("send verify email for new user failed", "err", err, "user", user.ID)
		}
	}
	_ = a.store.LogAudit(r.Context(), session.User.ID, "user.create", name, "")
	a.redirect(w, r, "/admin/users?notice=user_created", http.StatusSeeOther)
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value
}

func (a *App) handleAdminAuditExport(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	filter, _, _, err := auditFilter(r)
	if err != nil {
		http.Error(w, "invalid date", http.StatusBadRequest)
		return
	}
	logs, err := a.store.ListAuditLogs(r.Context(), filter, 5001, 0)
	if err != nil {
		slog.Error("export audit logs failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if len(logs) > 5000 {
		logs = logs[:5000]
		w.Header().Set("X-Result-Truncated", "true")
		w.Header().Set("X-Result-Limit", "5000")
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-`+time.Now().Format("20060102")+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"time", "actor", "action", "target", "detail"})
	for _, entry := range logs {
		_ = writer.Write([]string{
			entry.CreatedAt.Format("2006-01-02 15:04:05"),
			csvSafe(entry.ActorName),
			csvSafe(entry.Action),
			csvSafe(entry.Target),
			csvSafe(entry.Detail),
		})
	}
	writer.Flush()
}

func csvSafe(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func (a *App) handleAdminGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	groups, err := a.store.ListGroups(r.Context())
	if err != nil {
		slog.Error("list groups failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取用户组.")
		return
	}
	rights := store.AllRights()
	rows := make([]adminGroupRow, 0, len(groups))
	for _, group := range groups {
		choices := make([]groupRightChoice, 0, len(rights))
		for _, right := range rights {
			choices = append(choices, groupRightChoice{Name: right, Selected: slices.Contains(group.Rights, right)})
		}
		rows = append(rows, adminGroupRow{Group: group, Rights: choices})
	}
	data := adminGroupsData{
		baseData: a.baseData(r, "用户组管理"),
		Groups:   rows,
		Rights:   rights,
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_groups", data)
}

func (a *App) handleAdminGroupCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderMessage(w, r, http.StatusForbidden, "操作失败", "请求校验失败.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	rights := r.PostForm["rights"]
	if len(rights) == 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "创建失败", "至少选择一个权限.")
		return
	}
	if err := a.store.CreateGroup(r.Context(), name, rights, session.User.ID); err != nil {
		a.renderGroupError(w, r, err, "暂时无法创建用户组.")
		return
	}
	a.redirect(w, r, "/admin/groups?notice=group_created", http.StatusSeeOther)
}

func (a *App) handleAdminGroupRights(w http.ResponseWriter, r *http.Request) {
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
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户组编号不正确.")
		return
	}
	rights := r.PostForm["rights"]
	if len(rights) == 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "至少选择一个权限.")
		return
	}
	if err := a.store.SetGroupRights(r.Context(), id, rights, session.User.ID); err != nil {
		a.renderGroupError(w, r, err, "暂时无法保存权限.")
		return
	}
	a.redirect(w, r, "/admin/groups?notice=group_rights_saved", http.StatusSeeOther)
}

func (a *App) handleAdminGroupDelete(w http.ResponseWriter, r *http.Request) {
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
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户组编号不正确.")
		return
	}
	if err := a.store.DeleteGroup(r.Context(), id, session.User.ID); err != nil {
		a.renderGroupError(w, r, err, "暂时无法删除用户组.")
		return
	}
	a.redirect(w, r, "/admin/groups?notice=group_deleted", http.StatusSeeOther)
}

func (a *App) renderGroupError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	switch {
	case errors.Is(err, store.ErrGroupExists):
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "用户组已存在.")
	case errors.Is(err, store.ErrBadGroup):
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户组名称不合法.")
	case errors.Is(err, store.ErrBadRight):
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "权限不合法.")
	case errors.Is(err, store.ErrBuiltinGroup):
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "内置用户组不能删除.")
	case errors.Is(err, store.ErrLastAdmin):
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "必须保留至少一个启用的管理员.")
	case errors.Is(err, store.ErrRevokeDisabled):
		a.renderDeleteDisabled(w, r)
	case errors.Is(err, store.ErrNotFound):
		a.renderMessage(w, r, http.StatusNotFound, "用户组不存在", "没有找到这个用户.")
	default:
		slog.Error("group operation failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", fallback)
	}
}

func (a *App) handleAdminUserPassword(w http.ResponseWriter, r *http.Request) {
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
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户编号不正确.")
		return
	}
	if id == session.User.ID {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "请使用账号页修改自己的密码.")
		return
	}
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	switch {
	case utf8.RuneCountInString(password) < 8 || utf8.RuneCountInString(password) > 128:
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "密码长度必须为 8 到 128 个字符.")
		return
	case password != confirm:
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "两次输入的新密码不一致.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		slog.Error("hash reset password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法重置密码.")
		return
	}
	if err := a.store.UpdatePassword(r.Context(), id, hash, "user.password_reset", session.User.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "用户不存在", "没有找到这个用户.")
			return
		}
		slog.Error("reset user password failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法重置密码.")
		return
	}
	a.redirect(w, r, "/admin/users?notice=password_reset", http.StatusSeeOther)
}

func (a *App) handleAdminUserTOTPDisable(w http.ResponseWriter, r *http.Request) {
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
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户编号不正确.")
		return
	}
	if id == session.User.ID {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "请在账号页关闭自己的两步验证.")
		return
	}
	target, err := a.store.UserByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "用户不存在", "没有找到这个用户.")
			return
		}
		slog.Error("load totp target failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法关闭两步验证.")
		return
	}
	if !target.TOTPEnabled {
		a.redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}
	if err := a.store.DisableTOTP(r.Context(), target.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "用户不存在", "没有找到这个用户.")
			return
		}
		slog.Error("disable user totp failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法关闭两步验证.")
		return
	}
	_ = a.store.LogAudit(r.Context(), session.User.ID, "user.totp_disable", target.Name, "admin")
	a.redirect(w, r, "/admin/users?notice=user_totp_disabled", http.StatusSeeOther)
}

func (a *App) handleAdminUserGroups(w http.ResponseWriter, r *http.Request) {
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
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户编号不正确.")
		return
	}
	groups := r.PostForm["groups"]
	if len(groups) == 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "至少选择一个用户组.")
		return
	}
	if id == session.User.ID {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "不能修改自己的用户组.")
		return
	}
	if err := a.store.SetUserGroups(r.Context(), id, groups, session.User.ID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.renderMessage(w, r, http.StatusNotFound, "用户不存在", "没有找到这个用户.")
		case errors.Is(err, store.ErrBadGroup):
			a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户组不合法.")
		case errors.Is(err, store.ErrLastAdmin):
			a.renderMessage(w, r, http.StatusConflict, "操作失败", "必须保留至少一个启用的管理员.")
		default:
			slog.Error("set user groups failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法保存用户组.")
		}
		return
	}
	a.redirect(w, r, "/admin/users?notice=groups_saved", http.StatusSeeOther)
}

func (a *App) handleAdminUserStatus(w http.ResponseWriter, r *http.Request) {
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
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "用户编号不正确.")
		return
	}
	if id == session.User.ID {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "不能修改当前登录账号的状态.")
		return
	}
	disabled := r.FormValue("disabled") == "1"
	if err := a.store.SetUserDisabled(r.Context(), id, disabled, session.User.ID); err != nil {
		switch {
		case errors.Is(err, store.ErrNotFound):
			a.renderMessage(w, r, http.StatusNotFound, "用户不存在", "没有找到这个用户.")
		case errors.Is(err, store.ErrLastAdmin):
			a.renderMessage(w, r, http.StatusConflict, "操作失败", "必须保留至少一个启用的管理员.")
		default:
			slog.Error("set user status failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", "暂时无法修改账号状态.")
		}
		return
	}
	notice := "user_enabled"
	if disabled {
		notice = "user_disabled"
	}
	a.redirect(w, r, "/admin/users?notice="+notice, http.StatusSeeOther)
}

func (a *App) handleAdminReports(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightReview); !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "open", "resolved", "all":
	default:
		status = "open"
	}
	page, size := adminPage(r, 50)
	total, err := a.store.CountReports(r.Context(), status)
	if err != nil {
		slog.Error("count reports failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取举报.")
		return
	}
	pager := newPager(page, totalPages(total, size), size)
	reports, err := a.store.ListReports(r.Context(), status, size, (pager.Page-1)*size)
	if err != nil {
		slog.Error("list reports failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取举报.")
		return
	}
	data := adminReportsData{
		baseData: a.baseData(r, "举报管理"),
		Reports:  reports,
		Status:   status,
		Pager:    pager,
	}
	data.Notice = a.noticeText(r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_reports", data)
}

func (a *App) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	filter, fromValue, toValue, err := auditFilter(r)
	if err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "读取失败", "日期格式应为 YYYY-MM-DD.")
		return
	}
	_, size := adminPage(r, 100)
	cursorValue := strings.TrimSpace(r.URL.Query().Get("cursor"))
	cursor, err := parseAuditCursor(cursorValue)
	if err != nil {
		a.renderMessage(w, r, http.StatusBadRequest, "读取失败", "翻页参数不正确.")
		return
	}
	logs, next, err := a.store.ListAuditLogsPage(r.Context(), filter, cursor, size)
	if err != nil {
		slog.Error("list audit logs failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取审计日志.")
		return
	}
	nextCursor := ""
	if next != nil {
		nextCursor = formatAuditCursor(next)
	}
	data := adminAuditData{
		baseData:   a.baseData(r, "审计日志"),
		Logs:       logs,
		Query:      filter.Query,
		From:       fromValue,
		To:         toValue,
		Size:       size,
		Cursor:     cursorValue,
		NextCursor: nextCursor,
	}
	a.renderUI(w, r, http.StatusOK, "admin_audit", data)
}

func auditFilter(r *http.Request) (store.AuditFilter, string, string, error) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 100 {
		query = string([]rune(query)[:100])
	}
	fromValue := strings.TrimSpace(r.URL.Query().Get("from"))
	toValue := strings.TrimSpace(r.URL.Query().Get("to"))
	filter := store.AuditFilter{Query: query}
	if fromValue != "" {
		from, err := time.ParseInLocation("2006-01-02", fromValue, time.Local)
		if err != nil {
			return filter, fromValue, toValue, err
		}
		filter.From = &from
	}
	if toValue != "" {
		to, err := time.ParseInLocation("2006-01-02", toValue, time.Local)
		if err != nil {
			return filter, fromValue, toValue, err
		}
		to = to.AddDate(0, 0, 1)
		filter.To = &to
	}
	return filter, fromValue, toValue, nil
}

func parseAuditCursor(raw string) (*store.AuditCursor, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	text := string(data)
	index := strings.LastIndex(text, ":")
	if index <= 0 || index == len(text)-1 {
		return nil, errors.New("bad cursor")
	}
	nano, err := strconv.ParseInt(text[:index], 10, 64)
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(text[index+1:], 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("bad cursor")
	}
	return &store.AuditCursor{CreatedAt: time.Unix(0, nano).UTC(), ID: id}, nil
}

func formatAuditCursor(cursor *store.AuditCursor) string {
	text := strconv.FormatInt(cursor.CreatedAt.UnixNano(), 10) + ":" + strconv.FormatInt(cursor.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(text))
}

func adminPage(r *http.Request, defaultSize int) (int, int) {
	page := 1
	if value, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && value > 0 {
		page = value
	}
	size := defaultSize
	if value, err := strconv.Atoi(r.URL.Query().Get("page_size")); err == nil && value >= 10 && value <= 100 {
		size = value
	}
	return page, size
}

func totalPages(total, size int) int {
	if total <= 0 || size <= 0 {
		return 1
	}
	return (total + size - 1) / size
}

func (a *App) redirectDiscussionResult(w http.ResponseWriter, r *http.Request, route, notice string) {
	next := strings.TrimSpace(r.FormValue("next"))
	if next != "" && len(next) <= 2048 {
		a.redirect(w, r, safeNext(next), http.StatusSeeOther)
		return
	}
	a.redirect(w, r, "/discussion?route="+url.QueryEscape(route)+"&notice="+notice, http.StatusSeeOther)
}
