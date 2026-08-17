package main

import "strings"

// systemSpaces are the top level spaces xWiki ships with. They hold
// configuration, classes, UI extensions and other metadata - never user
// content - and are excluded per Anforderungen.txt ("Standard, System,
// Metadaten Seiten von xWiki sollen nicht migriert werden").
var systemSpaces = map[string]bool{
	"Activity":         true,
	"Annotations":      true,
	"AppWithinMinutes": true,
	"Attachment":       true,
	"Blog":             true,
	"CKEditor":         true,
	"ColorThemes":      true,
	"Confluence":       true,
	"Crypto":           true,
	"Dashboard":        true,
	"Filter":           true,
	"FlamingoThemes":   true,
	"Help":             true,
	"Invitation":       true,
	"Mail":             true,
	"Macros":           true,
	"Menu":             true,
	"Model":            true,
	"Notifications":    true,
	"OIDC":             true,
	"PDFExport":        true,
	"Panels":           true,
	"Rendering":        true,
	"Sandbox":          true,
	"Scheduler":        true,
	"Search":           true,
	"Sharing":          true,
	"Sheets":           true,
	"SkinsCode":        true,
	"Stats":            true,
	"SVGPlot":          true,
	"Wiki":             true,
	"WikiManager":      true,
	"XWiki":            true,
	"XWikiSAS":         true,
}

// systemPageNames are technical documents that exist inside otherwise normal
// content spaces.
var systemPageNames = map[string]bool{
	"WebPreferences": true,
	"WebRss":         true,
	"WebSearch":      true,
	"WebSideBar":     true,
	"WebLeftPanels":  true,
	"WebRightPanels": true,
	"WebHorizontal":  true,
}

// systemNameSuffixes mark class, sheet and template documents.
var systemNameSuffixes = []string{
	"Class", "Sheet", "Template", "TemplateProvider", "Translations", "Macros",
}

// SkipReason explains why a page is not migrated. Empty means "migrate".
type SkipReason string

const (
	SkipNone       SkipReason = ""
	SkipSystemPage SkipReason = "System-/Metadatenseite von xWiki"
	SkipConfigured SkipReason = "Space per --skip-spaces ausgeschlossen"
)

// classifyPage decides whether a page is user content worth migrating.
//
// fullName is the dotted xWiki reference, e.g. "Clientthemen.Windows.WebHome".
// extraSkip holds the spaces named via --skip-spaces.
func classifyPage(fullName string, extraSkip map[string]bool) SkipReason {
	parts := strings.Split(fullName, ".")
	if len(parts) < 2 {
		return SkipSystemPage
	}

	spaceSegments := parts[:len(parts)-1]
	name := parts[len(parts)-1]

	// System spaces are reported as such even when they are also listed in
	// --skip-spaces, so the report gives the more informative reason.
	root := spaceSegments[0]
	if systemSpaces[root] {
		return SkipSystemPage
	}
	if extraSkip[root] {
		return SkipConfigured
	}

	// "Code" sub-spaces hold the implementation of applications, e.g.
	// "Attachment.Code.MoveAttachment".
	for _, seg := range spaceSegments {
		if seg == "Code" || systemSpaces[seg] {
			return SkipSystemPage
		}
	}

	if systemPageNames[name] {
		return SkipSystemPage
	}
	for _, suffix := range systemNameSuffixes {
		if name != suffix && strings.HasSuffix(name, suffix) {
			return SkipSystemPage
		}
	}

	// Main.WebHome is the wiki dashboard, not migratable content. Other pages
	// users created below Main are kept.
	if fullName == "Main.WebHome" {
		return SkipSystemPage
	}

	return SkipNone
}

// hierarchyDepth is the nesting level of a page reference, used to create
// parents before their children.
//
// Counting dots is not enough: a terminal page ("A.B.Page") sits one level
// below its space home ("A.B.WebHome") even though both contain two dots.
func hierarchyDepth(fullName string) int {
	parts := strings.Split(fullName, ".")
	if len(parts) < 2 {
		return 0
	}

	segments := len(parts) - 1
	if parts[len(parts)-1] == "WebHome" {
		return segments
	}
	return segments + 1
}

// parentFullName derives the hierarchical parent from a page reference.
//
// xWiki's nested page model is encoded in the reference itself, which is more
// reliable than the legacy "parent" field (frequently empty on modern wikis):
//
//	A.B.C.WebHome  -> A.B.WebHome   (a non-terminal page, i.e. an "Ueberordner")
//	A.B.Page       -> A.B.WebHome   (a terminal page)
//	A.WebHome      -> ""            (top level, attaches to the import root)
func parentFullName(fullName string) string {
	parts := strings.Split(fullName, ".")
	if len(parts) < 2 {
		return ""
	}

	spaceSegments := parts[:len(parts)-1]
	name := parts[len(parts)-1]

	if name == "WebHome" {
		if len(spaceSegments) < 2 {
			return "" // top level space home
		}
		return strings.Join(spaceSegments[:len(spaceSegments)-1], ".") + ".WebHome"
	}

	// Terminal page: its parent is the home of its own space.
	return strings.Join(spaceSegments, ".") + ".WebHome"
}
