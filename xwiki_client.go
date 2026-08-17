package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// defaultWikiName is the name of the main wiki in a standard installation.
const defaultWikiName = "xwiki"

// XWikiClient interacts with the xWiki REST API.
type XWikiClient struct {
	BaseURL  string
	WikiName string
	Username string
	Password string
	Client   *http.Client

	// userNames caches resolved display names; a wiki has far fewer authors
	// than pages, so every name is looked up only once.
	userNames map[string]string
}

// NewXWikiClient creates a new xWiki REST API client.
func NewXWikiClient(baseURL, wikiName, username, password string) *XWikiClient {
	if wikiName == "" {
		wikiName = defaultWikiName
	}
	return &XWikiClient{
		// A trailing slash would produce "//rest/..." further down.
		BaseURL:   strings.TrimRight(baseURL, "/"),
		WikiName:  wikiName,
		Username:  username,
		Password:  password,
		Client:    &http.Client{},
		userNames: map[string]string{},
	}
}

// WikisResponse is the xWiki REST response listing the wikis of an instance.
type WikisResponse struct {
	Wikis []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"wikis"`
}

// Verify checks that the REST API answers under the configured base URL and
// that the configured wiki exists.
//
// Both mistakes are easy to make and produce a bare 404 otherwise: xWiki is
// usually deployed under a context path (".../xwiki"), and in a wiki farm the
// main wiki may not be called "xwiki".
func (c *XWikiClient) Verify() error {
	body, err := c.doRequest(c.BaseURL + "/rest/wikis")
	if err != nil {
		return fmt.Errorf("xWiki-REST-API unter %s/rest nicht erreichbar.\n"+
			"    Haeufigste Ursache: der Kontextpfad fehlt in XWIKI_URL.\n"+
			"    Beispiel: XWIKI_URL=https://host.example.com/xwiki "+
			"(statt nur https://host.example.com)\n"+
			"    Zum Pruefen im Browser oeffnen: %s/rest/wikis\n"+
			"    Details: %w", c.BaseURL, c.BaseURL, err)
	}

	var resp WikisResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unerwartete Antwort von %s/rest/wikis: %w", c.BaseURL, err)
	}

	names := make([]string, 0, len(resp.Wikis))
	for _, w := range resp.Wikis {
		name := w.Name
		if name == "" {
			name = w.ID
		}
		names = append(names, name)
		if name == c.WikiName {
			return nil
		}
	}

	return fmt.Errorf("Wiki %q existiert nicht auf %s. Vorhanden: %s\n"+
		"    Passenden Namen mit --xwiki-name bzw. XWIKI_NAME setzen.",
		c.WikiName, c.BaseURL, strings.Join(names, ", "))
}

// objectResponse is the xWiki REST representation of a single XObject.
type objectResponse struct {
	ClassName  string `json:"className"`
	Properties []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"properties"`
}

// userRefParts splits a user reference into its space and page name.
// Accepts "xwiki:XWiki.Admin", "XWiki.Admin" and "Admin".
func userRefParts(ref string) (space, name string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	if idx := strings.Index(ref, ":"); idx >= 0 {
		ref = ref[idx+1:]
	}
	if idx := strings.LastIndex(ref, "."); idx >= 0 {
		return ref[:idx], ref[idx+1:]
	}
	return "XWiki", ref
}

// GetUserDisplayName resolves a user reference to "Vorname Nachname" as stored
// in the XWiki.XWikiUsers object of the user profile.
//
// Falls back to the bare user name when the profile has no name filled in or
// cannot be read, so a page never loses its authorship information.
func (c *XWikiClient) GetUserDisplayName(ref string) string {
	space, name := userRefParts(ref)
	if name == "" {
		return ""
	}
	if cached, ok := c.userNames[space+"."+name]; ok {
		return cached
	}

	display := name
	body, err := c.doRequest(c.pageEndpoint(space, name) + "/objects/XWiki.XWikiUsers/0")
	if err == nil {
		var obj objectResponse
		if err := json.Unmarshal(body, &obj); err == nil {
			var first, last string
			for _, p := range obj.Properties {
				switch p.Name {
				case "first_name":
					first = strings.TrimSpace(p.Value)
				case "last_name":
					last = strings.TrimSpace(p.Value)
				}
			}
			if full := strings.TrimSpace(first + " " + last); full != "" {
				display = full
			}
		}
	}

	c.userNames[space+"."+name] = display
	return display
}

// SpacesResponse represents the xWiki REST API response for spaces.
type SpacesResponse struct {
	Spaces []SpaceEntry `json:"spaces"`
}

// SpaceEntry represents a single space in xWiki.
type SpaceEntry struct {
	ID               string `json:"id"`
	Wiki             string `json:"wiki"`
	Name             string `json:"name"`
	Home             string `json:"home"`
	XWikiRelativeURL string `json:"xwikiRelativeUrl"`
}

