package extension

import (
	"html/template"
	"sort"
	"strings"
	"sync"
)

type Node struct {
	Type        string
	Key         string
	Title       string
	Description string
	Config      map[string]string
}

type Renderer func(Node) template.HTML

var (
	mu        sync.RWMutex
	renderers = make(map[string]Renderer)
)

func Register(kind string, renderer Renderer) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" || renderer == nil {
		return
	}
	mu.Lock()
	renderers[kind] = renderer
	mu.Unlock()
}

func Kinds() []string {
	mu.RLock()
	kinds := make([]string, 0, len(renderers))
	for kind := range renderers {
		kinds = append(kinds, kind)
	}
	mu.RUnlock()
	sort.Strings(kinds)
	return kinds
}

func Render(node Node) template.HTML {
	kind := strings.ToLower(strings.TrimSpace(node.Config["kind"]))
	if kind == "" {
		kind = "card"
	}
	mu.RLock()
	renderer := renderers[kind]
	mu.RUnlock()
	if renderer == nil {
		return template.HTML(`<p class="muted">Unknown interactive kind.</p>`)
	}
	return renderer(node)
}

func init() {
	Register("card", renderCard)
	Register("quiz", renderQuiz)
}

func renderCard(node Node) template.HTML {
	return execute(`<section class="interactive-card"><h2>{{.Title}}</h2>{{if .Description}}<p>{{.Description}}</p>{{end}}<dl>{{range .Rows}}<div><dt>{{.Key}}</dt><dd>{{.Value}}</dd></div>{{end}}</dl></section>`, node)
}

func renderQuiz(node Node) template.HTML {
	return execute(`<section class="interactive-card interactive-quiz"><h2>{{.Title}}</h2>{{if .Description}}<p>{{.Description}}</p>{{end}}<p class="muted">{{index .Config "question"}}</p><ul>{{range splitOptions (index .Config "options")}}<li>{{.}}</li>{{end}}</ul><details><summary>Show answer</summary><p>{{index .Config "answer"}}</p></details></section>`, node)
}

type configRow struct {
	Key   string
	Value string
}

func execute(source string, node Node) template.HTML {
	rows := make([]configRow, 0, len(node.Config))
	keys := make([]string, 0, len(node.Config))
	for key := range node.Config {
		if key != "kind" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		rows = append(rows, configRow{Key: key, Value: node.Config[key]})
	}
	view := struct {
		Title       string
		Description string
		Config      map[string]string
		Rows        []configRow
	}{Title: node.Title, Description: node.Description, Config: node.Config, Rows: rows}
	tmpl, err := template.New("interactive").Funcs(template.FuncMap{
		"splitOptions": func(value string) []string {
			parts := strings.Split(value, "|")
			out := make([]string, 0, len(parts))
			for _, part := range parts {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
			return out
		},
	}).Parse(source)
	if err != nil {
		return template.HTML(`<p class="muted">Interactive template error.</p>`)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, view); err != nil {
		return template.HTML(`<p class="muted">Interactive render error.</p>`)
	}
	return template.HTML(buf.String())
}
