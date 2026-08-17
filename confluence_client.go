package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	neturl "net/url"
	"strings"
)

// ConfluenceClient interacts with the Confluence Cloud REST API v2.
type ConfluenceClient struct {
	BaseURL string
	Email   string
	Token   string
	Client  *http.Client
}

// NewConfluenceClient creates a new Confluence Cloud REST API client.
func NewConfluenceClient(baseURL, email, token string) *ConfluenceClient {
	return &ConfluenceClient{
		BaseURL: baseURL,
		Email:   email,
		Token:   token,
		Client:  &http.Client{},
	}
}

// ConfluenceSpace represents a Confluence space.
type ConfluenceSpace struct {
	ID   json.Number `json:"id"`
	Key  string      `json:"key"`
	Name string      `json:"name"`
	Type string      `json:"type"`
}

// SpacesListResponse represents the response from GET /wiki/api/v2/spaces.
type SpacesListResponse struct {
	Results []ConfluenceSpace `json:"results"`
}

// PagesListResponse represents the response from GET /wiki/api/v2/pages.
type PagesListResponse struct {
	Results []ConfluencePage `json:"results"`
}

// ConfluenceVersion represents a page version.
type ConfluenceVersion struct {
	Number int `json:"number"`
}

// ConfluencePage represents a Confluence page.
type ConfluencePage struct {
	ID      string            `json:"id,omitempty"`
	Title   string            `json:"title"`
	Status  string            `json:"status"`
	SpaceID string            `json:"spaceId,omitempty"`
	Version ConfluenceVersion `json:"version,omitempty"`
	Links   ConfluenceLinks   `json:"_links"`
}

// pagesPage is one page of the paginated GET /pages result.
type pagesPage struct {
	Results []ConfluencePage `json:"results"`
	Links   struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// CreatePageRequest is the body for POST /wiki/api/v2/pages.
type CreatePageRequest struct {
	SpaceID  string         `json:"spaceId"`
	Status   string         `json:"status"`
	Title    string         `json:"title"`
	ParentID string         `json:"parentId,omitempty"`
	Body     CreatePageBody `json:"body"`
}

// CreatePageBody contains the page body content.
type CreatePageBody struct {
	Representation string `json:"representation"`
	Value          string `json:"value"`
}

// CreatePageResponse is the response from creating a page.
type CreatePageResponse struct {
	ID      string            `json:"id"`
	Title   string            `json:"title"`
	Status  string            `json:"status"`
	Version ConfluenceVersion `json:"version"`
	Links   ConfluenceLinks   `json:"_links"`

	// Existing is set when the page was already present and was reused rather
	// than created.
	Existing bool `json:"-"`
}

// ConfluenceLinks carries the web UI path of a page.
type ConfluenceLinks struct {
	WebUI string `json:"webui"`
	Base  string `json:"base"`
}

// CreateSpaceRequest is the body for POST /wiki/api/v2/spaces.
type CreateSpaceRequest struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// ConfluenceFolder represents a Confluence folder.
type ConfluenceFolder struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// FoldersListResponse is the response from GET /wiki/api/v2/folders.
type FoldersListResponse struct {
	Results []ConfluenceFolder `json:"results"`
}

// CreateFolderRequest is the body for POST /wiki/api/v2/folders.
type CreateFolderRequest struct {
	SpaceID  string `json:"spaceId"`
	Title    string `json:"title"`
	ParentID string `json:"parentId,omitempty"`
}

func (c *ConfluenceClient) doRequest(method, url string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshalling request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("executing request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body from %s: %w", url, err)
	}

	return respBody, resp.StatusCode, nil
}

// GetSpaceByKey looks up a Confluence space by its key using V1 API (more reliable for single key lookup).
func (c *ConfluenceClient) GetSpaceByKey(key string) (*ConfluenceSpace, error) {
	v1Base := strings.Replace(c.BaseURL, "/api/v2", "", 1)
	url := fmt.Sprintf("%s/rest/api/space/%s", v1Base, key)
	body, status, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if status == http.StatusNotFound {
		return nil, nil // space not found
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d looking up space %s: %s", status, key, string(body))
	}

	var space ConfluenceSpace
	if err := json.Unmarshal(body, &space); err != nil {
		return nil, fmt.Errorf("parsing space response: %w", err)
	}

	return &space, nil
}

// CreateSpace creates a new Confluence space.
func (c *ConfluenceClient) CreateSpace(key, name string) (*ConfluenceSpace, error) {
	url := fmt.Sprintf("%s/api/v2/spaces", c.BaseURL)
	reqBody := CreateSpaceRequest{Key: key, Name: name}

	body, status, err := c.doRequest("POST", url, reqBody)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status %d creating space %s: %s", status, key, string(body))
	}

	var space ConfluenceSpace
	if err := json.Unmarshal(body, &space); err != nil {
		return nil, fmt.Errorf("parsing create space response: %w", err)
	}

	return &space, nil
}

