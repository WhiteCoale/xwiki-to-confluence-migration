package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Migration status values used in the report.
const (
	StatusMigrated    = "migriert"
	StatusNotMigrated = "nicht migriert"
	StatusError       = "Fehler"
	StatusPending     = "nicht importiert"
)

// MigrationResult is one row of the report.
type MigrationResult struct {
	FullName        string
	Title           string
	ConfluenceTitle string
	ConfluenceURL   string
	Status          string
	Reason          string

	Creator     string
	LastEditor  string
	Created     string
	Modified    string
	Version     string
	Hidden      bool
	Tags        []string
	Attachments []string
	Comments    int
	XWikiURL    string

	ExcelFile   string
	ExcelStatus string
	ExcelNote   string
}

// buildPlan decides which exported pages are migrated and produces one result
// row per page, covering pages that are skipped as well - Anforderungen.txt
// requires the non migrated pages to appear in the report too.
func buildPlan(index *ExportIndex, sel *Selection) ([]ExportPage, []MigrationResult) {
	exported := map[string]ExportPage{}
	for _, page := range index.Pages {
		exported[page.FullName] = page
	}

	selected := map[string]bool{}
	reasons := map[string]string{}
	noList := sel.IsEmpty()

	for _, page := range index.Pages {
		entry, listed := sel.ByPage[page.FullName]
		switch {
		case noList:
			selected[page.FullName] = true
		case !listed:
			reasons[page.FullName] = "Nicht in der Eingabeliste enthalten"
		case entry.Decision == DecisionMigrate:
			selected[page.FullName] = true
		case entry.Conflict != "":
			// Rows disagree; the conservative verdict applied, so name the
			// conflict instead of a status the sheet does not consistently show.
			reasons[page.FullName] = "Nicht migriert wegen Statuskonflikt: " + entry.Conflict
		case entry.Decision == DecisionDelete:
			reasons[page.FullName] = fmt.Sprintf("Status %q in der Eingabeliste", entry.RawStatus)
		default:
			reasons[page.FullName] = fmt.Sprintf("Status %q nicht auswertbar", entry.RawStatus)
		}
	}

	// Keep the hierarchy intact: an "Ueberordner" of a migrated page is pulled
	// in unless it was explicitly marked for deletion.
	for _, page := range index.Pages {
		if !selected[page.FullName] {
			continue
		}
		for parent := page.Parent; parent != ""; parent = parentFullName(parent) {
			if selected[parent] {
				break
			}
			ancestor, exists := exported[parent]
			if !exists {
				break
			}
			if entry, listed := sel.ByPage[parent]; listed && entry.Decision == DecisionDelete {
				break // respect the explicit verdict, child attaches further up
			}
			selected[parent] = true
			reasons[parent] = "Automatisch ergaenzt: Elternseite einer migrierten Seite"
			_ = ancestor
		}
	}

	var plan []ExportPage
	var results []MigrationResult

	for _, page := range index.Pages {
		res := MigrationResult{
			FullName:    page.FullName,
			Title:       page.Title,
			Creator:     page.Creator,
			LastEditor:  page.LastEditor,
			Created:     page.Created,
			Modified:    page.Modified,
			Version:     page.Version,
			Hidden:      page.Hidden,
			Tags:        page.Tags,
			Attachments: page.Attachments,
			Comments:    len(page.Comments),
			XWikiURL:    page.XWikiURL,
		}
		if entry, listed := sel.ByPage[page.FullName]; listed {
			res.ExcelFile = entry.SourceFile
			res.ExcelStatus = entry.RawStatus
			res.ExcelNote = strings.TrimSpace(entry.Comment + " " + entry.Conflict)
		}

		if selected[page.FullName] {
			res.Status = StatusPending
			if r, ok := reasons[page.FullName]; ok {
				res.Reason = r
			}
			plan = append(plan, page)
		} else {
			res.Status = StatusNotMigrated
			res.Reason = reasons[page.FullName]
		}
		results = append(results, res)
	}

	// Pages excluded during export (system pages, export errors).
	for _, skipped := range index.Skipped {
		res := MigrationResult{
			FullName: skipped.FullName,
			Title:    skipped.Title,
			Status:   StatusNotMigrated,
			Reason:   skipped.Reason,
			XWikiURL: skipped.XWikiURL,
		}
		if entry, listed := sel.ByPage[skipped.FullName]; listed {
			res.ExcelFile = entry.SourceFile
			res.ExcelStatus = entry.RawStatus
			res.ExcelNote = entry.Comment
		}
		results = append(results, res)
	}

	// Rows of the input lists that match no page in xWiki at all.
	for _, entry := range sel.Entries {
		if entry.Duplicate {
			continue
		}
		if entry.FullName != "" {
			if _, ok := exported[entry.FullName]; ok {
				continue
			}
			if skippedInExport(index, entry.FullName) {
				continue
			}
		}
		reason := "In xWiki nicht gefunden"
		if entry.Conflict != "" {
			reason = entry.Conflict
		}
		results = append(results, MigrationResult{
			FullName:    entry.FullName,
			Title:       entry.Title,
			Status:      StatusNotMigrated,
			Reason:      reason,
			Creator:     entry.Creator,
			LastEditor:  entry.LastEditor,
			Created:     entry.Created,
			Modified:    entry.LastChange,
			XWikiURL:    entry.Link,
			ExcelFile:   entry.SourceFile,
			ExcelStatus: entry.RawStatus,
			ExcelNote:   entry.Comment,
		})
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].FullName < results[j].FullName })
	return plan, results
}

