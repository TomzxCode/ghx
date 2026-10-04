package webui

import (
	"bytes"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// mdRenderer converts GitHub-flavoured markdown to HTML. Raw HTML is removed
// (replaced by an HTML comment) because html.WithUnsafe is not enabled, and
// unsafe URL schemes (such as javascript:) are neutralised by goldmark's
// default URL policy.
var mdRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
		extension.Linkify,
		extension.TaskList,
	),
)

// RenderMarkdown converts markdown text to HTML. The input is treated as
// untrusted: raw HTML is removed, not passed through.
func RenderMarkdown(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := mdRenderer.Convert([]byte(src), &buf); err != nil {
		return "<p>" + html.EscapeString(src) + "</p>"
	}
	return buf.String()
}

// hexColorRe matches GitHub label colors: 3 or 6 hex digits, no leading '#'
// (the GraphQL API omits the '#').
var hexColorRe = regexp.MustCompile(`^(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// SanitizeColor normalizes a GitHub label color to a lowercase "#rrggbb"
// (or "#rgb") CSS value, returning "" for anything unsafe so callers can fall
// back to a neutral style.
func SanitizeColor(color string) string {
	if !hexColorRe.MatchString(color) {
		return ""
	}
	return "#" + strings.ToLower(color)
}