// GetOrCreateSpace gets an existing space by key, or creates it if it doesn't exist.
func (c *ConfluenceClient) GetOrCreateSpace(key, name string) (*ConfluenceSpace, error) {
	space, err := c.GetSpaceByKey(key)
	if err != nil {
		return nil, fmt.Errorf("looking up space: %w", err)
	}

	if space != nil {
		fmt.Printf("  Found existing Confluence space: %s (ID: %s)\n", space.Name, space.ID)
		return space, nil
	}

	fmt.Printf("  Creating new Confluence space: %s (%s)\n", name, key)
	space, err = c.CreateSpace(key, name)
	if err != nil {
		return nil, fmt.Errorf("creating space: %w", err)
	}

	fmt.Printf("  Created Confluence space: %s (ID: %s)\n", space.Name, space.ID)
	return space, nil
}

// GetPageByTitle looks up a Confluence page by its title in a specific space.
func (c *ConfluenceClient) GetPageByTitle(spaceID, title string) (*ConfluencePage, error) {
	url := fmt.Sprintf("%s/api/v2/pages?spaceId=%s&title=%s",
		c.BaseURL, spaceID, neturl.QueryEscape(title))
	body, status, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d looking up page '%s': %s", status, title, string(body))
	}

	var resp PagesListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing pages search response: %w", err)
	}

	// The spaceId query parameter is not honoured reliably: the endpoint also
	// returns same-titled pages from other spaces. Filter here, otherwise a
	// page from an unrelated space would be adopted as the target.
	for i := range resp.Results {
		if resp.Results[i].SpaceID == spaceID {
			return &resp.Results[i], nil
		}
	}

	return nil, nil // page not found in this space
}

// CreatePage creates a new page in Confluence.
func (c *ConfluenceClient) CreatePage(spaceID, title, storageFormatBody, parentID string) (*CreatePageResponse, error) {
	// First, check if it already exists to avoid 409
	existing, err := c.GetPageByTitle(spaceID, title)
	if err != nil {
		return nil, fmt.Errorf("checking for existing page: %w", err)
	}
	if existing != nil {
		return &CreatePageResponse{
			ID:       existing.ID,
			Title:    existing.Title,
			Status:   existing.Status,
			Version:  existing.Version,
			Links:    existing.Links,
			Existing: true,
		}, nil
	}

	url := fmt.Sprintf("%s/api/v2/pages", c.BaseURL)
	reqBody := CreatePageRequest{
		SpaceID: spaceID,
		Status:  "current",
		Title:   title,
		Body: CreatePageBody{
			Representation: "storage",
			Value:          storageFormatBody,
		},
	}
	if parentID != "" {
		reqBody.ParentID = parentID
	}

	body, status, err := c.doRequest("POST", url, reqBody)
	if err != nil {
		return nil, err
	}

	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("unexpected status %d creating page '%s': %s", status, title, string(body))
	}

	var page CreatePageResponse
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parsing create page response: %w", err)
	}

	return &page, nil
}

