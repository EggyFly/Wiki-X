package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestEntityAdminFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/entities")

	resp := postForm(t, admin, baseURL+"/admin/entities/types", url.Values{
		"csrf_token": {csrf},
		"name":       {"character"},
		"label":      {"角色"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create type status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	types, err := data.ListEntityTypes(context.Background())
	if err != nil || len(types) != 1 {
		t.Fatalf("types = %+v, %v", types, err)
	}
	typeID := strconv.FormatInt(types[0].ID, 10)

	for _, field := range []url.Values{
		{"csrf_token": {csrf}, "type_id": {typeID}, "name": {"display_name"}, "label": {"名称"}, "kind": {"text"}, "required": {"1"}},
		{"csrf_token": {csrf}, "type_id": {typeID}, "name": {"rarity"}, "label": {"稀有度"}, "kind": {"select"}, "options": {"S,A"}, "required": {"1"}},
	} {
		resp = postForm(t, admin, baseURL+"/admin/entities/fields", field)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("add field status = %d", resp.StatusCode)
		}
		resp.Body.Close()
	}

	resp = postForm(t, admin, baseURL+"/admin/entities/save", url.Values{
		"csrf_token":         {csrf},
		"type_id":            {typeID},
		"name":               {"kiana"},
		"label":              {"琪亚娜"},
		"field_display_name": {"琪亚娜"},
		"field_rarity":       {"S"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save entity status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	entity, err := data.GetEntityByName(context.Background(), "kiana")
	if err != nil {
		t.Fatal(err)
	}
	if entity.Data["rarity"] != "S" || entity.TypeName != "character" {
		t.Fatalf("entity = %+v", entity)
	}

	page, err := data.GetPage(context.Background(), "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	editCSRF := fetchCSRFToken(t, admin, baseURL+"/edit?route=/guide/")
	body := "new guide\n\n```wx-infobox\nentity: kiana\n```\n"
	resp = postForm(t, admin, baseURL+"/edit/save", url.Values{
		"csrf_token":   {editCSRF},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"description":  {"guide page"},
		"body":         {body},
		"comment":      {"bind entity"},
		"base_version": {page.Version},
		"entity":       {"kiana"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit save status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	pageHTML := readResponse(t, mustGet(t, testHTTPClient(), baseURL+"/guide/"))
	for _, want := range []string{"琪亚娜", "稀有度", "S", "wx-infobox"} {
		if !strings.Contains(pageHTML, want) {
			t.Fatalf("page missing %q:\n%s", want, pageHTML)
		}
	}
	entitiesHTML := readResponse(t, mustGet(t, testHTTPClient(), baseURL+"/entities?type=character&sort=rarity&order=desc"))
	if !strings.Contains(entitiesHTML, "琪亚娜") || !strings.Contains(entitiesHTML, "/guide/") {
		t.Fatalf("entities page missing data:\n%s", entitiesHTML)
	}

	resp = postForm(t, admin, baseURL+"/admin/entities/save", url.Values{
		"csrf_token":         {csrf},
		"id":                 {strconv.FormatInt(entity.ID, 10)},
		"type_id":            {typeID},
		"name":               {"kiana"},
		"label":              {"琪亚娜"},
		"field_display_name": {"琪亚娜"},
		"field_rarity":       {"A"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update entity status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	pageHTML = readResponse(t, mustGet(t, testHTTPClient(), baseURL+"/guide/"))
	if !strings.Contains(pageHTML, "稀有度") || !strings.Contains(pageHTML, "A") {
		t.Fatalf("page cache did not refresh entity data:\n%s", pageHTML)
	}
	if strings.Contains(pageHTML, "<dd>S</dd>") {
		t.Fatalf("stale entity value in page:\n%s", pageHTML)
	}

	resp = postForm(t, admin, baseURL+"/admin/entities/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {strconv.FormatInt(entity.ID, 10)},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete entity status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = postForm(t, admin, baseURL+"/admin/entities/types/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {typeID},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete type status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestEntityValidationPage(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/entities")
	resp := postForm(t, admin, baseURL+"/admin/entities/types", url.Values{
		"csrf_token": {csrf}, "name": {"item"}, "label": {"道具"},
	})
	resp.Body.Close()
	types, _ := data.ListEntityTypes(context.Background())
	typeID := strconv.FormatInt(types[0].ID, 10)
	resp = postForm(t, admin, baseURL+"/admin/entities/fields", url.Values{
		"csrf_token": {csrf}, "type_id": {typeID}, "name": {"level"}, "label": {"等级"}, "kind": {"number"}, "required": {"1"},
	})
	resp.Body.Close()

	resp = postForm(t, admin, baseURL+"/admin/entities/save", url.Values{
		"csrf_token": {csrf}, "type_id": {typeID}, "name": {"bad"}, "label": {"坏数据"}, "field_level": {"abc"},
	})
	body := readResponse(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "实体数据不合法") {
		t.Fatalf("validation status = %d", resp.StatusCode)
	}
}
