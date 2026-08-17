package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ImportState remembers which Confluence page belongs to which xWiki page, so
// repeated runs update their own pages instead of colliding with them.
type ImportState struct {
	SpaceKey string                 `json:"space_key"`
	RootID   string                 `json:"root_id"`
	Pages    map[string]ImportedRef `json:"pages"` // keyed by xWiki full name
}

// ImportedRef is one previously created Confluence page.
type ImportedRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// DefaultStateFile lives outside the export directory on purpose: a fresh
// export must not discard the mapping to the already created Confluence pages.
const DefaultStateFile = "migration-state.json"

// legacyStateFile is the previous location, still read so existing migrations
// keep working.
const legacyStateFile = "imported.json"

// xwikiLabelPrefix marks the label that identifies a migrated page.
const xwikiLabelPrefix = "xwiki-id-"

// xwikiLabel is the label identifying a migrated page and its origin.
func xwikiLabel(fullName string) string { return xwikiLabelPrefix + shortID(fullName) }

func loadImportState(statePath, exportDir string) *ImportState {
	state := &ImportState{Pages: map[string]ImportedRef{}}

	data, err := os.ReadFile(statePath)
	if err != nil {
		data, err = os.ReadFile(filepath.Join(exportDir, legacyStateFile))
	}
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, state); err != nil {
		return &ImportState{Pages: map[string]ImportedRef{}}
	}
	if state.Pages == nil {
		state.Pages = map[string]ImportedRef{}
	}
	return state
}

func (s *ImportState) save(statePath string) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(statePath, data, 0644)
}

// shortID is the deterministic disambiguator appended to duplicate titles.
func shortID(fullName string) string {
	sum := sha1.Sum([]byte(fullName))
	return hex.EncodeToString(sum[:])[:8]
}

// assignTitles gives every page a title that is unique within the target space.
//
// Confluence rejects duplicate titles per space, so this runs before anything is
// created: colliding titles get a stable id suffix derived from the xWiki
// reference, which keeps re-runs idempotent.
func assignTitles(pages []ExportPage, state *ImportState, taken map[string]string) map[string]string {
	titles := make(map[string]string, len(pages))
	claimed := map[string]string{} // lowercased title -> xWiki full name

	// Pages migrated by an earlier run keep the title they already have.
	for _, page := range pages {
		if ref, ok := state.Pages[page.FullName]; ok && ref.Title != "" {
			titles[page.FullName] = ref.Title
			claimed[strings.ToLower(ref.Title)] = page.FullName
		}
	}

	// Deterministic order so that collisions resolve the same way every run.
	ordered := make([]ExportPage, len(pages))
	copy(ordered, pages)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FullName < ordered[j].FullName })

	for _, page := range ordered {
		if _, done := titles[page.FullName]; done {
			continue
		}

		base := strings.TrimSpace(page.Title)
		if base == "" {
			base = page.Name
		}
		// Confluence rejects a handful of characters in page titles.
		base = strings.Map(func(r rune) rune {
			switch r {
			case ':', ';', '@', '/', '\\', '|', '#', '^', '{', '}', '[', ']', '<', '>':
				return ' '
			}
			return r
		}, base)
		base = strings.Join(strings.Fields(base), " ")
		if base == "" {
			base = page.Name
		}
		if len(base) > 230 {
			base = strings.TrimSpace(base[:230])
		}

		title := base
		key := strings.ToLower(title)
		_, collidesWithOurs := claimed[key]
		otherOwner, existsInSpace := taken[key]
		foreign := existsInSpace && otherOwner != page.FullName

		if collidesWithOurs || foreign {
			title = fmt.Sprintf("%s (%s)", base, shortID(page.FullName))
			key = strings.ToLower(title)
		}

		titles[page.FullName] = title
		claimed[key] = page.FullName
	}

	return titles
}