// attachmentListResponse is the V1 response when looking up an attachment.
type attachmentListResponse struct {
	Results []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"results"`
}

// findAttachmentID returns the ID of an existing attachment with that filename.
func (c *ConfluenceClient) findAttachmentID(pageID, filename string) string {
	url := fmt.Sprintf("%s/rest/api/content/%s/child/attachment?filename=%s",
		c.BaseURL, pageID, neturl.QueryEscape(filename))
	body, status, err := c.doRequest("GET", url, nil)
	if err != nil || status != http.StatusOK {
		return ""
	}

	var resp attachmentListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	for _, a := range resp.Results {
		if a.Title == filename {
			return a.ID
		}
	}
	return ""
}

// UploadAttachment uploads a file attachment to a Confluence page. An
// attachment that is already present is replaced with a new version, because
// Confluence rejects a second upload under the same name.
func (c *ConfluenceClient) UploadAttachment(pageID, filename string, data []byte) error {
	// Use V1 API for attachments as V2 can be unreliable for this specific operation
	url := fmt.Sprintf("%s/rest/api/content/%s/child/attachment", c.BaseURL, pageID)
	if existingID := c.findAttachmentID(pageID, filename); existingID != "" {
		url = fmt.Sprintf("%s/rest/api/content/%s/child/attachment/%s/data",
			c.BaseURL, pageID, existingID)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("writing data to part: %w", err)
	}

	// Add comment if needed (optional)
	// _ = writer.WriteField("comment", "Migrated from xWiki")

	err = writer.Close()
	if err != nil {
		return fmt.Errorf("closing multipart writer: %w", err)
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.SetBasicAuth(c.Email, c.Token)
	req.Header.Set("X-Atlassian-Token", "nocheck")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("executing request to %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status %d uploading attachment '%s' to page %s: %s", resp.StatusCode, filename, pageID, string(respBody))
	}

	return nil
}

// AddLabel adds a label to a Confluence page.
func (c *ConfluenceClient) AddLabel(pageID, label string) error {
	// Use V1 API for labels as it's more widely supported for this operation
	url := fmt.Sprintf("%s/rest/api/content/%s/label", c.BaseURL, pageID)
	reqBody := []map[string]string{
		{
			"prefix": "global",
			"name":   strings.ReplaceAll(label, " ", "-"),
		},
	}
	_, status, err := c.doRequest("POST", url, reqBody)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("unexpected status %d adding label '%s' to page %s", status, label, pageID)
	}
	return nil
}

// footerCommentsResponse is the V2 response for a page's footer comments.
type footerCommentsResponse struct {
	Results []struct {
		Body struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	} `json:"results"`
}

// ListFooterCommentBodies returns the storage bodies of a page's comments. It
// lets the import skip comments it already migrated, so repeated runs do not
// pile up duplicates.
func (c *ConfluenceClient) ListFooterCommentBodies(pageID string) (map[string]bool, error) {
	url := fmt.Sprintf("%s/api/v2/pages/%s/footer-comments?body-format=storage&limit=250",
		c.BaseURL, pageID)
	body, status, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d listing comments of page %s", status, pageID)
	}

	var resp footerCommentsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	bodies := make(map[string]bool, len(resp.Results))
	for _, r := range resp.Results {
		bodies[normalizeCommentBody(r.Body.Storage.Value)] = true
	}
	return bodies, nil
}

// normalizeCommentBody removes the formatting differences Confluence
// introduces when storing a comment, so comparisons stay reliable.
func normalizeCommentBody(body string) string {
	body = strings.ReplaceAll(body, "&nbsp;", " ")
	body = strings.ReplaceAll(body, " ", " ")
	return strings.Join(strings.Fields(body), " ")
}

// AddComment adds a footer comment to a Confluence page.
func (c *ConfluenceClient) AddComment(pageID, body string) error {
	// Using V1 API for comments
	url := fmt.Sprintf("%s/rest/api/content", c.BaseURL)
	reqBody := map[string]interface{}{
		"type": "comment",
		"container": map[string]string{
			"id":   pageID,
			"type": "page",
		},
		"body": map[string]interface{}{
			"storage": map[string]string{
				"value":          body,
				"representation": "storage",
			},
		},
	}
	_, status, err := c.doRequest("POST", url, reqBody)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("unexpected status %d adding comment to page %s", status, pageID)
	}
	return nil
}

// GetOrCreatePage returns the page with the given title, creating it when it
// does not exist yet. Used for the "Import" root page.
func (c *ConfluenceClient) GetOrCreatePage(spaceID, title, body, parentID string) (*CreatePageResponse, error) {
	return c.CreatePage(spaceID, title, body, parentID)
}

// UpdatePage replaces the body of an existing page, bumping its version.
func (c *ConfluenceClient) UpdatePage(pageID, title, storageBody string, currentVersion int, parentID string) error {
	url := fmt.Sprintf("%s/api/v2/pages/%s", c.BaseURL, pageID)
	req := map[string]interface{}{
		"id":     pageID,
		"status": "current",
		"title":  title,
		"body": map[string]interface{}{
			"representation": "storage",
			"value":          storageBody,
		},
		"version": map[string]interface{}{
			"number":  currentVersion + 1,
			"message": "xWiki-Migration aktualisiert",
		},
	}
	if parentID != "" {
		req["parentId"] = parentID
	}

	body, status, err := c.doRequest("PUT", url, req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("unexpected status %d updating page %s: %s", status, pageID, string(body))
	}
	return nil
}

// siteBase strips the "/wiki" suffix so that relative API links can be resolved.
func (c *ConfluenceClient) siteBase() string {
	if parsed, err := neturl.Parse(c.BaseURL); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return c.BaseURL
}

// PageURL builds the browser URL of a created page.
func (c *ConfluenceClient) PageURL(page *CreatePageResponse) string {
	if page == nil {
		return ""
	}
	if page.Links.WebUI != "" {
		return strings.TrimSuffix(c.BaseURL, "/") + page.Links.WebUI
	}
	return fmt.Sprintf("%s/pages/viewpage.action?pageId=%s", strings.TrimSuffix(c.BaseURL, "/"), page.ID)
}

// userSearchResult is the shape of GET /rest/api/search/user.
type userSearchResult struct {
	Results []struct {
		User struct {
			AccountID   string `json:"accountId"`
			Email       string `json:"email"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"results"`
}

// FindAccountIDByName resolves an xWiki user name or e-mail to a Confluence
// account ID. It returns an empty string when there is no match, which is the
// normal case for users that do not exist in the Cloud site.
func (c *ConfluenceClient) FindAccountIDByName(name string) (string, error) {
	query := strings.TrimSpace(name)
	if query == "" {
		return "", nil
	}

	field := "user.fullname"
	if strings.Contains(query, "@") {
		field = "user.email"
	}
	cql := fmt.Sprintf(`%s~"%s"`, field, strings.ReplaceAll(query, `"`, ""))
	url := fmt.Sprintf("%s/rest/api/search/user?cql=%s&limit=1", c.BaseURL, neturl.QueryEscape(cql))

	body, status, err := c.doRequest("GET", url, nil)
	if err != nil || status != http.StatusOK {
		return "", err
	}

	var resp userSearchResult
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if len(resp.Results) == 0 {
		return "", nil
	}
	return resp.Results[0].User.AccountID, nil
}

