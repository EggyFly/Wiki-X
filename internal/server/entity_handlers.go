package server

import (
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleAdminEntities(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	types, err := a.store.ListEntityTypes(r.Context())
	if err != nil {
		slog.Error("list entity types failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取实体类型.")
		return
	}
	entities, err := a.store.ListEntities(r.Context(), 1000)
	if err != nil {
		slog.Error("list entities failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取实体.")
		return
	}
	data := adminEntitiesData{
		baseData: a.baseData(r, "实体数据"),
		Types:    types,
		Entities: entities,
	}
	if id, err := strconv.ParseInt(r.URL.Query().Get("edit"), 10, 64); err == nil && id > 0 {
		entity, err := a.store.GetEntity(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			a.renderMessage(w, r, http.StatusNotFound, "实体不存在", "没有找到这个实体.")
			return
		}
		if err != nil {
			slog.Error("load entity failed", "err", err)
			a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取实体.")
			return
		}
		data.Edit = entity
		data.EditType = findEntityType(types, entity.TypeID)
		if data.EditType != nil {
			data.FieldValues = buildEntityFieldValues(data.EditType, entity.Data)
		}
	} else if id, err := strconv.ParseInt(r.URL.Query().Get("type"), 10, 64); err == nil && id > 0 {
		data.EditType = findEntityType(types, id)
		if data.EditType != nil {
			data.FieldValues = buildEntityFieldValues(data.EditType, map[string]string{})
		}
	}
	data.Notice = a.noticeTextFor(a.uiLang(), r.URL.Query().Get("notice"))
	a.renderUI(w, r, http.StatusOK, "admin_entities", data)
}

func (a *App) handleAdminEntityTypeCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	label := strings.TrimSpace(r.FormValue("label"))
	if _, err := a.store.CreateEntityType(r.Context(), name, label, session.User.ID); err != nil {
		a.renderEntityError(w, r, err, "暂时无法创建实体类型.")
		return
	}
	a.redirect(w, r, "/admin/entities?notice=entity_type_created", http.StatusSeeOther)
}

func (a *App) handleAdminEntityTypeDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "实体类型编号不正确.")
		return
	}
	if err := a.store.DeleteEntityType(r.Context(), id, session.User.ID); err != nil {
		a.renderEntityError(w, r, err, "暂时无法删除实体类型.")
		return
	}
	a.redirect(w, r, "/admin/entities?notice=entity_type_deleted", http.StatusSeeOther)
}

func (a *App) handleAdminEntityFieldCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	typeID, err := strconv.ParseInt(r.FormValue("type_id"), 10, 64)
	if err != nil || typeID <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "实体类型编号不正确.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	label := strings.TrimSpace(r.FormValue("label"))
	kind := strings.TrimSpace(r.FormValue("kind"))
	required := r.FormValue("required") == "1"
	options := splitOptions(r.FormValue("options"))
	if err := a.store.AddEntityField(r.Context(), typeID, name, label, kind, required, options, 0, session.User.ID); err != nil {
		a.renderEntityError(w, r, err, "暂时无法添加字段.")
		return
	}
	a.redirect(w, r, "/admin/entities?notice=entity_field_created", http.StatusSeeOther)
}

func (a *App) handleAdminEntityFieldDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "字段编号不正确.")
		return
	}
	if err := a.store.DeleteEntityField(r.Context(), id, session.User.ID); err != nil {
		a.renderEntityError(w, r, err, "暂时无法删除字段.")
		return
	}
	a.redirect(w, r, "/admin/entities?notice=entity_field_deleted", http.StatusSeeOther)
}

func (a *App) handleAdminEntitySave(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	typeID, err := strconv.ParseInt(r.FormValue("type_id"), 10, 64)
	if err != nil || typeID <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "保存失败", "实体类型编号不正确.")
		return
	}
	types, err := a.store.ListEntityTypes(r.Context())
	if err != nil {
		slog.Error("list entity types failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "保存失败", "暂时无法读取实体类型.")
		return
	}
	entityType := findEntityType(types, typeID)
	if entityType == nil {
		a.renderMessage(w, r, http.StatusBadRequest, "保存失败", "实体类型不存在.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	label := strings.TrimSpace(r.FormValue("label"))
	values := make(map[string]string, len(entityType.Fields))
	for _, field := range entityType.Fields {
		values[field.Name] = strings.TrimSpace(r.FormValue("field_" + field.Name))
	}
	_, err = a.store.SaveEntity(r.Context(), store.EntityInput{
		ID:      id,
		TypeID:  typeID,
		Name:    name,
		Label:   label,
		Data:    values,
		ActorID: session.User.ID,
	})
	if err != nil {
		if errors.Is(err, store.ErrBadEntity) {
			a.renderEntityFormError(w, r, types, entityType, id, name, label, values)
			return
		}
		a.renderEntityError(w, r, err, "暂时无法保存实体.")
		return
	}
	a.reloadEntities(r.Context())
	a.redirect(w, r, "/admin/entities?notice=entity_saved", http.StatusSeeOther)
}

func (a *App) handleAdminEntityDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if !a.parseGuard(w, r, session) {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "实体编号不正确.")
		return
	}
	if err := a.store.DeleteEntity(r.Context(), id, session.User.ID); err != nil {
		a.renderEntityError(w, r, err, "暂时无法删除实体.")
		return
	}
	a.reloadEntities(r.Context())
	a.redirect(w, r, "/admin/entities?notice=entity_deleted", http.StatusSeeOther)
}