// runImport performs step 2: create the exported pages in Confluence below a
// page named "Import" inside the existing target space.
func runImport(conf *ConfluenceClient, index *ExportIndex, sel *Selection, exportDir,
	statePath, spaceKey, spaceName, rootTitle string, createSpace bool) ([]MigrationResult, error) {

	fmt.Println("[Schritt 2/3] Import aus lokaler Ablage nach Confluence")

	space, err := conf.GetSpaceByKey(spaceKey)
	if err != nil {
		return nil, fmt.Errorf("looking up space %s: %w", spaceKey, err)
	}
	if space == nil {
		if !createSpace {
			return nil, fmt.Errorf("Confluence space %q existiert nicht "+
				"(mit --create-space anlegen lassen)", spaceKey)
		}
		if space, err = conf.CreateSpace(spaceKey, spaceName); err != nil {
			return nil, fmt.Errorf("creating space %s: %w", spaceKey, err)
		}
	}
	spaceID := space.ID.String()
	fmt.Printf("  Ziel-Space: %s (%s, ID %s)\n", space.Name, space.Key, spaceID)

	root, err := conf.GetOrCreatePage(spaceID, rootTitle,
		"<p>Automatisch angelegte Wurzel der xWiki-Migration.</p>", "")
	if err != nil {
		return nil, fmt.Errorf("creating root page %q: %w", rootTitle, err)
	}
	fmt.Printf("  Wurzelseite: %s (ID %s)\n", rootTitle, root.ID)

	state := loadImportState(statePath, exportDir)
	state.SpaceKey = spaceKey
	state.RootID = root.ID

	// Decide what to migrate before touching Confluence.
	plan, results := buildPlan(index, sel)

	// One call gives both the titles already in use and the pages this
	// migration created earlier, identified by their xWiki label.
	spacePages, err := conf.ListSpacePages(spaceKey)
	if err != nil {
		fmt.Printf("  Hinweis: bestehende Seiten konnten nicht gelesen werden: %v\n", err)
	}

	existing := map[string]string{}     // lowercased title -> page ID
	byLabel := map[string]ImportedRef{} // xWiki label -> page
	for _, sp := range spacePages {
		existing[strings.ToLower(sp.Title)] = sp.ID
		for _, label := range sp.Labels {
			if strings.HasPrefix(label, xwikiLabelPrefix) {
				if _, seen := byLabel[label]; !seen {
					byLabel[label] = ImportedRef{ID: sp.ID, Title: sp.Title}
				}
			}
		}
	}

	// Recover pages missing from the state file, so a lost or deleted state
	// file cannot duplicate the whole tree.
	recovered := 0
	for _, page := range plan {
		if _, known := state.Pages[page.FullName]; known {
			continue
		}
		if ref, ok := byLabel[xwikiLabel(page.FullName)]; ok {
			state.Pages[page.FullName] = ref
			recovered++
		}
	}
	if recovered > 0 {
		fmt.Printf("  %d bereits migrierte Seiten ueber Label wiedergefunden\n", recovered)
		state.save(statePath)
	}

	// Titles created by earlier runs of this migration are ours to reuse.
	for _, ref := range state.Pages {
		delete(existing, strings.ToLower(ref.Title))
	}
	titles := assignTitles(plan, state, existing)

	linkIndex := buildLinkIndex(index, titles)
	byFullName := map[string]ExportPage{}
	for _, page := range index.Pages {
		byFullName[page.FullName] = page
	}
	inPlan := map[string]bool{}
	for _, page := range plan {
		inPlan[page.FullName] = true
	}

	// Parents before children: sorting by depth guarantees it.
	ordered := make([]ExportPage, len(plan))
	copy(ordered, plan)
	sort.Slice(ordered, func(i, j int) bool {
		di, dj := hierarchyDepth(ordered[i].FullName), hierarchyDepth(ordered[j].FullName)
		if di != dj {
			return di < dj
		}
		return ordered[i].FullName < ordered[j].FullName
	})

	resultFor := map[string]*MigrationResult{}
	for i := range results {
		resultFor[results[i].FullName] = &results[i]
	}

	for _, page := range ordered {
		res := resultFor[page.FullName]
		title := titles[page.FullName]

		// Attach to the nearest migrated ancestor, otherwise to the root page.
		parentID := root.ID
		for parent := page.Parent; parent != ""; parent = parentFullName(parent) {
			if ref, ok := state.Pages[parent]; ok && inPlan[parent] {
				parentID = ref.ID
				break
			}
		}

		created, err := importOnePage(conf, exportDir, spaceID, page, title, parentID, linkIndex)
		if err != nil {
			fmt.Printf("    FEHLER %s: %v\n", page.FullName, err)
			res.Status = StatusError
			res.Reason = err.Error()
			continue
		}

		state.Pages[page.FullName] = ImportedRef{
			ID: created.ID, Title: title, URL: created.URL,
		}
		state.save(statePath)

		res.Status = StatusMigrated
		res.ConfluenceTitle = title
		res.ConfluenceURL = created.URL
		if title != page.Title {
			res.Reason = "Titel wegen Namenskonflikt eindeutig gemacht"
		}
	}

	migrated := 0
	for _, r := range results {
		if r.Status == StatusMigrated {
			migrated++
		}
	}
	fmt.Printf("  %d von %d geplanten Seiten migriert\n", migrated, len(plan))

	return results, nil
}

