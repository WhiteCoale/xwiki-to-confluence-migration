package main

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConvertExportedPagesIsWellFormed converts every locally exported page and
// checks the result parses as XML. Confluence rejects a malformed storage body,
// so this catches conversion regressions before they reach the API.
//
// The test is skipped when no export has been produced yet.
func TestConvertExportedPagesIsWellFormed(t *testing.T) {
	index, err := LoadExportIndex("./export")
	if err != nil {
		t.Skip("no local export available:", err)
	}

	converter := &StorageConverter{}
	for _, page := range index.Pages {
		raw, err := os.ReadFile(filepath.Join("./export", "pages", page.Dir, "content.html"))
		if err != nil {
			t.Errorf("%s: %v", page.FullName, err)
			continue
		}

		body, err := converter.Convert(string(raw))
		if err != nil {
			t.Errorf("%s: convert: %v", page.FullName, err)
			continue
		}

		full := originPanel(page) + body + revisionHistory(page)
		if err := checkWellFormed(full); err != nil {
			t.Errorf("%s: storage body is not well formed: %v", page.FullName, err)
		}
	}
}

// checkWellFormed parses the fragment inside a wrapper that declares the
// Confluence namespaces used by the storage format.
func checkWellFormed(fragment string) error {
	wrapped := `<root xmlns:ac="http://atlassian.com/content" xmlns:ri="http://atlassian.com/resource/identifier">` +
		fragment + `</root>`

	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	decoder.Strict = true
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
