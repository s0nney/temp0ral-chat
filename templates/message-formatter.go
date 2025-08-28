package templates

import (
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
)

var postReferencePattern = regexp.MustCompile(`>>\d+`)

func PostProcessor(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, ">") && !strings.HasPrefix(trimmedLine, ">>") {
			lines[i] = `<span class="greentext">` + template.HTMLEscapeString(line) + `</span>`
		} else if strings.HasPrefix(trimmedLine, "<") {
			lines[i] = `<span class="bluetext">` + template.HTMLEscapeString(line) + `</span>`
		} else {
			lines[i] = ProcessLineWithReferences(line)
		}
	}

	processedContent := strings.Join(lines, "<br>")
	return processedContent
}

func ProcessLineWithReferences(line string) string {
	var anchors []string
	placeholder := "{{POST_REF}}"

	lineWithPlaceholders := postReferencePattern.ReplaceAllStringFunc(line, func(match string) string {
		postIDStr := match[2:]
		if _, err := strconv.Atoi(postIDStr); err != nil {
			return match
		}
		anchor := `<a href="#p` + html.EscapeString(postIDStr) +
			`" class="post-reference">` + html.EscapeString(match) + `</a>`
		anchors = append(anchors, anchor)
		return placeholder
	})

	escapedLine := template.HTMLEscapeString(lineWithPlaceholders)

	for _, anchor := range anchors {
		escapedLine = strings.Replace(escapedLine, placeholder, anchor, 1)
	}

	return ApplyTextEffects(escapedLine)
}

func ApplyTextEffects(line string) string {
	urlPattern := regexp.MustCompile(`((?:\w+://)[\w\.\/\%\-\:\/\=\#\?\&]+)`) // url formatting
	line = urlPattern.ReplaceAllString(line, "<a href=\"$1\">$1</a>")

	effects := []struct {
		pattern     string
		replacement string
	}{ // various text effects
		{"`(.+?)`", "<code>$1</code>"},
		{`==(.+?)==`, "<span class=\"redtext\">$1</span>"},
		{`%%(.+?)%%`, "<span class=\"spoiler\">$1</span>"},
		{`\*\*(.+?)\*\*`, "<b>$1</b>"},
		{`\*(.+?)\*`, "<i>$1</i>"},
	}

	for _, effect := range effects {
		re := regexp.MustCompile(effect.pattern)
		line = re.ReplaceAllString(line, effect.replacement)
	}

	return line
}
