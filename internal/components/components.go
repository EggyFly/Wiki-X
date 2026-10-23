package components

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxBodySize = 8 * 1024
	maxNodes    = 200
	maxDepth    = 8
)

var (
	ErrEmptyBody        = errors.New("component body is empty")
	ErrBodyTooLarge     = errors.New("component body is too large")
	ErrTooComplex       = errors.New("component body is too complex")
	ErrUnknownComponent = errors.New("unknown component")
	ErrInvalidSpec      = errors.New("component spec is invalid")
)

type EntityField struct {
	Name  string
	Label string
	Value string
}

type Entity struct {
	Key    string
	Label  string
	Type   string
	Fields []EntityField
}

type EntityLookup func(key string) (*Entity, bool)

type componentFunc func(body []byte, lookup EntityLookup) (template.HTML, error)

var registry = map[string]componentFunc{
	"infobox":  renderInfobox,
	"notice":   renderNotice,
	"gallery":  renderGallery,
	"collapse": renderCollapse,
	"audio":    renderAudio,
}

func Render(name string, body []byte) (template.HTML, error) {
	return RenderWithEntities(name, body, nil)
}

func RenderWithEntities(name string, body []byte, lookup EntityLookup) (template.HTML, error) {
	render, ok := registry[name]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	if len(body) == 0 {
		return "", ErrEmptyBody
	}
	if len(body) > maxBodySize {
		return "", ErrBodyTooLarge
	}
	return render(body, lookup)
}

func ErrorBox(err error) template.HTML {
	return errorBox(err.Error())
}

func errorBox(msg string) template.HTML {
	return template.HTML(`<div class="wx-component-error">组件渲染失败: ` + template.HTMLEscapeString(msg) + `</div>`)
}

func decodeNode(body []byte) (*yaml.Node, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(body, &node); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	if len(node.Content) == 0 {
		return nil, ErrEmptyBody
	}
	root := node.Content[0]
	count := 0
	if err := checkNode(root, 1, &count); err != nil {
		return nil, err
	}
	return root, nil
}

func checkNode(node *yaml.Node, depth int, count *int) error {
	*count++
	if *count > maxNodes {
		return ErrTooComplex
	}
	if depth > maxDepth {
		return ErrTooComplex
	}
	if node.Alias != nil {
		return fmt.Errorf("%w: YAML alias is not allowed", ErrInvalidSpec)
	}
	for _, child := range node.Content {
		if err := checkNode(child, depth+1, count); err != nil {
			return err
		}
	}
	return nil
}

func decodeStrict(root *yaml.Node, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(mustMarshal(root)))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	return nil
}

func mustMarshal(node *yaml.Node) []byte {
	data, _ := yaml.Marshal(node)
	return data
}

func renderInfobox(body []byte, lookup EntityLookup) (template.HTML, error) {
	root, err := decodeNode(body)
	if err != nil {
		return "", err
	}
	var spec infoboxSpec
	if err := decodeStrict(root, &spec); err != nil {
		return "", err
	}
	if err := validateInfobox(&spec); err != nil {
		return "", err
	}
	if spec.Entity != "" {
		if lookup == nil {
			return "", specError("entity 引用不可用")
		}
		entity, ok := lookup(spec.Entity)
		if !ok {
			return "", specError("entity %q 不存在", spec.Entity)
		}
		if strings.TrimSpace(spec.Title) == "" {
			spec.Title = entity.Label
		}
		if strings.TrimSpace(spec.Title) == "" {
			spec.Title = entity.Key
		}
		explicit := make(map[string]bool, len(spec.Rows))
		for _, row := range spec.Rows {
			explicit[row.Label] = true
		}
		rows := make([]infoboxRow, 0, len(entity.Fields)+len(spec.Rows))
		for _, field := range entity.Fields {
			if strings.TrimSpace(field.Value) == "" || explicit[field.Label] {
				continue
			}
			rows = append(rows, infoboxRow{Label: field.Label, Value: field.Value})
		}
		spec.Rows = append(rows, spec.Rows...)
		if len(spec.Rows) > 40 {
			return "", specError("entity 字段和 rows 合计不能超过 40")
		}
	}
	var builder strings.Builder
	if err := infoboxTemplate.Execute(&builder, spec); err != nil {
		return "", err
	}
	return template.HTML(builder.String()), nil
}

type infoboxSpec struct {
	Entity  string       `yaml:"entity"`
	Title   string       `yaml:"title"`
	Image   string       `yaml:"image"`
	Caption string       `yaml:"caption"`
	Rows    []infoboxRow `yaml:"rows"`
	Tags    []string     `yaml:"tags"`
	Footer  string       `yaml:"footer"`
}

type infoboxRow struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
}

