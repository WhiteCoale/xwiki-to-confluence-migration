package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExportPage is the metadata of one page saved locally in step 1.
type ExportPage struct {
	FullName string `json:"full_name"` // xWiki reference, e.g. "A.B.WebHome"
	Space    string `json:"space"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Parent   string `json:"parent"` // derived hierarchical parent reference

	Tags        []string         `json:"tags"`
	Comments    []Comment        `json:"comments"`
	History     []HistorySummary `json:"history"`
	Attachments []string         `json:"attachments"`

	Creator    string `json:"creator"`
	LastEditor string `json:"last_editor"`
	Created    string `json:"created"`
	Modified   string `json:"modified"`
	Version    string `json:"version"`
	Hidden     bool   `json:"hidden"`

	XWikiURL string `json:"xwiki_url"`
	Dir      string `json:"dir"` // directory name below <export>/pages
}

// SkippedPage records a page that exists in xWiki but is not exported.
type SkippedPage struct {
	FullName string `json:"full_name"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
	XWikiURL string `json:"xwiki_url"`
}

// ExportIndex is the manifest written next to the exported pages.
type ExportIndex struct {
	XWikiURL   string        `json:"xwiki_url"`
	ExportedAt string        `json:"exported_at"`
	Pages      []ExportPage  `json:"pages"`
	Skipped    []SkippedPage `json:"skipped"`
}

const indexFileName = "index.json"

// pageDirName produces a stable, collision free directory name for a page
// reference. The hash suffix keeps "A.B" and "A-B" apart after sanitising.
func pageDirName(fullName string) string {
	sum := sha1.Sum([]byte(fullName))
	return sanitizeFilename(fullName) + "-" + hex.EncodeToString(sum[:])[:8]
}

func sanitizeFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	return cleaned
}

func formatMillis(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("2006-01-02 15:04:05")
}

// runExport performs step 1: download every content page of the wiki and store
// it locally, so that step 2 can run on a machine with no xWiki access.
func runExport(xwiki *XWikiClient, exportDir string, skipSet map[string]bool) error {
	fmt.Println("[Schritt 1/3] Export aus xWiki in lokale Ablage")

	pagesDir := filepath.Join(exportDir, "pages")
	if err := os.MkdirAll(pagesDir, 0755); err != nil {
		return err
	}

	summaries, err := xwiki.GetAllPages()
	if err != nil {
		return fmt.Errorf("listing wiki pages: %w", err)
	}
	fmt.Printf("  %d Seiten im Wiki gefunden\n", len(summaries))

	index := ExportIndex{
		XWikiURL:   xwiki.BaseURL,
		ExportedAt: time.Now().Format(time.RFC3339),
	}

	for _, summary := range summaries {
		if reason := classifyPage(summary.FullName, skipSet); reason != SkipNone {
			index.Skipped = append(index.Skipped, SkippedPage{
				FullName: summary.FullName,
				Title:    summary.Title,
				Reason:   string(reason),
				XWikiURL: summary.XWikiRelativeURL,
			})
			continue
		}

		if err := exportOnePage(xwiki, pagesDir, summary, &index); err != nil {
			fmt.Printf("    FEHLER %s: %v\n", summary.FullName, err)
			index.Skipped = append(index.Skipped, SkippedPage{
				FullName: summary.FullName,
				Title:    summary.Title,
				Reason:   "Exportfehler: " + err.Error(),
				XWikiURL: summary.XWikiRelativeURL,
			})
		}
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(exportDir, indexFileName), data, 0644); err != nil {
		return err
	}

	fmt.Printf("  %d Seiten exportiert, %d uebersprungen (System-/Metadatenseiten)\n",
		len(index.Pages), len(index.Skipped))
	return nil
}

func exportOnePage(xwiki *XWikiClient, pagesDir string, summary PageSummary, index *ExportIndex) error {
	space, name := summary.Space, summary.Name
	fmt.Printf("    %s ... ", summary.FullName)

	detail, err := xwiki.GetPageContent(space, name)
	if err != nil {
		return err
	}

	dirName := pageDirName(summary.FullName)
	pageDir := filepath.Join(pagesDir, dirName)
	if err := os.MkdirAll(filepath.Join(pageDir, "attachments"), 0755); err != nil {
		return err
	}

	// Rendered HTML is the migration source; the raw syntax is kept alongside
	// it so nothing is lost if the rendering needs to be revisited.
	rendered, err := xwiki.GetRenderedHTML(space, name)
	if err != nil {
		return fmt.Errorf("rendering page: %w", err)
	}
	if err := os.WriteFile(filepath.Join(pageDir, "content.html"), []byte(rendered), 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pageDir, "source.xwiki"), []byte(detail.Content), 0644); err != nil {
		return err
	}

	tags, _ := xwiki.GetTags(space, name)
	comments, _ := xwiki.GetComments(space, name)
	history, _ := xwiki.GetHistory(space, name)
	attachments, _ := xwiki.GetAttachments(space, name)

	// Resolve the user references to "Vorname Nachname" while xWiki is still
	// reachable - the import step runs offline against the export only.
	for i := range comments {
		comments[i].AuthorName = xwiki.GetUserDisplayName(comments[i].Author)
	}
	for i := range history {
		history[i].AuthorName = xwiki.GetUserDisplayName(history[i].Author)
	}

	title := detail.Title
	if title == "" {
		title = summary.Title
	}
	if title == "" {
		title = name
	}

	meta := ExportPage{
		FullName:   summary.FullName,
		Space:      space,
		Name:       name,
		Title:      title,
		Parent:     parentFullName(summary.FullName),
		Tags:       tags,
		Comments:   comments,
		History:    history,
		Creator:    xwiki.GetUserDisplayName(detail.Creator),
		LastEditor: xwiki.GetUserDisplayName(firstNonEmpty(detail.Modifier, detail.Author)),
		Created:    formatMillis(detail.Created),
		Modified:   formatMillis(detail.Modified),
		Version:    detail.Version,
		Hidden:     detail.Hidden,
		XWikiURL:   firstNonEmpty(detail.XWikiAbsoluteURL, detail.XWikiRelativeURL),
		Dir:        dirName,
	}

	for _, att := range attachments {
		data, err := xwiki.DownloadAttachment(space, name, att.Name)
		if err != nil {
			fmt.Printf("(Anhang %s fehlgeschlagen) ", att.Name)
			continue
		}
		if err := os.WriteFile(filepath.Join(pageDir, "attachments", att.Name), data, 0644); err != nil {
			return err
		}
		meta.Attachments = append(meta.Attachments, att.Name)
	}

	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pageDir, "metadata.json"), metaJSON, 0644); err != nil {
		return err
	}

	index.Pages = append(index.Pages, meta)
	hiddenNote := ""
	if meta.Hidden {
		hiddenNote = " (versteckt)"
	}
	fmt.Printf("OK%s\n", hiddenNote)
	return nil
}

// trimUserRef shortens an xWiki user reference ("xwiki:XWiki.Admin") to the
// bare user name.
func trimUserRef(ref string) string {
	if ref == "" {
		return ""
	}
	ref = strings.TrimPrefix(ref, "xwiki:")
	ref = strings.TrimPrefix(ref, "XWiki.")
	return ref
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// LoadExportIndex reads the manifest written by runExport.
func LoadExportIndex(exportDir string) (*ExportIndex, error) {
	data, err := os.ReadFile(filepath.Join(exportDir, indexFileName))
	if err != nil {
		return nil, fmt.Errorf("reading export index (run --mode export first): %w", err)
	}
	var index ExportIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parsing export index: %w", err)
	}
	return &index, nil
}
