package util

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TruncateRunes cuts s to at most max bytes without splitting a UTF-8 rune.
func TruncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// SanitizeFilename reduces a client-supplied filename to a safe base name:
// no directories, no control characters, bounded length, extension kept.
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r), r == '/', r == '"', r == '<', r == '>', r == '|', r == '*', r == '?', r == ':':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.Trim(b.String(), " .")
	if name == "" {
		name = "file"
	}
	if len(name) > 120 {
		ext := path.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		name = TruncateRunes(strings.TrimSuffix(name, ext), 120-len(ext)) + ext
	}
	return name
}

// FenceUntrusted wraps third-party text (web pages, uploaded files, tool
// output) in a tag the system prompt tells the model to treat as data only.
// Any closing tag inside the content is neutralised so it can't break out.
func FenceUntrusted(source, content string) string {
	content = strings.ReplaceAll(content, "</untrusted_data", "<\\/untrusted_data")
	source = strings.Map(func(r rune) rune {
		if r == '"' || r == '<' || r == '>' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, source)
	return "<untrusted_data source=\"" + source + "\">\n" + content + "\n</untrusted_data>"
}
