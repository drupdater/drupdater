package drupalorg

import (
	"encoding/json"
	"net/http"
	"regexp"

	"go.uber.org/zap"
)

type Client interface {
	GetIssue(issueID string) (*Issue, error)
	FindIssueNumber(text string) (string, bool)
}

type HTTPClient struct {
	DrupalOrgBaseURL string
	logger           *zap.Logger
}

func NewHTTPClient(logger *zap.Logger) *HTTPClient {
	return &HTTPClient{
		DrupalOrgBaseURL: "https://www.drupal.org",
		logger:           logger,
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

func (s *HTTPClient) GetIssue(issueID string) (*Issue, error) {
	resp, err := http.Get(s.DrupalOrgBaseURL + "/api-d7/node/" + issueID + ".json")
	if err != nil {
		s.logger.Error("failed to make request", zap.Error(err))
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp Issue
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		s.logger.Error("failed to decode response", zap.Error(err))
		return nil, err
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
