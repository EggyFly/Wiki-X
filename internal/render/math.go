package render

import (
	"bytes"
	"html/template"

	mathjax "github.com/litao91/goldmark-mathjax"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type mathRenderer struct{}

func (r *mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(mathjax.KindInlineMath, r.renderInline)
	reg.Register(mathjax.KindMathBlock, r.renderBlock)
}

func (r *mathRenderer) renderInline(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var buf bytes.Buffer
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		text, ok := child.(*ast.Text)
		if !ok {
			continue
		}
		value := text.Segment.Value(source)
		value = bytes.TrimSuffix(value, []byte("\n"))
		buf.Write(value)
	}
	_, _ = writer.WriteString(`<span class="math inline">`)
	_, _ = writer.WriteString(template.HTMLEscapeString(buf.String()))
	_, _ = writer.WriteString(`</span>`)
	return ast.WalkSkipChildren, nil
}

func (r *mathRenderer) renderBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block, ok := node.(*mathjax.MathBlock)
	if !ok {
		return ast.WalkContinue, nil
	}
	var buf bytes.Buffer
	for i := 0; i < block.Lines().Len(); i++ {
		line := block.Lines().At(i)
		buf.Write(line.Value(source))
	}
	_, _ = writer.WriteString(`<div class="math display">`)
	_, _ = writer.WriteString(template.HTMLEscapeString(buf.String()))
	_, _ = writer.WriteString("</div>\n")
	return ast.WalkSkipChildren, nil
}
