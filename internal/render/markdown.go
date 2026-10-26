package render

import (
	"bytes"
	"html/template"
	"strings"

	mathjax "github.com/litao91/goldmark-mathjax"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"

	"github.com/EggyFly/Wiki-X/internal/components"
)

const componentPrefix = "wx-"

type Markdown struct {
	md        goldmark.Markdown
	entities  components.EntityLookup
	repo      string
	sanitizer *bluemonday.Policy
}

func NewMarkdown() *Markdown {
	return NewMarkdownWithRepo("")
}

func NewMarkdownWithRepo(repo string) *Markdown {
	return NewMarkdownWithEntitiesAndRepo(nil, repo)
}

func NewMarkdownWithEntities(lookup components.EntityLookup) *Markdown {
	return NewMarkdownWithEntitiesAndRepo(lookup, "")
}

func NewMarkdownWithEntitiesAndRepo(lookup components.EntityLookup, repo string) *Markdown {
	return &Markdown{
		md: goldmark.New(
			goldmark.WithExtensions(
				extension.GFM,
				extension.Footnote,
				mathjax.NewMathJax(mathjax.WithInlineDelim("", ""), mathjax.WithBlockDelim("", "")),
			),
			goldmark.WithParserOptions(
				parser.WithAutoHeadingID(),
				parser.WithASTTransformers(util.Prioritized(&alertTransformer{}, 100)),
				parser.WithInlineParsers(
					util.Prioritized(&mentionParser{}, 499),
					util.Prioritized(&emojiParser{codes: newEmojiCodes()}, 500),
					util.Prioritized(&issueParser{repo: repo}, 501),
				),
			),
			goldmark.WithRendererOptions(
				html.WithUnsafe(),
				renderer.WithNodeRenderers(
					util.Prioritized(&fencedRenderer{entities: lookup}, 100),
					util.Prioritized(&blockquoteRenderer{}, 100),
					util.Prioritized(&mathRenderer{}, 100),
					util.Prioritized(&emojiRenderer{}, 100),
					util.Prioritized(&mentionRenderer{}, 100),
					util.Prioritized(&issueRenderer{}, 100),
				),
			),
		),
		entities:  lookup,
		repo:      repo,
		sanitizer: newSanitizer(),
	}
}

func (m *Markdown) ToHTML(source []byte) (template.HTML, error) {
	var buf bytes.Buffer
	if err := m.md.Convert(source, &buf); err != nil {
		return "", err
	}
	return template.HTML(m.sanitizer.Sanitize(buf.String())), nil
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
	if strings.EqualFold(language, "mermaid") {
		_, _ = writer.WriteString(`<pre class="mermaid">`)
		_, _ = writer.WriteString(template.HTMLEscapeString(string(blockText(block, source))))
		_, _ = writer.WriteString("</pre>\n")
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
