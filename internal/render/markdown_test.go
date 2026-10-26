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

func TestGitHubAlertsAndSafeHTML(t *testing.T) {
	source := "> [!WARNING]\n> **careful**\n\n" +
		"<details><summary>More</summary><kbd>Ctrl</kbd> <mark>hot</mark></details>\n\n" +
		"- [x] done\n\n" +
		"<img src=\"/media/a.png\" alt=\"icon\" onerror=\"alert(1)\">\n"
	html, err := NewMarkdown().ToHTML([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{
		"markdown-alert-warning", "markdown-alert-title", "<strong>careful</strong>",
		"<details>", "<summary>More</summary>", "<kbd>Ctrl</kbd>", "<mark>hot</mark>",
		`type="checkbox"`, "<img", "/media/a.png",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "[!WARNING]") || strings.Contains(got, "onerror") {
		t.Fatalf("unsafe or marker content leaked: %s", got)
	}
}

func TestStripsUnsafeHTML(t *testing.T) {
	source := `<div onclick="alert(1)"><script>alert(1)</script><iframe src="https://evil.example"></iframe><style>body{display:none}</style></div>`
	html, err := NewMarkdown().ToHTML([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ToLower(string(html))
	for _, bad := range []string{"<script", "<iframe", "<style", "onclick", "alert(1)"} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe html leaked %q: %s", bad, got)
		}
	}
}

func TestAudioSurvivesSanitizer(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("```wx-audio\nsrc: /media/theme.mp3\ntitle: Theme\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, "<audio") || !strings.Contains(got, `src="/media/theme.mp3"`) || !strings.Contains(got, "wx-audio") {
		t.Fatalf("audio component was stripped: %s", got)
	}
}

func TestMathAndMermaid(t *testing.T) {
	source := "Inline $x^2$ math.\n\n$$\n\\frac{a}{b}\n$$\n\n```mermaid\ngraph TD\nA-->B\n```\n"
	html, err := NewMarkdown().ToHTML([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{
		`<span class="math inline">x^2</span>`,
		`<div class="math display">`,
		`\frac{a}{b}`,
		`<pre class="mermaid">`,
		"A--&gt;B",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
}

func TestEmojiShortcode(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("Hello :smile: and `:smile:` and :not_an_emoji:"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, `class="emoji"`) || !strings.Contains(got, "😄") {
		t.Fatalf("emoji missing: %s", got)
	}
	if strings.Count(got, `class="emoji"`) != 1 {
		t.Fatalf("emoji parsed inside code or unknown alias: %s", got)
	}
}

func TestMention(t *testing.T) {
	html, err := NewMarkdown().ToHTML([]byte("Hello @alice and mail foo@bar.com"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	if !strings.Contains(got, `class="mention" href="/u/alice"`) || !strings.Contains(got, ">@alice</a>") {
		t.Fatalf("mention missing: %s", got)
	}
	if strings.Contains(got, "/u/bar.com") {
		t.Fatalf("email was parsed as mention: %s", got)
	}
}

func TestIssueLinks(t *testing.T) {
	html, err := NewMarkdownWithRepo("owner/repo").ToHTML([]byte("See #12 and other/project#34 and https://example.com/#5"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{
		`href="https://github.com/owner/repo/issues/12"`, ">#12</a>",
		`href="https://github.com/other/project/issues/34"`, ">other/project#34</a>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "issues/5") {
		t.Fatalf("url fragment parsed as issue: %s", got)
	}
}
