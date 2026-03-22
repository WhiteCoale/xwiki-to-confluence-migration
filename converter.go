package main

import (
	"fmt"
	"regexp"
	"strings"
)

// ConvertXWikiToConfluenceStorage converts xWiki 2.1 syntax to Confluence storage format (XHTML).
func ConvertXWikiToConfluenceStorage(xwikiContent string) string {
	// Normalize line endings
	content := strings.ReplaceAll(xwikiContent, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	var result strings.Builder
	var i int

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Skip empty lines
		if trimmed == "" {
			i++
			continue
		}

		// Horizontal rule
		if trimmed == "----" {
			result.WriteString("<hr/>\n")
			i++
			continue
		}

		// Headings: = H1 =, == H2 ==, ... ====== H6 ======
		if heading, level, ok := parseHeading(trimmed); ok {
			result.WriteString(fmt.Sprintf("<h%d>%s</h%d>\n", level, convertInlineFormatting(heading), level))
			i++
			continue
		}

		// Code blocks: {{code language="..."}} ... {{/code}}
		if strings.HasPrefix(trimmed, "{{code") {
			codeBlock, endIdx := parseCodeBlock(lines, i)
			result.WriteString(codeBlock)
			i = endIdx + 1
			continue
		}

		// Tables: |=Header| or |Cell|
		if strings.HasPrefix(trimmed, "|") {
			tableHTML, endIdx := parseTable(lines, i)
			result.WriteString(tableHTML)
			i = endIdx
			continue
		}

		// Unordered list: * item, ** sub-item
		if strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "** ") || strings.HasPrefix(trimmed, "*** ") {
			listHTML, endIdx := parseList(lines, i, "*")
			result.WriteString(listHTML)
			i = endIdx
			continue
		}

		// Ordered list: 1. item, 1.1. sub-item
		if isOrderedListItem(trimmed) {
			listHTML, endIdx := parseOrderedList(lines, i)
			result.WriteString(listHTML)
			i = endIdx
			continue
		}

		// Info/Warning/Error macros
		if strings.HasPrefix(trimmed, "{{info}}") || strings.HasPrefix(trimmed, "{{warning}}") || strings.HasPrefix(trimmed, "{{error}}") {
			macroHTML, endIdx := parseMacroBlock(lines, i)
			result.WriteString(macroHTML)
			i = endIdx + 1
			continue
		}

		// Regular paragraph
		var paraLines []string
		for i < len(lines) {
			l := strings.TrimSpace(lines[i])
			if l == "" || strings.HasPrefix(l, "=") || strings.HasPrefix(l, "|") ||
				strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "1.") ||
				strings.HasPrefix(l, "{{") || l == "----" {
				break
			}
			paraLines = append(paraLines, convertInlineFormatting(l))
			i++
		}
		if len(paraLines) > 0 {
			result.WriteString("<p>" + strings.Join(paraLines, " ") + "</p>\n")
		}
	}

	return strings.TrimSpace(result.String())
}

// parseHeading parses an xWiki heading line.
// xWiki: = H1 =, == H2 ==, ... ====== H6 ======
func parseHeading(line string) (string, int, bool) {
	for level := 6; level >= 1; level-- {
		prefix := strings.Repeat("=", level) + " "
		suffix := " " + strings.Repeat("=", level)
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, suffix) {
			heading := line[len(prefix) : len(line)-len(suffix)]
			return strings.TrimSpace(heading), level, true
		}
	}
	return "", 0, false
}

// parseCodeBlock parses a {{code}} ... {{/code}} block.
func parseCodeBlock(lines []string, startIdx int) (string, int) {
	firstLine := strings.TrimSpace(lines[startIdx])

	// Extract language if specified
	lang := ""
	langRe := regexp.MustCompile(`language="([^"]*)"`)
	if matches := langRe.FindStringSubmatch(firstLine); len(matches) > 1 {
		lang = matches[1]
	}

	var codeLines []string
	i := startIdx + 1
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "{{/code}}" {
			break
		}
		codeLines = append(codeLines, escapeHTML(lines[i]))
		i++
	}

	var sb strings.Builder
	sb.WriteString(`<ac:structured-macro ac:name="code">`)
	if lang != "" {
		sb.WriteString(fmt.Sprintf(`<ac:parameter ac:name="language">%s</ac:parameter>`, lang))
	}
	sb.WriteString(`<ac:plain-text-body><![CDATA[`)
	sb.WriteString(strings.Join(codeLines, "\n"))
	sb.WriteString(`]]></ac:plain-text-body></ac:structured-macro>`)
	sb.WriteString("\n")

	return sb.String(), i
}

// parseTable parses xWiki table syntax into HTML table.
func parseTable(lines []string, startIdx int) (string, int) {
	var sb strings.Builder
	sb.WriteString("<table>\n<tbody>\n")

	i := startIdx
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "|") {
			break
		}

		sb.WriteString("<tr>")
		cells := splitTableCells(trimmed)
		for _, cell := range cells {
			cell = strings.TrimSpace(cell)
			if strings.HasPrefix(cell, "=") {
				// Header cell
				header := strings.TrimSpace(strings.TrimPrefix(cell, "="))
				sb.WriteString("<th>" + convertInlineFormatting(header) + "</th>")
			} else {
				sb.WriteString("<td>" + convertInlineFormatting(cell) + "</td>")
			}
		}
		sb.WriteString("</tr>\n")
		i++
	}

	sb.WriteString("</tbody>\n</table>\n")
	return sb.String(), i
}

