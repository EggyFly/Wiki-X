package render

import (
	"bytes"
	"html/template"
	"regexp"
	"unicode"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	kindIssue        = ast.NewNodeKind("Issue")
	repoIssueSuffix  = regexp.MustCompile(`([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)$`)
	issueRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
)

type issueNode struct {
	ast.BaseInline
	repo   string
	number string
	label  string
}

func (n *issueNode) Kind() ast.NodeKind {
	return kindIssue
}

func (n *issueNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"repo": n.repo, "number": n.number, "label": n.label}, nil)
}

type issueParser struct {
	repo string
}

func (p *issueParser) Trigger() []byte {
	return []byte{'#'}
}

func (p *issueParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 2 || line[0] != '#' {
		return nil
	}
	length := 0
	for length < len(line)-1 && length < 12 && line[length+1] >= '0' && line[length+1] <= '9' {
		length++
	}
	if length == 0 {
		return nil
	}
	repo, matched := issueRepoPrefix(parent, block.Source())
	label := "#" + string(line[1:1+length])
	if matched {
		label = repo + label
	} else {
		prev := block.PrecendingCharacter()
		if unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '/' || prev == '#' || prev == '_' || prev == '-' || prev == '.' {
			return nil
		}
		repo = p.repo
	}
	if repo == "" || !issueRepoPattern.MatchString(repo) {
		return nil
	}
	number := string(line[1 : 1+length])
	block.Advance(1 + length)
	return &issueNode{repo: repo, number: number, label: label}
}

func issueRepoPrefix(parent ast.Node, source []byte) (string, bool) {
	last, ok := parent.LastChild().(*ast.Text)
	if !ok {
		return "", false
	}
	value := last.Segment.Value(source)
	if bytes.Contains(value, []byte("://")) {
		return "", false
	}
	match := repoIssueSuffix.FindSubmatch(value)
	if len(match) != 2 {
		return "", false
	}
	repo := string(match[1])
	last.Segment = last.Segment.WithStop(last.Segment.Stop - len(repo))
	if last.Segment.IsEmpty() {
		parent.RemoveChild(parent, last)
	}
	return repo, true
}

type issueRenderer struct{}

func (r *issueRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindIssue, r.renderIssue)
}

func (r *issueRenderer) renderIssue(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	item, ok := node.(*issueNode)
	if !ok {
		return ast.WalkContinue, nil
	}
	target := "https://github.com/" + item.repo + "/issues/" + item.number
	_, _ = writer.WriteString(`<a class="issue-link" href="`)
	_, _ = writer.WriteString(template.HTMLEscapeString(target))
	_, _ = writer.WriteString(`">`)
	_, _ = writer.WriteString(template.HTMLEscapeString(item.label))
	_, _ = writer.WriteString(`</a>`)
	return ast.WalkSkipChildren, nil
}