// SpacePage is one existing page of the target space together with its labels.
type SpacePage struct {
	ID     string
	Title  string
	Labels []string
}

// contentListResponse is the shape of GET /rest/api/content.
type contentListResponse struct {
	Results []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Metadata struct {
			Labels struct {
				Results []struct {
					Name string `json:"name"`
				} `json:"results"`
			} `json:"labels"`
		} `json:"metadata"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// ListSpacePages returns every page of a space with its labels.
//
// This uses the V1 content endpoint rather than a CQL search on purpose: search
// runs against an index that lags behind writes by minutes, so a migration
// re-run would not see the pages it had just created.
func (c *ConfluenceClient) ListSpacePages(spaceKey string) ([]SpacePage, error) {
	var pages []SpacePage
	url := fmt.Sprintf("%s/rest/api/content?spaceKey=%s&type=page&expand=metadata.labels&limit=200",
		c.BaseURL, neturl.QueryEscape(spaceKey))

	for url != "" {
		body, status, err := c.doRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("unexpected status %d listing content of space %s: %s",
				status, spaceKey, string(body))
		}

		var resp contentListResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parsing content list response: %w", err)
		}

		for _, r := range resp.Results {
			page := SpacePage{ID: r.ID, Title: r.Title}
			for _, l := range r.Metadata.Labels.Results {
				page.Labels = append(page.Labels, l.Name)
			}
			pages = append(pages, page)
		}

		if resp.Links.Next == "" {
			break
		}
		url = strings.TrimSuffix(c.siteBase(), "/") + resp.Links.Next
	}

	return pages, nil
}

// SetPageOwner assigns the page owner, which is the closest Confluence Cloud
// equivalent of the xWiki last editor. Confluence always records the API user
// as the version author, so this is best effort.
func (c *ConfluenceClient) SetPageOwner(pageID, accountID string) error {
	url := fmt.Sprintf("%s/api/v2/pages/%s/owner", c.BaseURL, pageID)
	_, status, err := c.doRequest("PUT", url, map[string]string{"accountId": accountID})
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d setting owner of page %s", status, pageID)
	}
	return nil
}

// CreateFolder creates a new folder in Confluence.
func (c *ConfluenceClient) CreateFolder(spaceID, title, parentID string) (string, error) {
	url := fmt.Sprintf("%s/api/v2/folders", c.BaseURL)
	req := CreateFolderRequest{
		SpaceID:  spaceID,
		Title:    title,
		ParentID: parentID,
	}
	body, _, err := c.doRequest("POST", url, req)
	if err != nil {
		return "", err
	}

	var resp ConfluenceFolder
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// GetFolderByTitle finds a folder by its title in a space.
func (c *ConfluenceClient) GetFolderByTitle(spaceID, title string) (string, error) {
	url := fmt.Sprintf("%s/api/v2/folders?spaceId=%s", c.BaseURL, spaceID)
	body, _, err := c.doRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	var resp FoldersListResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}

	for _, folder := range resp.Results {
		if folder.Title == title {
			return folder.ID, nil
		}
	}
	return "", nil
}

// MovePageToFolder moves a page into a folder using V2 API update.
func (c *ConfluenceClient) MovePageToFolder(pageID string, version int, title, folderID string) error {
	url := fmt.Sprintf("%s/api/v2/pages/%s", c.BaseURL, pageID)
	req := map[string]interface{}{
		"id":         pageID,
		"status":     "current",
		"title":      title,
		"parentId":   folderID,
		"parentType": "folder",
		"version": map[string]interface{}{
			"number":  version + 1,
			"message": "Moving to folder",
		},
	}
	_, status, err := c.doRequest("PUT", url, req)
	if err != nil {
		return err
	}
	if status != 200 && status != 201 {
		return fmt.Errorf("unexpected status %d moving page %s to folder %s", status, pageID, folderID)
	}
	return nil
}
