package components

import (
	"strings"
	"testing"
)

func testEntityLookup(key string) (*Entity, bool) {
	if key != "kiana" {
		return nil, false
	}
	return &Entity{
		Key:   "kiana",
		Label: "琪亚娜",
		Type:  "角色",
		Fields: []EntityField{
			{Name: "rarity", Label: "稀有度", Value: "S"},
			{Name: "level", Label: "等级", Value: "80"},
			{Name: "empty", Label: "空字段", Value: ""},
		},
	}, true
}

func TestInfoboxEntity(t *testing.T) {
	body := []byte("entity: kiana\nrows:\n  - label: 备注\n    value: <测试>\n")
	html, err := RenderWithEntities("infobox", body, testEntityLookup)
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, want := range []string{"琪亚娜", "稀有度", "S", "等级", "80", "备注", "&lt;测试&gt;", "wx-infobox"} {
		if !strings.Contains(text, want) {
			t.Fatalf("html missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "空字段") {
		t.Fatal("empty entity fields should be skipped")
	}
}

func TestInfoboxEntityMissing(t *testing.T) {
	body := []byte("entity: missing\n")
	if _, err := RenderWithEntities("infobox", body, testEntityLookup); err == nil {
		t.Fatal("missing entity should fail")
	}
	if _, err := RenderWithEntities("infobox", body, nil); err == nil {
		t.Fatal("nil lookup should fail")
	}
	bad := []byte("entity: 琪亚娜\n")
	if _, err := RenderWithEntities("infobox", bad, testEntityLookup); err == nil {
		t.Fatal("non-ascii entity key should fail")
	}
}
