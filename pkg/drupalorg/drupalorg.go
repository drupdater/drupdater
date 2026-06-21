package drupalorg

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"go.uber.org/zap"
)

type HTTPClient struct {
	DrupalOrgBaseURL string
	logger           *zap.Logger
	client           *http.Client
}

func NewHTTPClient(logger *zap.Logger) *HTTPClient {
	return &HTTPClient{
		DrupalOrgBaseURL: "https://www.drupal.org",
		logger:           logger,
		client:           &http.Client{Timeout: 30 * time.Second},
	}
}

type Issue struct {
	ID      string `json:"nid"`
	Title   string `json:"title"`
	Status  string `json:"field_issue_status"`
	URL     string `json:"url"`
	Project struct {
		MaschineName string `json:"machine_name"`
	} `json:"field_project"`
}

func (s *HTTPClient) GetIssue(ctx context.Context, issueID string) (*Issue, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.DrupalOrgBaseURL+"/api-d7/node/"+issueID+".json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	var apiResp Issue
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &apiResp, nil
}

func (s *HTTPClient) FindIssueNumber(text string) (string, bool) {
	// Define a regex pattern to match issue numbers
	re := regexp.MustCompile(`(\d{6,})`)

	// Find first match
	matches := re.FindStringSubmatch(text)
	if matches == nil {
		return "", false
	}

	return matches[0], true
}
