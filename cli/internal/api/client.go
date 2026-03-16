package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client wraps the PattyRich bingo API at praynr.com.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates an API client with default settings.
func NewClient() *Client {
	return &Client{
		BaseURL:    "https://praynr.com",
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateBoard creates a new bingo board.
// IMPORTANT: teams must be an integer count, NOT an array of names.
func (c *Client) CreateBoard(name, adminPw, generalPw string, rows, cols, teamCount int) error {
	body := map[string]interface{}{
		"boardName":       name,
		"adminPassword":   adminPw,
		"generalPassword": generalPw,
		"rows":            rows,
		"columns":         cols,
		"teams":           teamCount,
		"boardData":       []interface{}{},
	}
	return c.post("/createBoard", body)
}

// GetBoard fetches the current board state.
// pwType: "admin" or "general"
func (c *Client) GetBoard(name, password, pwType string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/getBoard/%s/%s/%s", c.BaseURL, name, password, pwType)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return result, nil
}

// UpdateBoard updates a tile on the board.
// Use admin pw + "admin" for metadata (title, points, image).
// Use general pw + "general" for completion state (checked, currPoints, teamId).
//
// IMPORTANT: PattyRich API naming is confusing. The server accesses
// boardData[data['row']][data['col']], but boardData is indexed as
// boardData[column][row]. So the API's "row" param = our column index,
// and the API's "col" param = our row index.
func (c *Client) UpdateBoard(name, password, pwType string, col, row int, info map[string]interface{}) error {
	url := fmt.Sprintf("%s/updateBoard/%s/%s/%s", c.BaseURL, name, password, pwType)
	body := map[string]interface{}{
		"row":  col, // API "row" = boardData first index = column
		"col":  row, // API "col" = boardData second index = row
		"info": info,
	}
	return c.put(url, body)
}

// RenameTeams renames the teams on a board.
func (c *Client) RenameTeams(name, adminPw string, teamNames []string, rows, cols int) error {
	url := fmt.Sprintf("%s/updateTeams/%s/%s/admin", c.BaseURL, name, adminPw)
	teamData := make([]map[string]interface{}, len(teamNames))
	for i, tn := range teamNames {
		teamData[i] = map[string]interface{}{
			"data": map[string]interface{}{"name": tn},
		}
	}
	body := map[string]interface{}{
		"dataToSend": map[string]interface{}{
			"teamData":         teamData,
			"rows":             rows,
			"columns":          cols,
			"passwordRequired": false,
		},
	}
	return c.put(url, body)
}

func (c *Client) post(path string, body interface{}) error {
	jsonBody, _ := json.Marshal(body)
	resp, err := c.HTTPClient.Post(c.BaseURL+path, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

func (c *Client) put(url string, body interface{}) error {
	jsonBody, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return nil
}
