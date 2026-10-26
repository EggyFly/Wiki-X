package render

import (
	"html/template"
	"net/url"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var kindMention = ast.NewNodeKind("Mention")

type mentionNode struct {
	ast.BaseInline
	name string
}

func (n *mentionNode) Kind() ast.NodeKind {
	return kindMention
}

func (n *mentionNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"name": n.name}, nil)
}

type mentionParser struct{}

func (p *mentionParser) Trigger() []byte {
	return []byte{'@'}
}

func (p *mentionParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 2 || line[0] != '@' {
		return nil
	}
	prev := block.PrecendingCharacter()
	if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '_' || prev == '.' || prev == '-' {
		return nil
	}
	length := 0
	for length < len(line)-1 && length < 256 {
		r, size := utf8.DecodeRune(line[1+length:])
		if r == utf8.RuneError || !(unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_' || r == '-' || r == '.') {
			break
		}
		length += size
	}
	if length == 0 {
		return nil
	}
	name := string(line[1 : 1+length])
	if len([]rune(name)) > 64 {
		return nil
	}
	block.Advance(1 + length)
	return &mentionNode{name: name}
}

type mentionRenderer struct{}

func (r *mentionRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMention, r.renderMention)
}

func (r *mentionRenderer) renderMention(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	item, ok := node.(*mentionNode)
	if !ok {
		return ast.WalkContinue, nil
	}
	_, _ = writer.WriteString(`<a class="mention" href="/u/`)
	_, _ = writer.WriteString(url.PathEscape(item.name))
	_, _ = writer.WriteString(`">@`)
	_, _ = writer.WriteString(template.HTMLEscapeString(item.name))
	_, _ = writer.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}
