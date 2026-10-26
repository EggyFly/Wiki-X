package render

import (
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var alertTitles = map[string]string{
	"note":      "Note",
	"tip":       "Tip",
	"important": "Important",
	"warning":   "Warning",
	"caution":   "Caution",
}

type alertTransformer struct{}

func (t *alertTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	source := reader.Source()
	ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		quote, ok := node.(*ast.Blockquote)
		if !ok {
			return ast.WalkContinue, nil
		}
		paragraph, ok := quote.FirstChild().(*ast.Paragraph)
		if !ok || paragraph.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		firstLine := paragraph.Lines().At(0)
		marker := strings.TrimSpace(string(firstLine.Value(source)))
		kind := alertKind(marker)
		if kind == "" {
			return ast.WalkContinue, nil
		}
		paragraph.Lines().SetSliced(1, paragraph.Lines().Len())
		remaining := len(marker)
		for child := paragraph.FirstChild(); child != nil && remaining > 0; {
			next := child.NextSibling()
			value := child.Text(source)
			if len(value) > remaining {
				break
			}
			paragraph.RemoveChild(paragraph, child)
			remaining -= len(value)
			child = next
		}
		if paragraph.Lines().Len() == 0 && paragraph.FirstChild() == nil {
			quote.RemoveChild(quote, paragraph)
		}
		quote.SetAttributeString("data-alert", []byte(kind))
		return ast.WalkContinue, nil
	})
}

func alertKind(marker string) string {
	marker = strings.ToUpper(strings.TrimSpace(marker))
	switch marker {
	case "[!NOTE]":
		return "note"
	case "[!TIP]":
		return "tip"
	case "[!IMPORTANT]":
		return "important"
	case "[!WARNING]":
		return "warning"
	case "[!CAUTION]":
		return "caution"
	default:
		return ""
	}
}

type blockquoteRenderer struct{}

func (r *blockquoteRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindBlockquote, r.renderBlockquote)
}

func (r *blockquoteRenderer) renderBlockquote(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	quote, ok := node.(*ast.Blockquote)
	if !ok {
		return ast.WalkContinue, nil
	}
	kind := ""
	if value, ok := quote.AttributeString("data-alert"); ok {
		switch item := value.(type) {
		case []byte:
			kind = string(item)
		case string:
			kind = item
		}
	}
	if kind == "" {
		if entering {
			_, _ = writer.WriteString("<blockquote>\n")
		} else {
			_, _ = writer.WriteString("</blockquote>\n")
		}
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = writer.WriteString(`<div class="markdown-alert markdown-alert-`)
		_, _ = writer.WriteString(kind)
		_, _ = writer.WriteString(`">` + "\n" + `<p class="markdown-alert-title">`)
		_, _ = writer.WriteString(alertTitles[kind])
		_, _ = writer.WriteString("</p>\n")
	} else {
		_, _ = writer.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}
