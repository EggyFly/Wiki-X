package components

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRenderInfobox(t *testing.T) {
	body := []byte("title: 蛋仔\nimage: /media/abc.png\ncaption: 说明\nrows:\n  - label: 稀有度\n    value: 史诗\ntags: [道具]\nfooter: 活动期间获取")
	html, err := Render("infobox", body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="wx-infobox"`,
		`src="/media/abc.png"`,
		`class="wx-infobox-title">蛋仔<`,
		`<dt>稀有度</dt><dd>史诗</dd>`,
		`<li>道具</li>`,
		`活动期间获取`,
	} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("infobox missing %q in %s", want, html)
		}
	}
}

func TestInfoboxEscapesHTML(t *testing.T) {
	html, err := Render("infobox", []byte("title: <script>alert(1)</script>"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>") {
		t.Fatalf("raw html leaked: %s", html)
	}
	if !strings.Contains(string(html), "&lt;script&gt;") {
		t.Fatalf("escaped html missing: %s", html)
	}
}

func TestInfoboxValidation(t *testing.T) {
	cases := map[string][]byte{
		"missing title":   []byte("rows: []"),
		"bad image":       []byte("title: x\nimage: https://evil/x.png"),
		"traversal image": []byte("title: x\nimage: /media/../secret.png"),
		"unknown field":   []byte("title: x\nnope: 1"),
		"empty row value": []byte("title: x\nrows:\n  - label: a\n"),
		"too many tags":   []byte("title: x\ntags: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]"),
	}
	for name, body := range cases {
		if _, err := Render("infobox", body); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestInfoboxAliasRejected(t *testing.T) {
	body := []byte("title: &a 蛋仔\ntags: [*a]")
	if _, err := Render("infobox", body); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("alias err = %v", err)
	}
}

func TestRenderNotice(t *testing.T) {
	html, err := Render("notice", []byte("type: warn\ntitle: 注意\ntext: 活动说明"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"wx-notice-warn", "注意", "活动说明"} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("notice missing %q in %s", want, html)
		}
	}
	defaultType, err := Render("notice", []byte("text: 说明"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(defaultType), "wx-notice-info") {
		t.Fatalf("default notice type missing: %s", defaultType)
	}
}

func TestNoticeValidation(t *testing.T) {
	if _, err := Render("notice", []byte("type: nope\ntext: x")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("bad type err = %v", err)
	}
	if _, err := Render("notice", []byte("title: x")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("missing text err = %v", err)
	}
}

func TestBodyLimits(t *testing.T) {
	if _, err := Render("notice", nil); !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("empty body err = %v", err)
	}
	big := bytes.Repeat([]byte("x"), maxBodySize+1)
	if _, err := Render("notice", big); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("big body err = %v", err)
	}
}

func TestRenderGallery(t *testing.T) {
	html, err := Render("gallery", []byte("images:\n  - image: /media/a.png\n    caption: 第一张\n  - image: /media/b.png"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="wx-gallery"`,
		`src="/media/a.png"`,
		`<figcaption>第一张</figcaption>`,
		`src="/media/b.png"`,
	} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("gallery missing %q in %s", want, html)
		}
	}
}

func TestGalleryValidation(t *testing.T) {
	if _, err := Render("gallery", []byte("images: []")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("empty images err = %v", err)
	}
	if _, err := Render("gallery", []byte("images:\n  - image: https://evil/a.png")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("bad image err = %v", err)
	}
	if _, err := Render("gallery", []byte("images:\n  - image: /media/a.png\n    caption: <script>")); err != nil {
		t.Fatal(err)
	}
}

func TestGalleryEscapesCaption(t *testing.T) {
	html, err := Render("gallery", []byte("images:\n  - image: /media/a.png\n    caption: <b>说明</b>"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<b>说明</b>") {
		t.Fatalf("raw caption leaked: %s", html)
	}
}

func TestRenderCollapse(t *testing.T) {
	html, err := Render("collapse", []byte("items:\n  - title: 问题\n    text: 答案\n  - title: 另一个\n    text: 内容"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="wx-collapse"`,
		`<summary>问题</summary>`,
		`<div class="wx-collapse-body">答案</div>`,
		`<summary>另一个</summary>`,
	} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("collapse missing %q in %s", want, html)
		}
	}
}

func TestCollapseValidation(t *testing.T) {
	if _, err := Render("collapse", []byte("items: []")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("empty items err = %v", err)
	}
	if _, err := Render("collapse", []byte("items:\n  - title: 问题")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("missing text err = %v", err)
	}
	if _, err := Render("collapse", []byte("items:\n  - text: 答案")); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("missing title err = %v", err)
	}
}

func TestDeepNestedRejected(t *testing.T) {
	body := []byte("items:\n  - title: x\n    text: " + strings.Repeat("[", 10) + strings.Repeat("]", 10))
	if _, err := Render("collapse", body); !errors.Is(err, ErrTooComplex) {
		t.Fatalf("deep nested err = %v", err)
	}
}

func TestFieldLevelErrors(t *testing.T) {
	_, err := Render("infobox", []byte("title: x\nrows:\n  - label: a\n    value: b\n  - label: c"))
	if err == nil || !strings.Contains(err.Error(), "rows[1].value 不能为空") {
		t.Fatalf("field error = %v", err)
	}
	_, err = Render("gallery", []byte("images:\n  - image: bad"))
	if err == nil || !strings.Contains(err.Error(), "images[0].image 必须是站内绝对路径") {
		t.Fatalf("field error = %v", err)
	}
}

func TestUnknownComponent(t *testing.T) {
	if _, err := Render("nope", []byte("x: 1")); !errors.Is(err, ErrUnknownComponent) {
		t.Fatalf("unknown err = %v", err)
	}
}
