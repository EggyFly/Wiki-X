package render

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/EggyFly/Wiki-X/internal/components"
)

const componentPrefix = "wx-"

type Markdown struct {
	md       goldmark.Markdown
	entities components.EntityLookup
}

func NewMarkdown() *Markdown {
	return NewMarkdownWithEntities(nil)
}

func NewMarkdownWithEntities(lookup components.EntityLookup) *Markdown {
	return &Markdown{
		md: goldmark.New(
			goldmark.WithExtensions(extension.GFM, extension.Footnote),
			goldmark.WithParserOptions(parser.WithAutoHeadingID()),
			goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(&fencedRenderer{entities: lookup}, 100))),
		),
		entities: lookup,
	}
}

func (m *Markdown) ToHTML(source []byte) (template.HTML, error) {
	var buf bytes.Buffer
	if err := m.md.Convert(source, &buf); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

type fencedRenderer struct {
	entities components.EntityLookup
}

func (r *fencedRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, r.renderFenced)
}

func (r *fencedRenderer) renderFenced(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block, ok := node.(*ast.FencedCodeBlock)
	if !ok {
		return ast.WalkContinue, nil
	}
	language := string(block.Language(source))
	if strings.HasPrefix(language, componentPrefix) {
		name := strings.ToLower(strings.TrimPrefix(language, componentPrefix))
		html, err := components.RenderWithEntities(name, blockText(block, source), r.entities)
		if err != nil {
			_, _ = writer.Write([]byte(components.ErrorBox(err)))
			return ast.WalkContinue, nil
		}
		_, _ = writer.Write([]byte(html))
		return ast.WalkContinue, nil
	}
	return r.renderCode(writer, source, block, language)
}

func (r *fencedRenderer) renderCode(writer util.BufWriter, source []byte, block *ast.FencedCodeBlock, language string) (ast.WalkStatus, error) {
	_, _ = writer.WriteString("<pre><code")
	if language != "" {
		_, _ = writer.WriteString(` class="language-`)
		_, _ = writer.WriteString(template.HTMLEscapeString(language))
		_ = writer.WriteByte('"')
	}
	_ = writer.WriteByte('>')
	for i := 0; i < block.Lines().Len(); i++ {
		segment := block.Lines().At(i)
		_, _ = writer.Write(util.EscapeHTML(segment.Value(source)))
	}
	_, _ = writer.WriteString("</code></pre>\n")
	return ast.WalkContinue, nil
}

func blockText(block *ast.FencedCodeBlock, source []byte) []byte {
	var buf bytes.Buffer
	for i := 0; i < block.Lines().Len(); i++ {
		segment := block.Lines().At(i)
		buf.Write(segment.Value(source))
	}
	return buf.Bytes()
}
