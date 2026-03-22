package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	// xWiki flags
	xwikiURL := flag.String("xwiki-url", "http://localhost:8080", "xWiki base URL")
	xwikiUser := flag.String("xwiki-user", "Admin", "xWiki username")
	xwikiPassword := flag.String("xwiki-password", "admin", "xWiki password")

	// Confluence flags
	confluenceURL := flag.String("confluence-url", "https://whitecoale.atlassian.net/wiki", "Confluence Cloud base URL")
	confluenceUser := flag.String("confluence-user", "", "Confluence user email (or set CONFLUENCE_USER env var)")
	confluenceToken := flag.String("confluence-token", "", "Confluence API token (or set CONFLUENCE_TOKEN env var)")
	confluenceSpaceKey := flag.String("confluence-space-key", "XWIKI", "Target Confluence space key")
	confluenceSpaceName := flag.String("confluence-space-name", "xWiki Import", "Target Confluence space name (used when creating)")

	// Mode flags
	mode := flag.String("mode", "all", "Migration mode: all, export, import")
	exportDir := flag.String("export-dir", "./export", "Directory for local data storage")

	// Filter flags
	skipSpaces := flag.String("skip-spaces", "XWiki", "Comma-separated list of xWiki spaces to skip (internal spaces)")

	flag.Parse()

	// Resolve Confluence credentials from env if not set by flags
	if *confluenceUser == "" {
		*confluenceUser = os.Getenv("CONFLUENCE_USER")
	}
	if *confluenceToken == "" {
		*confluenceToken = os.Getenv("CONFLUENCE_TOKEN")
		if *confluenceToken == "" {
			if data, err := os.ReadFile("API_KEY.txt"); err == nil {
				*confluenceToken = strings.TrimSpace(string(data))
			}
		}
	}

	actualSkipSet := make(map[string]bool)
	for _, s := range strings.Split(*skipSpaces, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			actualSkipSet[s] = true
		}
	}

	fmt.Println("=== xWiki to Confluence Migration (2-Step) ===")
	fmt.Printf("Mode:       %s\n", *mode)
	fmt.Printf("Export Dir: %s\n", *exportDir)
	fmt.Println()

	if *mode == "all" || *mode == "export" {
		xwiki := NewXWikiClient(*xwikiURL, *xwikiUser, *xwikiPassword)
		if err := runExport(xwiki, *exportDir, actualSkipSet); err != nil {
			fmt.Printf("ERROR during export: %v\n", err)
			os.Exit(1)
		}
	}

	if *mode == "all" || *mode == "import" {
		if *confluenceUser == "" || *confluenceToken == "" {
			fmt.Println("ERROR: Confluence credentials required for import.")
			os.Exit(1)
		}
		confluence := NewConfluenceClient(*confluenceURL, *confluenceUser, *confluenceToken)
		if err := runImport(confluence, *exportDir, *confluenceSpaceKey, *confluenceSpaceName); err != nil {
			fmt.Printf("ERROR during import: %v\n", err)
			os.Exit(1)
		}
	}
}

// ExportPage represents the metadata for a page saved locally.
type ExportPage struct {
	XWikiID     string           `json:"xwiki_id"`
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Parent      string           `json:"parent"`
	Tags        []string         `json:"tags"`
	Comments    []Comment        `json:"comments"`
	History     []HistorySummary `json:"history"`
	Attachments []string         `json:"attachments"`
}