// PagesResponse represents the xWiki REST API response for pages.
type PagesResponse struct {
	PageSummaries []PageSummary `json:"pageSummaries"`
}

// PageSummary represents a page summary in the xWiki pages listing.
type PageSummary struct {
	ID               string `json:"id"`
	FullName         string `json:"fullName"`
	Wiki             string `json:"wiki"`
	Space            string `json:"space"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	RawTitle         string `json:"rawTitle"`
	Parent           string `json:"parent"`
	ParentID         string `json:"parentId"`
	Syntax           string `json:"syntax"`
	XWikiRelativeURL string `json:"xwikiRelativeUrl"`
}

// AttachmentsResponse represents the xWiki REST API response for attachments.
type AttachmentsResponse struct {
	Attachments []Attachment `json:"attachments"`
}

// Attachment represents a single attachment in xWiki.
type Attachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int    `json:"size"`
	Type      string `json:"type"`
	Version   string `json:"version"`
	PageID    string `json:"pageId"`
	PageTitle string `json:"pageTitle"`
}

// TagsResponse represents the xWiki REST API response for tags.
type TagsResponse struct {
	Tags []Tag `json:"tags"`
}

// Tag represents a single tag in xWiki.
type Tag struct {
	Name string `json:"name"`
}

// CommentsResponse represents the xWiki REST API response for comments.
type CommentsResponse struct {
	Comments []Comment `json:"comments"`
}

// Comment represents a single comment in xWiki.
type Comment struct {
	ID     int         `json:"id"`
	PageID string      `json:"pageId"`
	Author string      `json:"author"`
	Date   interface{} `json:"date"`
	Text   string      `json:"text"`

	// AuthorName is the resolved "Vorname Nachname", filled in during export.
	AuthorName string `json:"authorDisplayName,omitempty"`
}

// HistoryResponse represents the xWiki REST API response for history.
type HistoryResponse struct {
	HistorySummaries []HistorySummary `json:"historySummaries"`
}

// HistorySummary represents a single version in xWiki history.
//
// The history endpoint names its fields "modifier" and "modified" - not
// "author"/"date" as the page endpoint does.
type HistorySummary struct {
	Version  string `json:"version"`
	Author   string `json:"modifier"`
	Modified int64  `json:"modified"` // epoch millis

	// AuthorName is the resolved "Vorname Nachname", filled in during export.
	AuthorName string `json:"authorDisplayName,omitempty"`
}

// PageDetail represents the full page detail from xWiki.
func (c *XWikiClient) GetTags(spaceName, pageName string) ([]string, error) {
	body, err := c.doRequest(c.pageEndpoint(spaceName, pageName) + "/tags")
	if err != nil {
		return nil, err
	}

	var resp TagsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing tags response: %w", err)
	}

	var tags []string
	for _, t := range resp.Tags {
		tags = append(tags, t.Name)
	}
	return tags, nil
}

func (c *XWikiClient) GetComments(spaceName, pageName string) ([]Comment, error) {
	body, err := c.doRequest(c.pageEndpoint(spaceName, pageName) + "/comments")
	if err != nil {
		return nil, err
	}

	var resp CommentsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing comments response: %w", err)
	}

	return resp.Comments, nil
}

func (c *XWikiClient) GetHistory(spaceName, pageName string) ([]HistorySummary, error) {
	body, err := c.doRequest(c.pageEndpoint(spaceName, pageName) + "/history")
	if err != nil {
		return nil, err
	}

	var resp HistoryResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing history response: %w", err)
	}

	return resp.HistorySummaries, nil
}

type PageDetail struct {
	ID               string `json:"id"`
	FullName         string `json:"fullName"`
	Wiki             string `json:"wiki"`
	Space            string `json:"space"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	RawTitle         string `json:"rawTitle"`
	Parent           string `json:"parent"`
	ParentID         string `json:"parentId"`
	Syntax           string `json:"syntax"`
	Content          string `json:"content"`
	XWikiRelativeURL string `json:"xwikiRelativeUrl"`

	// Authorship and lifecycle metadata. Anforderungen.txt requires these to be
	// carried over ("owner = last Editor").
	Version  string `json:"version"`
	Author   string `json:"author"`   // author of the latest version
	Creator  string `json:"creator"`  // original creator
	Modifier string `json:"modifier"` // last editor
	Created  int64  `json:"created"`  // epoch millis
	Modified int64  `json:"modified"` // epoch millis
	Hidden   bool   `json:"hidden"`

	XWikiAbsoluteURL string `json:"xwikiAbsoluteUrl"`
}