// createdPage is the outcome of importing a single page.
type createdPage struct {
	ID  string
	URL string
}

func importOnePage(conf *ConfluenceClient, exportDir, spaceID string, page ExportPage,
	title, parentID string, linkIndex map[string]string) (*createdPage, error) {

	fmt.Printf("    %s -> %q ... ", page.FullName, title)

	pageDir := filepath.Join(exportDir, "pages", page.Dir)
	rendered, err := os.ReadFile(filepath.Join(pageDir, "content.html"))
	if err != nil {
		return nil, fmt.Errorf("reading exported content: %w", err)
	}

	attachments := map[string]bool{}
	for _, name := range page.Attachments {
		attachments[name] = true
	}

	converter := &StorageConverter{
		Attachments: attachments,
		ResolveLink: func(href string) LinkTarget {
			if title, ok := linkIndex[normalizeViewPath(href)]; ok {
				return LinkTarget{ConfluenceTitle: title}
			}
			return LinkTarget{URL: absoluteXWikiURL(href, page.XWikiURL)}
		},
	}

	body, err := converter.Convert(string(rendered))
	if err != nil {
		return nil, err
	}
	body = originPanel(page) + body + revisionHistory(page)

	created, err := conf.CreatePage(spaceID, title, body, parentID)
	if err != nil {
		return nil, err
	}

	// A page that already existed is updated so re-runs converge.
	if created.Existing {
		if err := conf.UpdatePage(created.ID, title, body, created.Version.Number, parentID); err != nil {
			return nil, fmt.Errorf("updating existing page: %w", err)
		}
	}

	for _, tag := range page.Tags {
		if err := conf.AddLabel(created.ID, tag); err != nil {
			fmt.Printf("(Label %q fehlgeschlagen) ", tag)
		}
	}
	if page.Hidden {
		_ = conf.AddLabel(created.ID, "xwiki-versteckt")
	}
	// Identity marker, used to recover the mapping without the state file.
	_ = conf.AddLabel(created.ID, xwikiLabel(page.FullName))

	for _, name := range page.Attachments {
		data, err := os.ReadFile(filepath.Join(pageDir, "attachments", name))
		if err != nil {
			continue
		}
		if err := conf.UploadAttachment(created.ID, name, data); err != nil {
			fmt.Printf("(Anhang %q fehlgeschlagen) ", name)
		}
	}

	// Comments already migrated by an earlier run are left alone.
	existingComments, err := conf.ListFooterCommentBodies(created.ID)
	if err != nil {
		existingComments = map[string]bool{}
	}
	for _, comment := range page.Comments {
		text := fmt.Sprintf("<p><strong>%s (%s):</strong></p><p>%s</p>",
			escapeXMLText(firstNonEmpty(comment.AuthorName, trimUserRef(comment.Author))),
			escapeXMLText(formatXWikiDate(comment.Date)),
			escapeXMLText(comment.Text))
		if existingComments[normalizeCommentBody(text)] {
			continue
		}
		if err := conf.AddComment(created.ID, text); err != nil {
			fmt.Printf("(Kommentar fehlgeschlagen) ")
		}
	}

	// Confluence records the API user as the author, so the original ownership
	// is applied where the API allows it and always kept visible in the page.
	if page.LastEditor != "" {
		if accountID, err := conf.FindAccountIDByName(page.LastEditor); err == nil && accountID != "" {
			if err := conf.SetPageOwner(created.ID, accountID); err != nil {
				fmt.Printf("(Owner nicht setzbar) ")
			}
		}
	}

	fmt.Println("OK")
	return &createdPage{ID: created.ID, URL: conf.PageURL(created)}, nil
}

