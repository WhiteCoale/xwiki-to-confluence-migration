package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// XWikiClient interacts with the xWiki REST API.
type XWikiClient struct {
	BaseURL  string
	Username string
	Password string
	Client   *http.Client
}

// NewXWikiClient creates a new xWiki REST API client.
func NewXWikiClient(baseURL, username, password string) *XWikiClient {
	return &XWikiClient{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		Client:   &http.Client{},
	}
}

// SpacesResponse represents the xWiki REST API response for spaces.
type SpacesResponse struct {
	Spaces []SpaceEntry `json:"spaces"`
}

// SpaceEntry represents a single space in xWiki.
type SpaceEntry struct {
	ID   string `json:"id"`
	Wiki string `json:"wiki"`
	Name string `json:"name"`
	Home string `json:"home"`
	XWikiRelativeURL string `json:"xwikiRelativeUrl"`
}

// PagesResponse represents the xWiki REST API response for pages.
type PagesResponse struct {
	PageSummaries []PageSummary `json:"pageSummaries"`
}

// PageSummary represents a page summary in the xWiki pages listing.
type PageSummary struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Wiki     string `json:"wiki"`
	Space    string `json:"space"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	RawTitle string `json:"rawTitle"`
	Parent   string `json:"parent"`
	ParentID string `json:"parentId"`
	Syntax   string `json:"syntax"`
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
}

// HistoryResponse represents the xWiki REST API response for history.
type HistoryResponse struct {
	HistorySummaries []HistorySummary `json:"historySummaries"`
}

// HistorySummary represents a single version in xWiki history.
type HistorySummary struct {
	Version string `json:"version"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

// PageDetail represents the full page detail from xWiki.
func (c *XWikiClient) GetTags(spaceName, pageName string) ([]string, error) {
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages/%s/tags", c.BaseURL, spaceName, pageName)
	body, err := c.doRequest(url)
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
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages/%s/comments", c.BaseURL, spaceName, pageName)
	body, err := c.doRequest(url)
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
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages/%s/history", c.BaseURL, spaceName, pageName)
	body, err := c.doRequest(url)
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
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Wiki     string `json:"wiki"`
	Space    string `json:"space"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	RawTitle string `json:"rawTitle"`
	Parent   string `json:"parent"`
	ParentID string `json:"parentId"`
	Syntax   string `json:"syntax"`
	Content  string `json:"content"`
	XWikiRelativeURL string `json:"xwikiRelativeUrl"`
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
		return nil, fmt.Errorf("unexpected status %d from %s: %s", resp.StatusCode, url, string(body))
	}

	return body, nil
}

// GetSpaces retrieves all spaces from the xWiki instance.
func (c *XWikiClient) GetSpaces() ([]SpaceEntry, error) {
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces", c.BaseURL)
	body, err := c.doRequest(url)
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
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages", c.BaseURL, spaceName)
	body, err := c.doRequest(url)
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
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages/%s", c.BaseURL, spaceName, pageName)
	body, err := c.doRequest(url)
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
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages/%s/attachments", c.BaseURL, spaceName, pageName)
	body, err := c.doRequest(url)
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
	url := fmt.Sprintf("%s/rest/wikis/xwiki/spaces/%s/pages/%s/attachments/%s", c.BaseURL, spaceName, pageName, filename)
	return c.doRequest(url)
}
