package extension

import (
	"html"
	"html/template"
	"strings"
	"testing"
)

func TestRenderBuiltins(t *testing.T) {
	card := string(Render(Node{
		Type: "widget", Key: "daily", Title: "Daily", Description: "hello",
		Config: map[string]string{"kind": "card", "line": "<script>"},
	}))
	if !strings.Contains(card, "Daily") || !strings.Contains(card, "&lt;script&gt;") || strings.Contains(card, "<script>") {
		t.Fatalf("card = %s", card)
	}
	quiz := string(Render(Node{
		Type: "game", Key: "quiz", Title: "Quiz",
		Config: map[string]string{"kind": "quiz", "question": "2 plus 2", "options": "3|4", "answer": "4"},
	}))
	if !strings.Contains(quiz, "2 plus 2") || !strings.Contains(quiz, "Show answer") {
		t.Fatalf("quiz = %s", quiz)
	}
}

func TestRegisterCustomRenderer(t *testing.T) {
	Register("custom", func(node Node) template.HTML {
		return template.HTML("<strong>" + html.EscapeString(node.Title) + "</strong>")
	})
	got := string(Render(Node{Config: map[string]string{"kind": "custom"}, Title: "<x>"}))
	if got != "<strong>&lt;x&gt;</strong>" {
		t.Fatalf("custom = %q", got)
	}
}