func validateInfobox(spec *infoboxSpec) error {
	if spec.Entity != "" && !validEntityKey(spec.Entity) {
		return specError("entity 名称不合法")
	}
	if spec.Entity == "" && strings.TrimSpace(spec.Title) == "" {
		return specError("title 不能为空")
	}
	if utf8.RuneCountInString(spec.Title) > 200 {
		return specError("title 不能超过 200 个字符")
	}
	if spec.Image != "" && !validSitePath(spec.Image) {
		return specError("image 必须是站内绝对路径")
	}
	if len(spec.Rows) > 20 {
		return specError("rows 数量不能超过 20")
	}
	for i, row := range spec.Rows {
		if strings.TrimSpace(row.Label) == "" {
			return specError("rows[%d].label 不能为空", i)
		}
		if strings.TrimSpace(row.Value) == "" {
			return specError("rows[%d].value 不能为空", i)
		}
		if utf8.RuneCountInString(row.Value) > 500 {
			return specError("rows[%d].value 不能超过 500 个字符", i)
		}
	}
	if len(spec.Tags) > 10 {
		return specError("tags 数量不能超过 10")
	}
	return nil
}

func specError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSpec, fmt.Sprintf(format, args...))
}

func validEntityKey(key string) bool {
	if len(key) == 0 || len(key) > 100 {
		return false
	}
	for _, r := range key {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validSitePath(path string) bool {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "\\") {
		return false
	}
	parsed, err := url.Parse(path)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Opaque != "" {
		return false
	}
	if strings.Contains(parsed.Path, "..") {
		return false
	}
	for _, r := range path {
		if r < 0x20 {
			return false
		}
	}
	return true
}

var infoboxTemplate = template.Must(template.New("infobox").Parse(
	`<aside class="wx-infobox">` +
		`{{with .Image}}<img class="wx-infobox-image" src="{{.}}" alt="{{$.Title}}">{{end}}` +
		`<div class="wx-infobox-body">` +
		`<h3 class="wx-infobox-title">{{.Title}}</h3>` +
		`{{with .Caption}}<p class="wx-infobox-caption">{{.}}</p>{{end}}` +
		`{{with .Rows}}<dl class="wx-infobox-rows">{{range .}}<div class="wx-infobox-row"><dt>{{.Label}}</dt><dd>{{.Value}}</dd></div>{{end}}</dl>{{end}}` +
		`{{with .Tags}}<ul class="wx-infobox-tags">{{range .}}<li>{{.}}</li>{{end}}</ul>{{end}}` +
		`{{with .Footer}}<p class="wx-infobox-footer">{{.}}</p>{{end}}` +
		`</div></aside>`))

func renderNotice(body []byte, lookup EntityLookup) (template.HTML, error) {
	root, err := decodeNode(body)
	if err != nil {
		return "", err
	}
	var spec noticeSpec
	if err := decodeStrict(root, &spec); err != nil {
		return "", err
	}
	if err := validateNotice(&spec); err != nil {
		return "", err
	}
	var builder strings.Builder
	if err := noticeTemplate.Execute(&builder, spec); err != nil {
		return "", err
	}
	return template.HTML(builder.String()), nil
}

type noticeSpec struct {
	Type  string `yaml:"type"`
	Title string `yaml:"title"`
	Text  string `yaml:"text"`
}

func validateNotice(spec *noticeSpec) error {
	switch spec.Type {
	case "":
		spec.Type = "info"
	case "info", "warn", "danger":
	default:
		return specError("type 只允许 info/warn/danger")
	}
	if strings.TrimSpace(spec.Text) == "" {
		return specError("text 不能为空")
	}
	if utf8.RuneCountInString(spec.Text) > 2000 {
		return specError("text 不能超过 2000 个字符")
	}
	if utf8.RuneCountInString(spec.Title) > 200 {
		return specError("title 不能超过 200 个字符")
	}
	return nil
}

var noticeTemplate = template.Must(template.New("notice").Parse(
	`<div class="wx-notice wx-notice-{{.Type}}">` +
		`{{with .Title}}<p class="wx-notice-title">{{.}}</p>{{end}}` +
		`<p class="wx-notice-text">{{.Text}}</p></div>`))

func renderGallery(body []byte, lookup EntityLookup) (template.HTML, error) {
	root, err := decodeNode(body)
	if err != nil {
		return "", err
	}
	var spec gallerySpec
	if err := decodeStrict(root, &spec); err != nil {
		return "", err
	}
	if err := validateGallery(&spec); err != nil {
		return "", err
	}
	var builder strings.Builder
	if err := galleryTemplate.Execute(&builder, spec); err != nil {
		return "", err
	}
	return template.HTML(builder.String()), nil
}

type gallerySpec struct {
	Images []galleryImage `yaml:"images"`
}

type galleryImage struct {
	Image   string `yaml:"image"`
	Caption string `yaml:"caption"`
}

func validateGallery(spec *gallerySpec) error {
	if len(spec.Images) == 0 {
		return specError("images 不能为空")
	}
	if len(spec.Images) > 20 {
		return specError("images 数量不能超过 20")
	}
	for i, image := range spec.Images {
		if !validSitePath(image.Image) {
			return specError("images[%d].image 必须是站内绝对路径", i)
		}
		if utf8.RuneCountInString(image.Caption) > 200 {
			return specError("images[%d].caption 不能超过 200 个字符", i)
		}
	}
	return nil
}

var galleryTemplate = template.Must(template.New("gallery").Parse(
	`<div class="wx-gallery">{{range .Images}}` +
		`<figure class="wx-gallery-item"><img src="{{.Image}}" alt="{{.Caption}}">` +
		`{{with .Caption}}<figcaption>{{.}}</figcaption>{{end}}</figure>{{end}}</div>`))

func renderAudio(body []byte, lookup EntityLookup) (template.HTML, error) {
	root, err := decodeNode(body)
	if err != nil {
		return "", err
	}
	var spec audioSpec
	if err := decodeStrict(root, &spec); err != nil {
		return "", err
	}
	if err := validateAudio(&spec); err != nil {
		return "", err
	}
	var builder strings.Builder
	if err := audioTemplate.Execute(&builder, spec); err != nil {
		return "", err
	}
	return template.HTML(builder.String()), nil
}

type audioSpec struct {
	Src     string `yaml:"src"`
	Title   string `yaml:"title"`
	Caption string `yaml:"caption"`
}

func validateAudio(spec *audioSpec) error {
	if strings.TrimSpace(spec.Src) == "" {
		return specError("src 不能为空")
	}
	if !validAudioPath(spec.Src) {
		return specError("src 必须是站内 wav/mp3/ogg/flac 路径")
	}
	if utf8.RuneCountInString(spec.Title) > 200 {
		return specError("title 不能超过 200 个字符")
	}
	if utf8.RuneCountInString(spec.Caption) > 200 {
		return specError("caption 不能超过 200 个字符")
	}
	return nil
}

func validAudioPath(value string) bool {
	if !validSitePath(value) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".wav", ".mp3", ".ogg", ".flac":
		return true
	default:
		return false
	}
}

