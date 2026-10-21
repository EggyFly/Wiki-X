package render

import (
	"strings"
	"testing"
)

func TestToHTML(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("# Hello World\n\n| a | b |\n| - | - |\n| 1 | 2 |\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, `id="hello-world"`) {
		t.Fatalf("missing heading id: %s", got)
	}
	if !strings.Contains(got, "<table>") {
		t.Fatalf("missing table: %s", got)
	}
}

func TestEscapesRawHTML(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("<script>alert(1)</script>"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(html), "<script>") {
		t.Fatalf("raw html leaked: %s", html)
	}
}

func TestDropsDangerousLink(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("[click](javascript:alert(1))"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(html)), "javascript:") {
		t.Fatalf("dangerous link leaked: %s", html)
	}
}

func TestDropsDangerousDataLink(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("[click](data:text/html,<script>alert(1)</script>)"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(html)), "data:text/html") {
		t.Fatalf("dangerous data link leaked: %s", html)
	}
}

func TestRendersComponent(t *testing.T) {
	markdown := "```wx-notice\ntype: warn\ntext: 活动说明\n```\n"
	html, err := NewMarkdown().ToHTML([]byte(markdown))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "wx-notice-warn") || !strings.Contains(string(html), "活动说明") {
		t.Fatalf("component missing: %s", html)
	}
	if strings.Contains(string(html), "<pre>") {
		t.Fatalf("component should not render as code: %s", html)
	}
}

func TestComponentErrorBox(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("```wx-infobox\nnope: 1\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "wx-component-error") {
		t.Fatalf("error box missing: %s", html)
	}
}

func TestUnknownComponentErrorBox(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("```wx-nope\nx: 1\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "wx-component-error") || !strings.Contains(string(html), "nope") {
		t.Fatalf("unknown component box missing: %s", html)
	}
}

func TestPlainFencedCodeStaysCode(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("```go\nfmt.Println(1)\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `<code class="language-go">`) || !strings.Contains(string(html), "fmt.Println") {
		t.Fatalf("plain code block changed: %s", html)
	}
}
