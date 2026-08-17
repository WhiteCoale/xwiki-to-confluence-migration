package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// LinkTarget is the result of resolving an href found in rendered xWiki HTML.
type LinkTarget struct {
	// ConfluenceTitle is set when the link points at another migrated page.
	ConfluenceTitle string
	// URL is used otherwise (external links, or pages that were not migrated).
	URL string
}

// LinkResolver maps an href from the xWiki rendering onto its Confluence target.
type LinkResolver func(href string) LinkTarget

// StorageConverter turns rendered xWiki XHTML into Confluence storage format.
type StorageConverter struct {
	// Attachments of the page being converted, used to recognise image sources.
	Attachments map[string]bool
	// ResolveLink may be nil, in which case hrefs are passed through unchanged.
	ResolveLink LinkResolver
}

// passthroughTags are rendered as-is because Confluence storage format accepts
// them directly.
var passthroughTags = map[string]bool{
	"p": true, "br": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"strong": true, "em": true, "b": true, "i": true, "u": true, "s": true,
	"del": true, "ins": true, "sub": true, "sup": true, "code": true,
	"blockquote": true, "ul": true, "ol": true, "li": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
	"span": true,
}

// voidTags must be written self-closing to keep the output well-formed XML.
var voidTags = map[string]bool{"br": true, "hr": true}

// keptAttributes lists the attributes preserved per tag. "style" is kept so
// that colours set in xWiki survive the migration.
var keptAttributes = map[string][]string{
	"span":  {"style"},
	"td":    {"style", "colspan", "rowspan"},
	"th":    {"style", "colspan", "rowspan"},
	"table": {"style"},
	"tr":    {"style"},
	"p":     {"style"},
	"div":   {"style"},
}

// dropSubtreeClasses are decorative wrappers of the xWiki skin.
var dropSubtreeClasses = map[string]bool{
	"sr-only":    true,
	"icon-block": true,
}

var attachmentSrcRe = regexp.MustCompile(`/bin/download/[^?#]*/([^/?#]+)(\?.*)?$`)

// Convert renders the given xWiki HTML fragment as Confluence storage XHTML.
func (c *StorageConverter) Convert(fragment string) (string, error) {
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return "", fmt.Errorf("parsing rendered html: %w", err)
	}

	var sb strings.Builder
	for _, n := range nodes {
		c.renderNode(&sb, n)
	}
	return strings.TrimSpace(sb.String()), nil
}

func classList(n *html.Node) map[string]bool {
	classes := map[string]bool{}
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, cl := range strings.Fields(a.Val) {
				classes[cl] = true
			}
		}
	}
	return classes
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func (c *StorageConverter) renderChildren(sb *strings.Builder, n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.renderNode(sb, child)
	}
}

func (c *StorageConverter) renderNode(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(escapeXMLText(n.Data))
		return
	case html.ElementNode:
		// handled below
	default:
		// Comments, doctypes and the like carry nothing we want to migrate.
		return
	}

	tag := strings.ToLower(n.Data)
	classes := classList(n)

	for cl := range classes {
		if dropSubtreeClasses[cl] {
			return
		}
	}

	switch tag {
	case "script", "style", "noscript", "form", "button", "input":
		return
	case "img":
		c.renderImage(sb, n)
		return
	case "a":
		c.renderLink(sb, n)
		return
	case "pre":
		c.renderCodeMacro(sb, textContent(n), "")
		return
	case "div":
		if c.renderDiv(sb, n, classes) {
			return
		}
		// A plain <div> has no equivalent in storage format; keep its content.
		c.renderChildren(sb, n)
		return
	}

	if !passthroughTags[tag] {
		c.renderChildren(sb, n)
		return
	}

	sb.WriteString("<" + tag)
	for _, key := range keptAttributes[tag] {
		if v := attr(n, key); v != "" {
			sb.WriteString(fmt.Sprintf(` %s="%s"`, key, escapeXMLAttr(v)))
		}
	}
	if voidTags[tag] {
		sb.WriteString("/>")
		return
	}
	sb.WriteString(">")
	c.renderChildren(sb, n)
	sb.WriteString("</" + tag + ">")
}