// GetAllPages lists every page of the wiki in a single call, including pages in
// nested spaces and pages marked as hidden.
//
// This replaces walking /spaces: that endpoint only reports the last segment of
// a nested space name and omits hidden spaces entirely, so both nested and
// hidden pages were silently dropped.
func (c *XWikiClient) GetAllPages() ([]PageSummary, error) {
	all := []PageSummary{}
	const batch = 500

	for start := 0; ; start += batch {
		url := fmt.Sprintf("%s/rest/wikis/%s/pages?start=%d&number=%d",
			c.BaseURL, c.WikiName, start, batch)
		body, err := c.doRequest(url)
		if err != nil {
			return nil, err
		}

		var resp PagesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing wiki pages response: %w", err)
		}

		all = append(all, resp.PageSummaries...)
		if len(resp.PageSummaries) < batch {
			return all, nil
		}
	}
}

// spacePath turns the dotted space part of a full name ("A.B.C") into the
// nested REST path segment ("spaces/A/spaces/B/spaces/C").
func spacePath(space string) string {
	parts := strings.Split(space, ".")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		segments = append(segments, "spaces/"+url.PathEscape(p))
	}
	return strings.Join(segments, "/")
}

// pageEndpoint builds the REST base URL for one page of a (possibly nested) space.
func (c *XWikiClient) pageEndpoint(space, page string) string {
	return fmt.Sprintf("%s/rest/wikis/%s/%s/pages/%s",
		c.BaseURL, c.WikiName, spacePath(space), url.PathEscape(page))
}

// GetRenderedHTML fetches the page as rendered XHTML.
//
// The stored source is xWiki 2.1 syntax; rendering it in xWiki itself preserves
// formatting, colours, macros, tables and image references far more faithfully
// than re-implementing the syntax parser, which matters for the "moeglichst
// alle Felder uebernehmen" requirement.
func (c *XWikiClient) GetRenderedHTML(space, page string) (string, error) {
	viewPath := make([]string, 0)
	for _, p := range strings.Split(space, ".") {
		viewPath = append(viewPath, url.PathEscape(p))
	}
	viewPath = append(viewPath, url.PathEscape(page))

	// xpage=plain renders the document body without the skin. Do not add
	// outputSyntax=plain: that switches the renderer to plain text and strips
	// every tag, silently losing all formatting.
	u := fmt.Sprintf("%s/bin/view/%s?xpage=plain", c.BaseURL, strings.Join(viewPath, "/"))

	body, err := c.doRequest(u)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *XWikiClient) doRequest(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Accept", "application/json")

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body from %s: %w", url, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s: %s",
			resp.StatusCode, url, shortBody(body))
	}

	return body, nil
}

// shortBody trims an error response for display. Proxies answer with full HTML
// error pages, which would otherwise bury the actual message.
func shortBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if i := strings.Index(text, "<html"); i >= 0 {
		if t := strings.Index(text, "<title>"); t >= 0 {
			if e := strings.Index(text[t:], "</title>"); e > 0 {
				return strings.TrimSpace(text[t+7 : t+e])
			}
		}
		text = text[:i]
	}
	if len(text) > 300 {
		text = text[:300] + " ..."
	}
	return text
}

// GetSpaces retrieves all spaces from the xWiki instance.
func (c *XWikiClient) GetSpaces() ([]SpaceEntry, error) {
	body, err := c.doRequest(fmt.Sprintf("%s/rest/wikis/%s/spaces", c.BaseURL, c.WikiName))
	if err != nil {
		return nil, err
	}

	var resp SpacesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing spaces response: %w", err)
	}

	return resp.Spaces, nil
}

// GetPages retrieves all pages in a given space.
func (c *XWikiClient) GetPages(spaceName string) ([]PageSummary, error) {
	body, err := c.doRequest(fmt.Sprintf("%s/rest/wikis/%s/%s/pages", c.BaseURL, c.WikiName, spacePath(spaceName)))
	if err != nil {
		return nil, err
	}

	var resp PagesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing pages response: %w", err)
	}

	return resp.PageSummaries, nil
}

// GetPageContent retrieves the full content of a page.
func (c *XWikiClient) GetPageContent(spaceName, pageName string) (*PageDetail, error) {
	body, err := c.doRequest(c.pageEndpoint(spaceName, pageName))
	if err != nil {
		return nil, err
	}

	var page PageDetail
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parsing page detail: %w", err)
	}

	return &page, nil
}

// GetAttachments retrieves the list of attachments for a page.
func (c *XWikiClient) GetAttachments(spaceName, pageName string) ([]Attachment, error) {
	body, err := c.doRequest(c.pageEndpoint(spaceName, pageName) + "/attachments")
	if err != nil {
		return nil, err
	}

	var resp AttachmentsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing attachments response: %w", err)
	}

	return resp.Attachments, nil
}

// DownloadAttachment downloads the binary content of an attachment.
func (c *XWikiClient) DownloadAttachment(spaceName, pageName, filename string) ([]byte, error) {
	return c.doRequest(c.pageEndpoint(spaceName, pageName) + "/attachments/" + url.PathEscape(filename))
}