func runExport(xwiki *XWikiClient, exportDir string, skipSet map[string]bool) error {
	fmt.Println("[1/2] Step 1: Exporting from xWiki to local storage...")

	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return err
	}

	spaces, err := xwiki.GetSpaces()
	if err != nil {
		return err
	}

	// Save space list
	spaceJSON, _ := json.MarshalIndent(spaces, "", "  ")
	_ = os.WriteFile(filepath.Join(exportDir, "spaces.json"), spaceJSON, 0644)

	for _, space := range spaces {
		if skipSet[space.Name] {
			fmt.Printf("  Skipping space: %s\n", space.Name)
			continue
		}

		fmt.Printf("  Processing Space: %s\n", space.Name)
		spacePath := filepath.Join(exportDir, space.Name)
		if err := os.MkdirAll(filepath.Join(spacePath, "pages"), 0755); err != nil {
			return err
		}

		pages, err := xwiki.GetPages(space.Name)
		if err != nil {
			fmt.Printf("    Error fetching pages: %v\n", err)
			continue
		}

		// Save page list for the space
		pagesJSON, _ := json.MarshalIndent(pages, "", "  ")
		_ = os.WriteFile(filepath.Join(spacePath, "pages.json"), pagesJSON, 0644)

		for _, pSummary := range pages {
			pageName := pSummary.Name
			fmt.Printf("    Exporting Page: %s ... ", pageName)

			pageDetail, err := xwiki.GetPageContent(space.Name, pageName)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}

			tags, _ := xwiki.GetTags(space.Name, pageName)
			comments, _ := xwiki.GetComments(space.Name, pageName)
			history, _ := xwiki.GetHistory(space.Name, pageName)
			attachments, _ := xwiki.GetAttachments(space.Name, pageName)

			safeName := sanitizeFilename(pageName)
			pageDir := filepath.Join(spacePath, "pages", safeName)
			if err := os.MkdirAll(filepath.Join(pageDir, "attachments"), 0755); err != nil {
				return err
			}

			// Content
			_ = os.WriteFile(filepath.Join(pageDir, "content.html"), []byte(pageDetail.Content), 0644)

			// Metadata
			meta := ExportPage{
				XWikiID:  fmt.Sprintf("%s.%s", space.Name, pageName),
				Name:     pageName,
				Title:    pageDetail.Title,
				Parent:   pageDetail.Parent,
				Tags:     tags,
				Comments: comments,
				History:  history,
			}

			for _, att := range attachments {
				meta.Attachments = append(meta.Attachments, att.Name)
				data, err := xwiki.DownloadAttachment(space.Name, pageName, att.Name)
				if err == nil {
					_ = os.WriteFile(filepath.Join(pageDir, "attachments", att.Name), data, 0644)
				}
			}

			metaJSON, _ := json.MarshalIndent(meta, "", "  ")
			_ = os.WriteFile(filepath.Join(pageDir, "metadata.json"), metaJSON, 0644)

			fmt.Println("OK")
		}
	}

	fmt.Println("  Export finished successfully.")
	return nil
}