func (a *App) handleEntities(w http.ResponseWriter, r *http.Request) {
	types, err := a.store.ListEntityTypes(r.Context())
	if err != nil {
		slog.Error("list entity types failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取实体类型.")
		return
	}
	typeName := strings.TrimSpace(r.URL.Query().Get("type"))
	sortField := strings.TrimSpace(r.URL.Query().Get("sort"))
	order := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("order")))
	if order != "desc" {
		order = "asc"
	}
	items, err := a.store.ListEntitiesByType(r.Context(), typeName)
	if err != nil {
		slog.Error("list entities failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取实体.")
		return
	}
	data := publicEntitiesData{
		baseData: a.baseData(r, "实体图鉴"),
		Types:    types,
		Sort:     sortField,
		Order:    order,
	}
	var fields []*store.EntityField
	if typeName != "" {
		for _, item := range types {
			if item.Name == typeName {
				data.TypeName = item.Name
				data.TypeLabel = item.Label
				fields = item.Fields
				break
			}
		}
	}
	sortEntities(items, fields, sortField, order)
	data.Fields = fields
	data.Total = len(items)
	page, size := adminPage(r, 50)
	data.Pager = newPager(page, totalPages(data.Total, size), size)
	start := (data.Pager.Page - 1) * size
	if start > len(items) {
		start = len(items)
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	data.Items = items[start:end]
	a.renderUI(w, r, http.StatusOK, "entities", data)
}

func (a *App) renderEntityFormError(w http.ResponseWriter, r *http.Request, types []*store.EntityType, entityType *store.EntityType, id int64, name, label string, values map[string]string) {
	entities, _ := a.store.ListEntities(r.Context(), 1000)
	data := adminEntitiesData{
		baseData:    a.baseData(r, "实体数据"),
		Types:       types,
		Entities:    entities,
		Edit:        &store.EntityRecord{ID: id, TypeID: entityType.ID, Name: name, Label: label, Data: values},
		EditType:    entityType,
		FieldValues: buildEntityFieldValues(entityType, values),
	}
	data.Error = "实体数据不合法, 请检查必填项、数字、日期、链接和下拉选项."
	a.renderUI(w, r, http.StatusBadRequest, "admin_entities", data)
}

func (a *App) renderEntityError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	switch {
	case errors.Is(err, store.ErrBadEntity):
		a.renderMessage(w, r, http.StatusBadRequest, "操作失败", "实体名称、字段或数据不合法.")
	case errors.Is(err, store.ErrEntityExists):
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "实体名称已存在.")
	case errors.Is(err, store.ErrEntityTypeExists):
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "实体类型已存在.")
	case errors.Is(err, store.ErrEntityFieldExists):
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "字段名称已存在.")
	case errors.Is(err, store.ErrEntityTypeNotEmpty):
		a.renderMessage(w, r, http.StatusConflict, "操作失败", "实体类型下还有数据, 不能删除.")
	case errors.Is(err, store.ErrRevokeDisabled):
		a.renderDeleteDisabled(w, r)
	case errors.Is(err, store.ErrNotFound):
		a.renderMessage(w, r, http.StatusNotFound, "不存在", "没有找到对应的实体或类型.")
	default:
		slog.Error("entity operation failed", "err", err)
		a.renderMessage(w, r, http.StatusInternalServerError, "操作失败", fallback)
	}
}

func findEntityType(types []*store.EntityType, id int64) *store.EntityType {
	for _, item := range types {
		if item.ID == id {
			return item
		}
	}
	return nil
}

func buildEntityFieldValues(entityType *store.EntityType, data map[string]string) []entityFieldValue {
	values := make([]entityFieldValue, 0, len(entityType.Fields))
	for _, field := range entityType.Fields {
		value := data[field.Name]
		checked := false
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			checked = true
		}
		values = append(values, entityFieldValue{Field: field, Value: value, Checked: checked})
	}
	return values
}

func splitOptions(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func sortEntities(items []*store.EntityRecord, fields []*store.EntityField, sortField, order string) {
	kind := store.EntityKindText
	if sortField != "" {
		found := false
		for _, field := range fields {
			if field.Name == sortField {
				kind = field.Kind
				found = true
				break
			}
		}
		if !found {
			sortField = ""
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		leftValue := left.Label
		rightValue := right.Label
		if sortField != "" {
			leftValue = left.Data[sortField]
			rightValue = right.Data[sortField]
		}
		if leftValue == "" && rightValue != "" {
			return false
		}
		if rightValue == "" && leftValue != "" {
			return true
		}
		result := compareEntityValues(kind, leftValue, rightValue)
		if order == "desc" {
			return result > 0
		}
		return result < 0
	})
}

func compareEntityValues(kind, left, right string) int {
	switch kind {
	case store.EntityKindNumber:
		leftNumber, leftErr := strconv.ParseFloat(left, 64)
		rightNumber, rightErr := strconv.ParseFloat(right, 64)
		if leftErr == nil && rightErr == nil {
			switch {
			case leftNumber < rightNumber:
				return -1
			case leftNumber > rightNumber:
				return 1
			default:
				return 0
			}
		}
	case store.EntityKindDate:
		leftTime, leftErr := time.Parse("2006-01-02", left)
		rightTime, rightErr := time.Parse("2006-01-02", right)
		if leftErr == nil && rightErr == nil {
			switch {
			case leftTime.Before(rightTime):
				return -1
			case leftTime.After(rightTime):
				return 1
			default:
				return 0
			}
		}
	case store.EntityKindBool:
		return strings.Compare(strings.ToLower(left), strings.ToLower(right))
	}
	return strings.Compare(strings.ToLower(left), strings.ToLower(right))
}