var audioTemplate = template.Must(template.New("audio").Parse(
	`<figure class="wx-audio">` +
		`<audio controls preload="metadata" src="{{.Src}}">您的浏览器不支持音频播放.</audio>` +
		`{{with .Title}}<figcaption class="wx-audio-title">{{.}}</figcaption>{{end}}` +
		`{{with .Caption}}<figcaption class="wx-audio-caption">{{.}}</figcaption>{{end}}` +
		`</figure>`))

func renderCollapse(body []byte, lookup EntityLookup) (template.HTML, error) {
	root, err := decodeNode(body)
	if err != nil {
		return "", err
	}
	var spec collapseSpec
	if err := decodeStrict(root, &spec); err != nil {
		return "", err
	}
	if err := validateCollapse(&spec); err != nil {
		return "", err
	}
	var builder strings.Builder
	if err := collapseTemplate.Execute(&builder, spec); err != nil {
		return "", err
	}
	return template.HTML(builder.String()), nil
}

type collapseSpec struct {
	Items []collapseItem `yaml:"items"`
}

type collapseItem struct {
	Title string `yaml:"title"`
	Text  string `yaml:"text"`
}

func validateCollapse(spec *collapseSpec) error {
	if len(spec.Items) == 0 {
		return specError("items 不能为空")
	}
	if len(spec.Items) > 20 {
		return specError("items 数量不能超过 20")
	}
	for i, item := range spec.Items {
		if strings.TrimSpace(item.Title) == "" {
			return specError("items[%d].title 不能为空", i)
		}
		if utf8.RuneCountInString(item.Title) > 200 {
			return specError("items[%d].title 不能超过 200 个字符", i)
		}
		if strings.TrimSpace(item.Text) == "" {
			return specError("items[%d].text 不能为空", i)
		}
		if utf8.RuneCountInString(item.Text) > 2000 {
			return specError("items[%d].text 不能超过 2000 个字符", i)
		}
	}
	return nil
}

var collapseTemplate = template.Must(template.New("collapse").Parse(
	`<div class="wx-collapse">{{range .Items}}` +
		`<details class="wx-collapse-item"><summary>{{.Title}}</summary>` +
		`<div class="wx-collapse-body">{{.Text}}</div></details>{{end}}</div>`))
