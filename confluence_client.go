package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
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
	Version ConfluenceVersion `json:"version,omitempty"`
}

// CreatePageRequest is the body for POST /wiki/api/v2/pages.
type CreatePageRequest struct {
	SpaceID  string          `json:"spaceId"`
	Status   string          `json:"status"`
	Title    string          `json:"title"`
	ParentID string          `json:"parentId,omitempty"`
	Body     CreatePageBody  `json:"body"`
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
	url := fmt.Sprintf("%s/api/v2/pages?spaceId=%s&title=%s", c.BaseURL, spaceID, strings.ReplaceAll(title, " ", "%20"))
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

	if len(resp.Results) == 0 {
		return nil, nil // page not found
	}

	return &resp.Results[0], nil
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
			ID:      existing.ID,
			Title:   existing.Title,
			Status:  existing.Status,
			Version: existing.Version,
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

// UploadAttachment uploads a file attachment to a Confluence page.
func (c *ConfluenceClient) UploadAttachment(pageID, filename string, data []byte) error {
	// Use V1 API for attachments as V2 can be unreliable for this specific operation
	url := fmt.Sprintf("%s/rest/api/content/%s/child/attachment", c.BaseURL, pageID)

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
