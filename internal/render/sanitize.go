package render

import (
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

var alignPattern = regexp.MustCompile(`(?i)^(left|center|right|justify)$`)

func newSanitizer() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.AllowStyling()
	policy.AllowElements("kbd", "input", "audio", "video", "source", "track", "picture")
	policy.AllowAttrs("align").Matching(alignPattern).OnElements("p", "div", "table", "thead", "tbody", "tfoot", "tr", "td", "th", "col", "colgroup")
	policy.AllowAttrs("type").Matching(regexp.MustCompile(`(?i)^checkbox$`)).OnElements("input")
	policy.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`(?i)^(|checked|disabled)$`)).OnElements("input")
	policy.AllowAttrs("controls", "preload", "loop", "muted", "poster", "width", "height").OnElements("audio", "video")
	policy.AllowAttrs("src").OnElements("audio", "video", "source", "track")
	policy.AllowAttrs("type").OnElements("source")
	policy.AllowAttrs("kind", "label", "srclang", "default").OnElements("track")
	policy.AllowAttrs("loading").Matching(regexp.MustCompile(`(?i)^(lazy|eager)$`)).OnElements("img")
	policy.AllowAttrs("start").Matching(regexp.MustCompile(`^[0-9]+$`)).OnElements("ol")
	return policy
}
