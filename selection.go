package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Decision is the migration verdict for one xWiki page, taken from column A of
// an input Excel file.
type Decision string

const (
	DecisionMigrate Decision = "Migrieren"
	DecisionDelete  Decision = "Loeschen"
	DecisionUnknown Decision = "Unklar"
)

// SelectionEntry is one data row of an input Excel file.
type SelectionEntry struct {
	Decision   Decision
	RawStatus  string
	Comment    string
	Title      string
	Creator    string
	Created    string
	LastEditor string
	LastChange string
	Path       string // as written in the sheet, e.g. "Clientthemen/Windows/WebHome"
	Link       string
	FullName   string // xWiki reference, e.g. "Clientthemen.Windows.WebHome"

	SourceFile string
	SourceRow  int

	// Conflict is set when several rows describe the same page with differing
	// decisions. The entry then carries the conservative verdict (Loeschen).
	Conflict string
	// Duplicate marks a repeated row for a page already seen in another row.
	Duplicate bool
}

// Selection is the merged content of every Excel file in the input folder.
type Selection struct {
	// ByPage holds the effective verdict per xWiki reference.
	ByPage map[string]*SelectionEntry
	// Entries keeps every parsed row, including duplicates and unusable ones,
	// so the report can account for all of them.
	Entries []*SelectionEntry
	// Files lists the Excel files that were read.
	Files []string
}

// IsEmpty reports whether no input list was supplied at all.
func (s *Selection) IsEmpty() bool { return s == nil || len(s.Files) == 0 }

// normalizeStatus maps the free text of column A onto a Decision.
func normalizeStatus(raw string) Decision {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "ö", "oe")
	s = strings.ReplaceAll(s, "ä", "ae")
	s = strings.ReplaceAll(s, "ü", "ue")

	switch {
	case s == "":
		return DecisionUnknown
	case strings.HasPrefix(s, "migrieren"), s == "migrate", s == "ja":
		return DecisionMigrate
	case strings.HasPrefix(s, "loeschen"), s == "delete", s == "nein":
		return DecisionDelete
	default:
		return DecisionUnknown
	}
}

// pathToFullName converts an Excel path into an xWiki reference.
func pathToFullName(path string) string {
	p := strings.TrimSpace(path)
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return strings.ReplaceAll(p, "/", ".")
}

// columnIndex resolves the column positions from the header row, falling back
// to the fixed layout of the sample workbook when a header is missing.
func columnIndex(header []string) map[string]int {
	idx := map[string]int{
		"status": 0, "kommentarfeld": 1, "title": 2, "initial_creator": 3,
		"created_date": 4, "last_editor": 5, "last_change_date": 6,
		"path": 7, "link": 8,
	}
	found := map[string]int{}
	for i, cell := range header {
		key := strings.ToLower(strings.TrimSpace(cell))
		if _, ok := idx[key]; ok {
			found[key] = i
		}
	}
	// Only trust the header when the two columns we actually depend on are in it.
	if _, ok := found["status"]; ok {
		if _, ok := found["path"]; ok {
			for k, v := range idx {
				if _, present := found[k]; !present {
					found[k] = v
				}
			}
			return found
		}
	}
	return idx
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// isJunkRow detects rows where every column carries the same value - the sample
// workbook contains such a row and it must not be mistaken for a real page.
func isJunkRow(row []string) bool {
	var first string
	count := 0
	for _, c := range row {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if count == 0 {
			first = c
		} else if c != first {
			return false
		}
		count++
	}
	return count >= 3
}

// LoadSelection reads every .xlsx file in dir and merges them into one Selection.
// A missing directory is not an error: the migration then covers all content pages.
func LoadSelection(dir string) (*Selection, error) {
	sel := &Selection{ByPage: map[string]*SelectionEntry{}}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return sel, nil
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.xlsx"))
	if err != nil {
		return nil, err
	}
	for _, path := range matches {
		if strings.HasPrefix(filepath.Base(path), "~$") {
			continue // Excel lock file
		}
		if err := sel.readFile(path); err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
		}
		sel.Files = append(sel.Files, filepath.Base(path))
	}

	return sel, nil
}

func (s *Selection) readFile(path string) error {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return nil
	}

	idx := columnIndex(rows[0])
	base := filepath.Base(path)

	for i, row := range rows[1:] {
		rowNo := i + 2

		entry := &SelectionEntry{
			RawStatus:  cell(row, idx["status"]),
			Comment:    cell(row, idx["kommentarfeld"]),
			Title:      cell(row, idx["title"]),
			Creator:    cell(row, idx["initial_creator"]),
			Created:    cell(row, idx["created_date"]),
			LastEditor: cell(row, idx["last_editor"]),
			LastChange: cell(row, idx["last_change_date"]),
			Path:       cell(row, idx["path"]),
			Link:       cell(row, idx["link"]),
			SourceFile: base,
			SourceRow:  rowNo,
		}
		entry.Decision = normalizeStatus(entry.RawStatus)
		entry.FullName = pathToFullName(entry.Path)

		if entry.Path == "" && entry.Title == "" && entry.RawStatus == "" {
			continue // empty spacer row
		}
		if isJunkRow(row) {
			entry.Conflict = "Unbrauchbare Zeile: alle Spalten identisch"
			entry.FullName = ""
			s.Entries = append(s.Entries, entry)
			continue
		}
		if entry.FullName == "" {
			entry.Conflict = "Keine Pfadangabe in Spalte 'path'"
			s.Entries = append(s.Entries, entry)
			continue
		}

		s.Entries = append(s.Entries, entry)
		s.merge(entry)
	}

	return nil
}

// merge folds an entry into the per page verdict.
//
// When two rows disagree, the conservative verdict wins: a page is only
// migrated when nothing marks it for deletion. The conflict is recorded so it
// shows up in the report instead of being resolved silently.
func (s *Selection) merge(entry *SelectionEntry) {
	existing, seen := s.ByPage[entry.FullName]
	if !seen {
		copied := *entry
		s.ByPage[entry.FullName] = &copied
		return
	}

	entry.Duplicate = true

	if existing.Decision == entry.Decision {
		existing.Conflict = appendNote(existing.Conflict, fmt.Sprintf(
			"Mehrfach gelistet (%s Zeile %d)", entry.SourceFile, entry.SourceRow))
		return
	}

	note := fmt.Sprintf("Widerspruechlicher Status: '%s' (%s Zeile %d) vs. '%s' (%s Zeile %d)",
		existing.RawStatus, existing.SourceFile, existing.SourceRow,
		entry.RawStatus, entry.SourceFile, entry.SourceRow)

	if entry.Decision == DecisionDelete || existing.Decision == DecisionUnknown {
		existing.Decision = entry.Decision
	}
	if entry.Decision == DecisionDelete {
		existing.Decision = DecisionDelete
	}
	existing.Conflict = appendNote(existing.Conflict, note)
	entry.Conflict = note
}

func appendNote(existing, note string) string {
	if existing == "" {
		return note
	}
	return existing + "; " + note
}
