package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file
	_ = godotenv.Load()

	// xWiki flags
	xwikiURL := flag.String("xwiki-url", getEnv("XWIKI_URL", "http://localhost:8080"), "xWiki base URL")
	xwikiUser := flag.String("xwiki-user", getEnv("XWIKI_USER", "Admin"), "xWiki username")
	xwikiPassword := flag.String("xwiki-password", getEnv("XWIKI_PASSWORD", "admin"), "xWiki password")

	// Confluence flags
	confluenceURL := flag.String("confluence-url", getEnv("CONFLUENCE_URL", ""), "Confluence Cloud base URL")
	confluenceUser := flag.String("confluence-user", getEnv("CONFLUENCE_USER", ""), "Confluence user email")
	confluenceToken := flag.String("confluence-token", getEnv("CONFLUENCE_TOKEN", ""), "Confluence API token")
	confluenceSpaceKey := flag.String("confluence-space-key", getEnv("CONFLUENCE_SPACE_KEY", ""), "Key of the existing target Confluence space")
	confluenceSpaceName := flag.String("confluence-space-name", getEnv("CONFLUENCE_SPACE_NAME", "xWiki Import"), "Space name, only used together with --create-space")
	rootPageTitle := flag.String("root-page", getEnv("ROOT_PAGE", "Import"), "Title of the page all content is created below")
	createSpace := flag.Bool("create-space", getEnvBool("CREATE_SPACE", false), "Create the target space when it does not exist")

	// Mode flags
	mode := flag.String("mode", getEnv("MODE", "all"), "Migration mode: all, export, import, report")
	exportDir := flag.String("export-dir", getEnv("EXPORT_DIR", "./export"), "Directory for local data storage")
	inputDir := flag.String("input-dir", getEnv("INPUT_DIR", "./input"), "Directory holding the input Excel files")
	reportPath := flag.String("report", getEnv("REPORT", ""), "Path of the Excel report (default: <export-dir>/migration-report-<timestamp>.xlsx)")
	statePath := flag.String("state-file", getEnv("STATE_FILE", DefaultStateFile), "File mapping xWiki pages to the created Confluence pages")

	// Filter flags
	skipSpaces := flag.String("skip-spaces", getEnv("SKIP_SPACES", ""), "Comma-separated list of additional xWiki spaces to skip")

	flag.Parse()

	if *confluenceToken == "" {
		if data, err := os.ReadFile("API_KEY.txt"); err == nil {
			*confluenceToken = strings.TrimSpace(string(data))
		}
	}

	skipSet := make(map[string]bool)
	for _, s := range strings.Split(*skipSpaces, ",") {
		if s = strings.TrimSpace(s); s != "" {
			skipSet[s] = true
		}
	}

	doExport := *mode == "all" || *mode == "export"
	doImport := *mode == "all" || *mode == "import"
	doReport := *mode == "all" || *mode == "import" || *mode == "report"

	fmt.Println("=== xWiki nach Confluence Cloud - Migration ===")
	fmt.Printf("Modus:        %s\n", *mode)
	fmt.Printf("Export-Ordner: %s\n", *exportDir)
	fmt.Printf("Eingabe-Ordner: %s\n", *inputDir)
	fmt.Println()

	// The input lists decide what gets migrated; they are read for every mode so
	// export and report agree on the same data.
	selection, err := LoadSelection(*inputDir)
	if err != nil {
		fmt.Printf("FEHLER beim Lesen der Eingabelisten: %v\n", err)
		os.Exit(1)
	}
	if selection.IsEmpty() {
		fmt.Printf("Keine Excel-Dateien in %s gefunden - es werden alle Inhaltsseiten migriert.\n\n", *inputDir)
	} else {
		fmt.Printf("Eingabelisten: %s (%d Zeilen, %d Seiten)\n\n",
			strings.Join(selection.Files, ", "), len(selection.Entries), len(selection.ByPage))
	}

	if doExport {
		xwiki := NewXWikiClient(*xwikiURL, *xwikiUser, *xwikiPassword)
		if err := runExport(xwiki, *exportDir, skipSet); err != nil {
			fmt.Printf("FEHLER beim Export: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
	}

	index, err := LoadExportIndex(*exportDir)
	if err != nil {
		fmt.Printf("FEHLER: %v\n", err)
		os.Exit(1)
	}

	var results []MigrationResult
	if doImport {
		if *confluenceUser == "" || *confluenceToken == "" || *confluenceURL == "" {
			fmt.Println("FEHLER: CONFLUENCE_URL, CONFLUENCE_USER und CONFLUENCE_TOKEN werden fuer den Import benoetigt.")
			os.Exit(1)
		}
		if *confluenceSpaceKey == "" {
			fmt.Println("FEHLER: CONFLUENCE_SPACE_KEY (Key des bestehenden Ziel-Space) fehlt.")
			os.Exit(1)
		}

		conf := NewConfluenceClient(*confluenceURL, *confluenceUser, *confluenceToken)
		results, err = runImport(conf, index, selection, *exportDir, *statePath,
			*confluenceSpaceKey, *confluenceSpaceName, *rootPageTitle, *createSpace)
		if err != nil {
			fmt.Printf("FEHLER beim Import: %v\n", err)
			os.Exit(1)
		}
		fmt.Println()
	} else if doReport {
		// Report without import: show what would happen.
		_, results = buildPlan(index, selection)
	}

	if doReport {
		path := *reportPath
		if path == "" {
			path = filepath.Join(*exportDir,
				fmt.Sprintf("migration-report-%s.xlsx", time.Now().Format("2006-01-02-1504")))
		}
		if err := WriteReport(path, results, selection); err != nil {
			fmt.Printf("FEHLER beim Report: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("\nFertig.")
}

func formatXWikiDate(d interface{}) string {
	if d == nil {
		return "unbekannt"
	}
	switch v := d.(type) {
	case string:
		return v
	case float64:
		return time.UnixMilli(int64(v)).Format("2006-01-02 15:04:05")
	case int64:
		return time.UnixMilli(v).Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "1", "true", "yes", "ja":
			return true
		case "0", "false", "no", "nein":
			return false
		}
	}
	return fallback
}