// originPanel keeps the xWiki metadata visible on the migrated page. Confluence
// attributes every created page to the API user, so creator, last editor and
// the original timestamps would otherwise be lost.
func originPanel(page ExportPage) string {
	rows := [][2]string{
		{"xWiki-Seite", page.FullName},
		{"Ersteller (xWiki)", page.Creator},
		{"Letzter Bearbeiter (xWiki)", page.LastEditor},
		{"Erstellt", page.Created},
		{"Zuletzt geaendert", page.Modified},
		{"Version", page.Version},
	}
	if page.Hidden {
		rows = append(rows, [2]string{"Sichtbarkeit", "in xWiki versteckt"})
	}
	if len(page.Tags) > 0 {
		rows = append(rows, [2]string{"Tags", strings.Join(page.Tags, ", ")})
	}

	var sb strings.Builder
	sb.WriteString(`<ac:structured-macro ac:name="expand">`)
	sb.WriteString(`<ac:parameter ac:name="title">Herkunft (xWiki-Metadaten)</ac:parameter>`)
	sb.WriteString(`<ac:rich-text-body><table><tbody>`)
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		sb.WriteString("<tr><th>" + escapeXMLText(row[0]) + "</th><td>" +
			escapeXMLText(row[1]) + "</td></tr>")
	}
	if page.XWikiURL != "" {
		sb.WriteString(`<tr><th>Original</th><td><a href="` + escapeXMLAttr(page.XWikiURL) +
			`">` + escapeXMLText(page.XWikiURL) + `</a></td></tr>`)
	}
	sb.WriteString(`</tbody></table></ac:rich-text-body></ac:structured-macro>`)
	return sb.String()
}

func revisionHistory(page ExportPage) string {
	if len(page.History) < 2 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<ac:structured-macro ac:name="expand">`)
	sb.WriteString(`<ac:parameter ac:name="title">Versionshistorie aus xWiki</ac:parameter>`)
	sb.WriteString(`<ac:rich-text-body><ul>`)
	for _, h := range page.History {
		sb.WriteString(fmt.Sprintf("<li>v%s &#8211; %s (%s)</li>",
			escapeXMLText(h.Version), escapeXMLText(formatMillis(h.Modified)),
			escapeXMLText(firstNonEmpty(h.AuthorName, trimUserRef(h.Author)))))
	}
	sb.WriteString(`</ul></ac:rich-text-body></ac:structured-macro>`)
	return sb.String()
}

// buildLinkIndex maps xWiki view paths onto the Confluence titles they became,
// so internal links keep working after the migration.
func buildLinkIndex(index *ExportIndex, titles map[string]string) map[string]string {
	links := map[string]string{}
	for _, page := range index.Pages {
		title, ok := titles[page.FullName]
		if !ok {
			continue
		}
		segments := strings.Split(page.Space, ".")
		if page.Name != "WebHome" {
			segments = append(segments, page.Name)
		}
		links[normalizeViewPath("/bin/view/"+strings.Join(segments, "/"))] = title
	}
	return links
}

// normalizeViewPath reduces an href to a comparable "/bin/view/..." key.
func normalizeViewPath(href string) string {
	if href == "" {
		return ""
	}
	if parsed, err := url.Parse(href); err == nil {
		href = parsed.Path
	}
	if unescaped, err := url.PathUnescape(href); err == nil {
		href = unescaped
	}
	idx := strings.Index(href, "/bin/view/")
	if idx < 0 {
		return ""
	}
	return strings.TrimRight(href[idx:], "/")
}

// absoluteXWikiURL turns a server relative xWiki href into an absolute one so
// links that were not migrated still resolve.
func absoluteXWikiURL(href, pageURL string) string {
	if !strings.HasPrefix(href, "/") {
		return href
	}
	base, err := url.Parse(pageURL)
	if err != nil || base.Host == "" {
		return href
	}
	return base.Scheme + "://" + base.Host + href
}
