package webui

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
		not  []string
	}{
		{
			name: "bold and italic",
			in:   "This is **bold** and *italic*.",
			want: []string{"<strong>bold</strong>", "<em>italic</em>"},
		},
		{
			name: "table",
			in:   "| a | b |\n|---|---|\n| 1 | 2 |\n",
			want: []string{"<table>", "<th>a</th>", "<td>2</td>"},
		},
		{
			name: "strikethrough",
			in:   "~gone~",
			want: []string{"<del>gone</del>"},
		},
		{
			name: "task list",
			in:   "- [x] done\n- [ ] todo\n",
			want: []string{"<input", "disabled", "checked"},
		},
		{
			name: "linkify",
			in:   "See https://example.com/docs for details.",
			want: []string{`<a href="https://example.com/docs"`},
		},
		{
			name: "raw html removed",
			in:   "<script>alert(1)</script>",
			want: nil,
			not:  []string{"<script>", "</script>"},
		},
		{
			name: "html in inline code preserved as text",
			in:   "Use `<b>` tags",
			want: []string{"&lt;b&gt;"},
		},
		{
			name: "javascript url neutralized",
			in:   "[click](javascript:alert(1))",
			want: []string{"<a href=\""},
			not:  []string{"javascript:"},
		},
		{
			name: "empty input",
			in:   "   ",
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderMarkdown(tc.in)
			for _, s := range tc.want {
				if !strings.Contains(got, s) {
					t.Errorf("expected %q in output, got %q", s, got)
				}
			}
			for _, s := range tc.not {
				if strings.Contains(got, s) {
					t.Errorf("did not expect %q in output, got %q", s, got)
				}
			}
		})
	}
}

func TestSanitizeColor(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"d73a4a", "#d73a4a"},
		{"D73A4A", "#d73a4a"},
		{"f0f", "#f0f"},
		{"", ""},
		{"red", ""},
		{"#d73a4a", ""},
		{"<script>", ""},
		{"d73a4", ""},
	}
	for _, tc := range cases {
		if got := SanitizeColor(tc.in); got != tc.want {
			t.Errorf("SanitizeColor(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
