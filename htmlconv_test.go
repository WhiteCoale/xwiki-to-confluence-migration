package main

import (
	"strings"
	"testing"
)

func convert(t *testing.T, in string) string {
	t.Helper()
	c := &StorageConverter{}
	out, err := c.Convert(in)
	if err != nil {
		t.Fatalf("Convert(%q) failed: %v", in, err)
	}
	return out
}

func TestConvertKeepsColorAndStructure(t *testing.T) {
	in := `<h2 id="HTest" class="wikigeneratedid"><span>Kompatibilitaet</span></h2>` +
		`<table><tr><th scope="col">&nbsp;Komponente&nbsp;</th></tr>` +
		`<tr><td><strong><span style="color:#2e7d32">freigegeben</span></strong></td></tr></table>`

	out := convert(t, in)

	for _, want := range []string{"<h2>", "<table>", "<th>", "<td>",
		`<span style="color:#2e7d32">`, "<strong>"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\ngot: %s", want, out)
		}
	}
	if strings.Contains(out, "wikigeneratedid") || strings.Contains(out, `scope=`) {
		t.Errorf("skin attributes leaked into output: %s", out)
	}
}

func TestConvertCodeBlockBecomesMacro(t *testing.T) {
	in := `<div class="box"><div class="code">` +
		`<span style="color: #008000; ">Get-Service</span> PTSAgent<br/>Restart-Service</div></div>`

	out := convert(t, in)

	if !strings.Contains(out, `<ac:structured-macro ac:name="code">`) {
		t.Fatalf("expected code macro, got: %s", out)
	}
	if !strings.Contains(out, "Get-Service PTSAgent\nRestart-Service") {
		t.Errorf("code text or line break lost: %s", out)
	}
	if strings.Contains(out, "<span") {
		t.Errorf("highlighting markup leaked into code body: %s", out)
	}
}

func TestConvertMessageBoxesBecomeMacros(t *testing.T) {
	cases := map[string]string{
		"infomessage":    "info",
		"warningmessage": "warning",
		"errormessage":   "note",
	}
	for class, macro := range cases {
		in := `<div class="box ` + class + `">` +
			`<span class="icon-block"><img src="/resources/icons/silk/information.png" alt="Icon"/></span>` +
			`<span class="sr-only">Information</span><div><p>Hinweistext</p></div></div>`

		out := convert(t, in)

		if !strings.Contains(out, `ac:name="`+macro+`"`) {
			t.Errorf("%s: expected %s macro, got: %s", class, macro, out)
		}
		if !strings.Contains(out, "Hinweistext") {
			t.Errorf("%s: body text lost: %s", class, out)
		}
		if strings.Contains(out, "Information<") || strings.Contains(out, "silk") {
			t.Errorf("%s: skin decoration leaked: %s", class, out)
		}
	}
}

func TestConvertAttachmentImage(t *testing.T) {
	in := `<p><img src="/bin/download/Clientthemen/Mainboard%20Tausch/WebHome/mainboard-hinweis.png?rev=1.1" alt="x"/></p>`

	out := convert(t, in)

	want := `<ac:image><ri:attachment ri:filename="mainboard-hinweis.png"/></ac:image>`
	if !strings.Contains(out, want) {
		t.Errorf("expected %s\ngot: %s", want, out)
	}
}

func TestConvertSkipsSkinIcons(t *testing.T) {
	in := `<p><img src="/resources/icons/silk/information.png"/>Text</p>`
	out := convert(t, in)
	if strings.Contains(out, "ac:image") {
		t.Errorf("skin icon should not become an attachment image: %s", out)
	}
}

func TestConvertInternalLinkBecomesPageLink(t *testing.T) {
	c := &StorageConverter{
		ResolveLink: func(href string) LinkTarget {
			if strings.Contains(href, "/bin/view/Serverthemen/") {
				return LinkTarget{ConfluenceTitle: "Toolbox Installationsanleitung"}
			}
			return LinkTarget{URL: href}
		},
	}

	out, err := c.Convert(
		`<p><a href="/bin/view/Serverthemen/Toolbox%20Installationsanleitung/">Toolbox</a> und ` +
			`<a href="https://example.com">extern</a></p>`)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `<ri:page ri:content-title="Toolbox Installationsanleitung"/>`) {
		t.Errorf("internal link not mapped: %s", out)
	}
	if !strings.Contains(out, `<a href="https://example.com">extern</a>`) {
		t.Errorf("external link not preserved: %s", out)
	}
}