func skippedInExport(index *ExportIndex, fullName string) bool {
	for _, s := range index.Skipped {
		if s.FullName == fullName {
			return true
		}
	}
	return false
}

var reportColumns = []string{
	"Status", "Grund / Hinweis", "xWiki-Titel", "Confluence-Titel", "xWiki-Pfad",
	"Ersteller", "Letzter Bearbeiter", "Erstellt", "Zuletzt geaendert", "Version",
	"Versteckt", "Tags", "Anhaenge", "Kommentare", "xWiki-Link", "Confluence-Link",
	"Excel-Datei", "Excel-Status", "Excel-Kommentar",
}

// WriteReport produces step 3: the Excel report over the whole migration.
func WriteReport(path string, results []MigrationResult, sel *Selection) error {
	fmt.Println("[Schritt 3/3] Report erstellen")

	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Migration"
	f.SetSheetName(f.GetSheetName(0), sheet)

	header, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"1F4E79"}},
	})
	if err != nil {
		return err
	}
	migrated, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"C6EFCE"}},
	})
	if err != nil {
		return err
	}
	notMigrated, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFF2CC"}},
	})
	if err != nil {
		return err
	}
	failed, err := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFC7CE"}},
	})
	if err != nil {
		return err
	}

	for i, name := range reportColumns {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, name)
	}
	lastCol, _ := excelize.ColumnNumberToName(len(reportColumns))
	_ = f.SetCellStyle(sheet, "A1", lastCol+"1", header)

	for i, r := range results {
		row := i + 2
		values := []interface{}{
			r.Status, r.Reason, r.Title, r.ConfluenceTitle, r.FullName,
			r.Creator, r.LastEditor, r.Created, r.Modified, r.Version,
			boolText(r.Hidden), strings.Join(r.Tags, ", "), strings.Join(r.Attachments, ", "),
			r.Comments, r.XWikiURL, r.ConfluenceURL,
			r.ExcelFile, r.ExcelStatus, r.ExcelNote,
		}
		for c, v := range values {
			cell, _ := excelize.CoordinatesToCellName(c+1, row)
			_ = f.SetCellValue(sheet, cell, v)
		}

		style := notMigrated
		switch r.Status {
		case StatusMigrated:
			style = migrated
		case StatusError:
			style = failed
		}
		first, _ := excelize.CoordinatesToCellName(1, row)
		last, _ := excelize.CoordinatesToCellName(len(reportColumns), row)
		_ = f.SetCellStyle(sheet, first, last, style)
	}

	widths := []float64{16, 46, 44, 44, 46, 26, 26, 20, 20, 9, 11, 30, 30, 12, 56, 56, 22, 14, 40}
	for i, w := range widths {
		col, _ := excelize.ColumnNumberToName(i + 1)
		_ = f.SetColWidth(sheet, col, col, w)
	}
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze: true, Split: false, XSplit: 0, YSplit: 1,
		TopLeftCell: "A2", ActivePane: "bottomLeft",
	})
	if len(results) > 0 {
		_ = f.AutoFilter(sheet, fmt.Sprintf("A1:%s%d", lastCol, len(results)+1), nil)
	}

	if err := writeSummarySheet(f, results, sel); err != nil {
		return err
	}

	if err := f.SaveAs(path); err != nil {
		return err
	}

	counts := countByStatus(results)
	fmt.Printf("  %s geschrieben (%d Zeilen: %d migriert, %d nicht migriert, %d Fehler)\n",
		path, len(results), counts[StatusMigrated], counts[StatusNotMigrated], counts[StatusError])
	return nil
}

func writeSummarySheet(f *excelize.File, results []MigrationResult, sel *Selection) error {
	const sheet = "Zusammenfassung"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	counts := countByStatus(results)
	rows := [][]interface{}{
		{"Kennzahl", "Wert"},
		{"Seiten gesamt im Report", len(results)},
		{"Migriert", counts[StatusMigrated]},
		{"Nicht migriert", counts[StatusNotMigrated]},
		{"Fehler", counts[StatusError]},
		{"Noch nicht importiert", counts[StatusPending]},
		{"Eingelesene Excel-Dateien", strings.Join(sel.Files, ", ")},
		{"Zeilen in den Eingabelisten", len(sel.Entries)},
	}
	for i, row := range rows {
		for c, v := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, i+1)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}
	_ = f.SetColWidth(sheet, "A", "A", 34)
	_ = f.SetColWidth(sheet, "B", "B", 60)
	return nil
}

func countByStatus(results []MigrationResult) map[string]int {
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Status]++
	}
	return counts
}

func boolText(b bool) string {
	if b {
		return "ja"
	}
	return "nein"
}