// splitTableCells splits a table row line into individual cells.
func splitTableCells(line string) []string {
	// Remove leading and trailing |
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	return strings.Split(line, "|")
}

// parseList parses unordered list items.
func parseList(lines []string, startIdx int, marker string) (string, int) {
	var sb strings.Builder
	sb.WriteString("<ul>\n")

	i := startIdx
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "* ") && !strings.HasPrefix(trimmed, "** ") && !strings.HasPrefix(trimmed, "*** ") {
			break
		}

		depth := 0
		for strings.HasPrefix(trimmed, strings.Repeat("*", depth+1)) {
			depth++
		}

		content := strings.TrimSpace(trimmed[depth+1:])
		if depth == 1 {
			sb.WriteString("<li>" + convertInlineFormatting(content) + "</li>\n")
		} else {
			// Nested lists – simplified: just indent
			sb.WriteString("<li>" + convertInlineFormatting(content) + "</li>\n")
		}
		i++
	}

	sb.WriteString("</ul>\n")
	return sb.String(), i
}

// isOrderedListItem checks if a line is an ordered list item (e.g., "1. item").
func isOrderedListItem(line string) bool {
	re := regexp.MustCompile(`^\d+\.\s+`)
	return re.MatchString(line)
}

// parseOrderedList parses ordered list items.
func parseOrderedList(lines []string, startIdx int) (string, int) {
	var sb strings.Builder
	sb.WriteString("<ol>\n")

	i := startIdx
	re := regexp.MustCompile(`^\d+\.\s+(.*)$`)
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		matches := re.FindStringSubmatch(trimmed)
		if matches == nil {
			break
		}
		sb.WriteString("<li>" + convertInlineFormatting(matches[1]) + "</li>\n")
		i++
	}

	sb.WriteString("</ol>\n")
	return sb.String(), i
}

// parseMacroBlock parses info/warning/error macro blocks.
func parseMacroBlock(lines []string, startIdx int) (string, int) {
	firstLine := strings.TrimSpace(lines[startIdx])

	macroName := ""
	if strings.HasPrefix(firstLine, "{{info}}") {
		macroName = "info"
	} else if strings.HasPrefix(firstLine, "{{warning}}") {
		macroName = "warning"
	} else if strings.HasPrefix(firstLine, "{{error}}") {
		macroName = "note"
	}

	endTag := "{{/" + strings.TrimSuffix(strings.TrimPrefix(firstLine, "{{"), "}}") + "}}"
	if macroName == "note" {
		endTag = "{{/error}}"
	}

	var contentLines []string
	i := startIdx + 1
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == endTag {
			break
		}
		contentLines = append(contentLines, lines[i])
		i++
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<ac:structured-macro ac:name="%s">`, macroName))
	sb.WriteString(`<ac:rich-text-body>`)
	sb.WriteString("<p>" + convertInlineFormatting(strings.Join(contentLines, " ")) + "</p>")
	sb.WriteString(`</ac:rich-text-body>`)
	sb.WriteString(`</ac:structured-macro>`)
	sb.WriteString("\n")

	return sb.String(), i
}

// convertInlineFormatting converts inline xWiki formatting to HTML.
func convertInlineFormatting(text string) string {
	// Bold: **text**
	boldRe := regexp.MustCompile(`\*\*(.+?)\*\*`)
	text = boldRe.ReplaceAllString(text, "<strong>$1</strong>")

	// Italic: //text//
	italicRe := regexp.MustCompile(`//(.+?)//`)
	text = italicRe.ReplaceAllString(text, "<em>$1</em>")

	// Strikethrough: --text--
	strikeRe := regexp.MustCompile(`--(.+?)--`)
	text = strikeRe.ReplaceAllString(text, "<del>$1</del>")

	// Monospace: ##text##
	monoRe := regexp.MustCompile(`##(.+?)##`)
	text = monoRe.ReplaceAllString(text, "<code>$1</code>")

	// Superscript: ^^text^^
	superRe := regexp.MustCompile(`\^\^(.+?)\^\^`)
	text = superRe.ReplaceAllString(text, "<sup>$1</sup>")

	// Subscript: ,,text,,
	subRe := regexp.MustCompile(`,,(.+?),,`)
	text = subRe.ReplaceAllString(text, "<sub>$1</sub>")

	// Links: [[label>>url]]
	linkRe := regexp.MustCompile(`\[\[(.+?)>>(.+?)\]\]`)
	text = linkRe.ReplaceAllString(text, `<a href="$2">$1</a>`)

	// Simple links: [[url]]
	simpleLinkRe := regexp.MustCompile(`\[\[([^\]]+?)\]\]`)
	text = simpleLinkRe.ReplaceAllString(text, `<a href="$1">$1</a>`)

	// Images: image:filename
	imageRe := regexp.MustCompile(`image:([^\s|]+)`)
	text = imageRe.ReplaceAllString(text, `<ac:image><ri:attachment ri:filename="$1"/></ac:image>`)

	return text
}

// escapeHTML escapes HTML special characters.
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
