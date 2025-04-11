package services

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestGetIssue(t *testing.T) {
	// Mock server to simulate Drupal API
	mockResponse := `{
		"nid": "12345",
		"title": "Test Issue",
		"field_issue_status": "1",
		"url": "http://example.com"
	}`
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(mockResponse))
		assert.NoError(t, err)
	}))
	defer mockServer.Close()

	// Create service instance with mock server URL
	logger := zaptest.NewLogger(t)
	service := &DefaultDrupalOrgService{
		DrupalOrgBaseURL: mockServer.URL,
		logger:           logger,
	}

	// Call GetIssue method
	issueID := "12345"
	issue, err := service.GetIssue(issueID)

	// Assertions
	assert.NoError(t, err)
	assert.NotNil(t, issue)
	assert.Equal(t, "12345", issue.ID)
	assert.Equal(t, "Test Issue", issue.Title)
	assert.Equal(t, "1", issue.Status)
	assert.Equal(t, "http://example.com", issue.URL)
}

func TestGetIssue_Failure(t *testing.T) {
	// Mock server to simulate Drupal API failure
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	// Create service instance with mock server URL
	logger := zaptest.NewLogger(t)
	service := &DefaultDrupalOrgService{
		DrupalOrgBaseURL: mockServer.URL,
		logger:           logger,
	}

	// Call GetIssue method
	issueID := "12345"
	issue, err := service.GetIssue(issueID)

	// Assertions
	assert.Error(t, err)
	assert.Nil(t, issue)
}

func TestFindIssueNumber(t *testing.T) {
	// Create an instance of DefaultDrupalOrgService
	service := &DefaultDrupalOrgService{}

	// Define test cases
	testCases := []struct {
		text     string
		expected string
		found    bool
	}{
		{"This is a test with issue number #123456", "123456", true},
		{"No issue number here", "", false},
		{"Another test with issue number #654321", "654321", true},
		{"Multiple issues #111111 and #222222", "111111", true},
		{"https://www.drupal.org/files/issues/2022-10-04/password_policy_field_last_password_reset_unknown_error_2771129-134.patch", "2771129", true},
	}

	// Run test cases
	for _, tc := range testCases {
		issueNumber, found := service.FindIssueNumber(tc.text)
		assert.Equal(t, tc.expected, issueNumber)
		assert.Equal(t, tc.found, found)
	}
}