func TestConvertEscapesSpecialCharacters(t *testing.T) {
	out := convert(t, `<p>Gr&ouml;&szlig;e &amp; Pr&uuml;fung &lt;wichtig&gt;</p>`)

	if !strings.Contains(out, "Größe &amp; Prüfung &lt;wichtig&gt;") {
		t.Errorf("entities not handled correctly: %s", out)
	}
}

func TestParentFullName(t *testing.T) {
	cases := map[string]string{
		"Clientthemen.Windows.WebHome":      "Clientthemen.WebHome",
		"Clientthemen.WebHome":              "",
		"Serverthemen.Wsus.Server X":        "Serverthemen.Wsus.WebHome",
		"Serverthemen.Wsus.Patchen.WebHome": "Serverthemen.Wsus.WebHome",
	}
	for in, want := range cases {
		if got := parentFullName(in); got != want {
			t.Errorf("parentFullName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyPage(t *testing.T) {
	skip := map[string]bool{"Archiv": true}
	cases := map[string]SkipReason{
		"Clientthemen.WebHome":           SkipNone,
		"Serverthemen.Wsus.Server X":     SkipNone,
		"PersoenlicheNotizen.Versteckt":  SkipNone,
		"XWiki.XWikiPreferences":         SkipSystemPage,
		"Attachment.Code.MoveAttachment": SkipSystemPage,
		"Crypto.CertificateClass":        SkipSystemPage,
		"Main.WebHome":                   SkipSystemPage,
		"Clientthemen.WebPreferences":    SkipSystemPage,
		"Archiv.Alt":                     SkipConfigured,
	}
	for in, want := range cases {
		if got := classifyPage(in, skip); got != want {
			t.Errorf("classifyPage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeStatus(t *testing.T) {
	cases := map[string]Decision{
		"Migrieren":    DecisionMigrate,
		"  migrieren ": DecisionMigrate,
		"Löschen":      DecisionDelete,
		"Loeschen":     DecisionDelete,
		"":             DecisionUnknown,
		"unklar":       DecisionUnknown,
	}
	for in, want := range cases {
		if got := normalizeStatus(in); got != want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssignTitlesResolvesDuplicates(t *testing.T) {
	pages := []ExportPage{
		{FullName: "Clientthemen.Drucker.Installation", Title: "Installation", Name: "Installation"},
		{FullName: "Serverthemen.Backup.Installation", Title: "Installation", Name: "Installation"},
		{FullName: "Clientthemen.WebHome", Title: "Clientthemen", Name: "WebHome"},
	}
	state := &ImportState{Pages: map[string]ImportedRef{}}

	titles := assignTitles(pages, state, map[string]string{})

	seen := map[string]bool{}
	for _, page := range pages {
		title := titles[page.FullName]
		if title == "" {
			t.Fatalf("no title assigned for %s", page.FullName)
		}
		if seen[strings.ToLower(title)] {
			t.Errorf("duplicate title %q assigned", title)
		}
		seen[strings.ToLower(title)] = true
	}
	if titles["Clientthemen.WebHome"] != "Clientthemen" {
		t.Errorf("unique title should stay unchanged, got %q", titles["Clientthemen.WebHome"])
	}
}

func TestAssignTitlesAvoidsForeignExistingTitle(t *testing.T) {
	pages := []ExportPage{{FullName: "Clientthemen.WebHome", Title: "Clientthemen", Name: "WebHome"}}
	state := &ImportState{Pages: map[string]ImportedRef{}}

	titles := assignTitles(pages, state, map[string]string{"clientthemen": "123"})

	if titles["Clientthemen.WebHome"] == "Clientthemen" {
		t.Error("expected a suffix when the title is already taken in the space")
	}
}

func TestHierarchyDepthOrdersParentsFirst(t *testing.T) {
	cases := map[string]int{
		"Clientthemen.WebHome":              1,
		"Clientthemen.Drucker.WebHome":      2,
		"Clientthemen.Drucker.Installation": 3,
		"PersoenlicheNotizen.WebHome":       1,
		"PersoenlicheNotizen.Versteckt":     2,
	}
	for in, want := range cases {
		if got := hierarchyDepth(in); got != want {
			t.Errorf("hierarchyDepth(%q) = %d, want %d", in, got, want)
		}
	}

	// A child must always sort after its parent.
	for child := range cases {
		parent := parentFullName(child)
		if parent == "" {
			continue
		}
		if hierarchyDepth(parent) >= hierarchyDepth(child) {
			t.Errorf("parent %q not ordered before child %q", parent, child)
		}
	}
}