func runImport(confluence *ConfluenceClient, exportDir, targetSpaceKey, targetSpaceName string) error {
	fmt.Println("[2/2] Step 2: Importing from local storage to Confluence...")

	// 1. Setup space
	space, err := confluence.GetOrCreateSpace(targetSpaceKey, targetSpaceName)
	if err != nil {
		return err
	}

	// 2. Iterate spaces in export dir
	entries, err := os.ReadDir(exportDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		spaceName := entry.Name()
		spacePath := filepath.Join(exportDir, spaceName)

		// Check if it's a valid space export
		if _, err := os.Stat(filepath.Join(spacePath, "pages.json")); err != nil {
			continue
		}

		fmt.Printf("  Importing Space: %s\n", spaceName)

		// Read page summaries to know what exists
		var pages []PageSummary
		data, _ := os.ReadFile(filepath.Join(spacePath, "pages.json"))
		json.Unmarshal(data, &pages)

		xwikiToConfluenceID := make(map[string]string)
		isFolderMap := make(map[string]bool)

		// Recursive import function
		var importPage func(string) string
		importPage = func(pageName string) string {
			xwikiFullName := fmt.Sprintf("xwiki:%s.%s", spaceName, pageName)
			if id, exists := xwikiToConfluenceID[xwikiFullName]; exists {
				return id
			}

			safeName := sanitizeFilename(pageName)
			pageDir := filepath.Join(spacePath, "pages", safeName)

			var meta ExportPage
			metaData, err := os.ReadFile(filepath.Join(pageDir, "metadata.json"))
			if err != nil {
				return ""
			}
			json.Unmarshal(metaData, &meta)

			content, _ := os.ReadFile(filepath.Join(pageDir, "content.html"))

			fmt.Printf("    Importing Page: %s ... ", pageName)

			// Handle Parent
			parentID := ""
			parentFullName := ""
			if meta.Parent != "" {
				parentRef := meta.Parent
				if !strings.Contains(parentRef, ":") && !strings.Contains(parentRef, ".") {
					parentRef = spaceName + "." + parentRef
				}
				parentFullName = "xwiki:" + strings.TrimPrefix(parentRef, "xwiki:")
				parts := strings.Split(strings.TrimPrefix(parentRef, "xwiki:"), ".")
				if len(parts) == 2 {
					pSpace, pPage := parts[0], parts[1]
					if pSpace == spaceName && pPage != pageName {
						parentID = importPage(pPage)
					}
				}
			}

			// Folder Detection
			shouldBeFolder := strings.Contains(strings.ToLower(meta.Title), "folder") || 
							  strings.Contains(strings.ToLower(meta.Title), "ordner") ||
							  strings.HasPrefix(meta.Name, "Folder") ||
							  pageName == "WebHome"

			pageTitle := fmt.Sprintf("%s - %s", strings.ToUpper(spaceName), meta.Title)
			if meta.Title == "" {
				pageTitle = fmt.Sprintf("%s - %s", strings.ToUpper(spaceName), meta.Name)
			}

			var confluenceID string
			if shouldBeFolder {
				fmt.Printf(" (FOLDER) ...")
				folderID, err := confluence.CreateFolder(space.ID.String(), pageTitle, parentID)
				if err != nil {
					fmt.Printf(" FAILED: %v (falling back to page) ...", err)
					shouldBeFolder = false
				} else {
					confluenceID = folderID
					isFolderMap[xwikiFullName] = true
				}
			}

			if !shouldBeFolder {
				confluenceBody := ConvertXWikiToConfluenceStorage(string(content))
				if len(meta.History) > 1 {
					confluenceBody += "<hr/><p><strong>Revision History (from xWiki):</strong></p><ul>"
					for _, h := range meta.History {
						confluenceBody += fmt.Sprintf("<li>v%s - %s (%s)</li>", h.Version, formatXWikiDate(h.Date), h.Author)
					}
					confluenceBody += "</ul>"
				}

				actualParent := parentID
				if parentFullName != "" && isFolderMap[parentFullName] {
					actualParent = "" 
				}

				created, err := confluence.CreatePage(space.ID.String(), pageTitle, confluenceBody, actualParent)
				if err != nil {
					fmt.Printf(" ERROR: %v\n", err)
					return ""
				}
				confluenceID = created.ID

				if parentFullName != "" && isFolderMap[parentFullName] {
					fmt.Printf(" (MOVING) ...")
					_ = confluence.MovePageToFolder(confluenceID, created.Version.Number, pageTitle, parentID)
				}

				// Metadata
				for _, tag := range meta.Tags {
					_ = confluence.AddLabel(confluenceID, tag)
				}
				for _, comm := range meta.Comments {
					commBody := fmt.Sprintf("<p><strong>%s (%s):</strong></p><p>%s</p>", comm.Author, formatXWikiDate(comm.Date), comm.Text)
					_ = confluence.AddComment(confluenceID, commBody)
				}
				for _, attName := range meta.Attachments {
					attData, err := os.ReadFile(filepath.Join(pageDir, "attachments", attName))
					if err == nil {
						_ = confluence.UploadAttachment(confluenceID, attName, attData)
					}
				}
			}

			fmt.Println("OK")
			xwikiToConfluenceID[xwikiFullName] = confluenceID
			return confluenceID
		}

		for _, p := range pages {
			if p.Name != "WebHome" {
				importPage(p.Name)
			} else {
				// Ensure WebHome is imported first if it's the root
				importPage("WebHome")
			}
		}
	}

	fmt.Println("  Import finished successfully.")
	return nil
}

func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
}

func formatXWikiDate(d interface{}) string {
	if d == nil {
		return "unknown date"
	}
	switch v := d.(type) {
	case string:
		return v
	case float64:
		t := time.UnixMilli(int64(v))
		return t.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}
