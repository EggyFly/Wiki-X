package render

import (
	"bytes"
	"html/template"

	"github.com/kyokomi/emoji/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var kindEmoji = ast.NewNodeKind("Emoji")

type emojiNode struct {
	ast.BaseInline
	alias string
	value string
}

func (n *emojiNode) Kind() ast.NodeKind {
	return kindEmoji
}

func (n *emojiNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"alias": n.alias}, nil)
}

type emojiParser struct {
	codes map[string]string
}

func (p *emojiParser) Trigger() []byte {
	return []byte{':'}
}

func (p *emojiParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 3 || line[0] != ':' {
		return nil
	}
	end := bytes.IndexByte(line[1:], ':')
	if end < 1 || end > 64 {
		return nil
	}
	alias := string(line[1 : end+1])
	key := ":" + alias + ":"
	value, ok := p.codes[key]
	if !ok {
		return nil
	}
	block.Advance(end + 2)
	return &emojiNode{alias: alias, value: value}
}

type emojiRenderer struct{}

func (r *emojiRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindEmoji, r.renderEmoji)
}

func (r *emojiRenderer) renderEmoji(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	item, ok := node.(*emojiNode)
	if !ok {
		return ast.WalkContinue, nil
	}
	_, _ = writer.WriteString(`<span class="emoji" title="`)
	_, _ = writer.WriteString(template.HTMLEscapeString(":" + item.alias + ":"))
	_, _ = writer.WriteString(`">`)
	_, _ = writer.WriteString(template.HTMLEscapeString(item.value))
	_, _ = writer.WriteString(`</span>`)
	return ast.WalkSkipChildren, nil
}

func newEmojiCodes() map[string]string {
	return emoji.CodeMap()
}