// renderDiv maps the xWiki box widgets onto Confluence macros. It reports
// whether the node was fully handled.
func (c *StorageConverter) renderDiv(sb *strings.Builder, n *html.Node, classes map[string]bool) bool {
	if !classes["box"] {
		return false
	}

	// Code block: <div class="box"><div class="code">…</div></div>
	if code := findChildWithClass(n, "code"); code != nil {
		c.renderCodeMacro(sb, textContent(code), "")
		return true
	}

	macro := ""
	switch {
	case classes["infomessage"]:
		macro = "info"
	case classes["warningmessage"]:
		macro = "warning"
	case classes["errormessage"]:
		macro = "note"
	case classes["successmessage"]:
		macro = "tip"
	}
	if macro == "" {
		return false
	}

	var body strings.Builder
	c.renderChildren(&body, n)
	inner := strings.TrimSpace(body.String())
	if inner == "" {
		return true
	}
	if !strings.HasPrefix(inner, "<") {
		inner = "<p>" + inner + "</p>"
	}

	sb.WriteString(fmt.Sprintf(
		`<ac:structured-macro ac:name="%s"><ac:rich-text-body>%s</ac:rich-text-body></ac:structured-macro>`,
		macro, inner))
	return true
}

func (c *StorageConverter) renderCodeMacro(sb *strings.Builder, code, language string) {
	code = strings.TrimRight(code, "\n")
	if strings.TrimSpace(code) == "" {
		return
	}

	sb.WriteString(`<ac:structured-macro ac:name="code">`)
	if language != "" {
		sb.WriteString(fmt.Sprintf(`<ac:parameter ac:name="language">%s</ac:parameter>`,
			escapeXMLText(language)))
	}
	// CDATA cannot contain "]]>", so split any occurrence across two sections.
	sb.WriteString(`<ac:plain-text-body><![CDATA[`)
	sb.WriteString(strings.ReplaceAll(code, "]]>", "]]]]><![CDATA[>"))
	sb.WriteString(`]]></ac:plain-text-body></ac:structured-macro>`)
}

func (c *StorageConverter) renderImage(sb *strings.Builder, n *html.Node) {
	src := attr(n, "src")
	if src == "" {
		return
	}
	// Skin icons are not page content.
	if strings.Contains(src, "/resources/icons/") || strings.Contains(src, "/skins/") {
		return
	}

	if m := attachmentSrcRe.FindStringSubmatch(src); m != nil {
		name, err := url.PathUnescape(m[1])
		if err != nil {
			name = m[1]
		}
		sb.WriteString(fmt.Sprintf(
			`<ac:image><ri:attachment ri:filename="%s"/></ac:image>`, escapeXMLAttr(name)))
		return
	}

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		sb.WriteString(fmt.Sprintf(`<ac:image><ri:url ri:value="%s"/></ac:image>`,
			escapeXMLAttr(src)))
	}
}

func (c *StorageConverter) renderLink(sb *strings.Builder, n *html.Node) {
	href := attr(n, "href")

	var label strings.Builder
	c.renderChildren(&label, n)
	text := strings.TrimSpace(label.String())
	if text == "" {
		text = escapeXMLText(href)
	}

	if href == "" {
		sb.WriteString(text)
		return
	}

	target := LinkTarget{URL: href}
	if c.ResolveLink != nil {
		target = c.ResolveLink(href)
	}

	if target.ConfluenceTitle != "" {
		sb.WriteString(`<ac:link><ri:page ri:content-title="` +
			escapeXMLAttr(target.ConfluenceTitle) + `"/>`)
		sb.WriteString(`<ac:plain-text-link-body><![CDATA[` +
			strings.ReplaceAll(stripTags(text), "]]>", "]]]]><![CDATA[>") +
			`]]></ac:plain-text-link-body></ac:link>`)
		return
	}

	if target.URL == "" {
		sb.WriteString(text)
		return
	}
	sb.WriteString(`<a href="` + escapeXMLAttr(target.URL) + `">` + text + `</a>`)
}

func findChildWithClass(n *html.Node, class string) *html.Node {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		if classList(child)[class] {
			return child
		}
	}
	return nil
}

// textContent collects the plain text of a subtree, turning <br/> into newlines.
func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		switch node.Type {
		case html.TextNode:
			sb.WriteString(node.Data)
		case html.ElementNode:
			if strings.EqualFold(node.Data, "br") {
				sb.WriteString("\n")
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return sb.String()
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	return s
}

func escapeXMLText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeXMLAttr(s string) string {
	s = escapeXMLText(s)
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
