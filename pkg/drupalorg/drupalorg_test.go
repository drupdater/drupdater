package drupalorg

import (
	"io"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewHTTPClient_HasTimeout(t *testing.T) {
	logger := zap.NewNop()
	c := NewHTTPClient(logger)

	assert.Equal(t, 30*time.Second, c.client.Timeout)
	// The base URL is the only thing pointing this client at drupal.org; the tests below all
	// override it, so nothing else pins the production default.
	assert.Equal(t, "https://www.drupal.org", c.DrupalOrgBaseURL)
	assert.Same(t, logger, c.logger)
}

func TestGetIssue_Timeout(t *testing.T) {
	// Server never responds; the client timeout must abort the request.
	block := make(chan struct{})
	mockServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	defer mockServer.Close()
	defer close(block)

	service := &HTTPClient{
		DrupalOrgBaseURL: mockServer.URL,
		logger:           zaptest.NewLogger(t),
		client:           &http.Client{Timeout: 10 * time.Millisecond},
	}

	issue, err := service.GetIssue(context.Background(), "12345")
	require.Error(t, err)
	assert.Nil(t, issue)
	assert.Contains(t, err.Error(), "failed to make request")
	// The transport failure has to stay reachable so a caller can tell a timeout from a
	// refused connection.
	var urlErr *url.Error
	require.ErrorAs(t, err, &urlErr)
	assert.True(t, urlErr.Timeout())
}

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
	service := &HTTPClient{
		DrupalOrgBaseURL: mockServer.URL,
		logger:           logger,
		client:           &http.Client{},
	}

	// Call GetIssue method
	issueID := "12345"
	issue, err := service.GetIssue(context.Background(), issueID)

	// Assertions
	require.NoError(t, err)
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
	service := &HTTPClient{
		DrupalOrgBaseURL: mockServer.URL,
		logger:           logger,
		client:           &http.Client{},
	}

	// Call GetIssue method
	issueID := "12345"
	issue, err := service.GetIssue(context.Background(), issueID)

	// Assertions
	require.Error(t, err)
	assert.Nil(t, issue)
}

func TestGetIssue_MalformedBody(t *testing.T) {
	// drupal.org answering 200 with something that is not the expected JSON must be reported as
	// a decode failure, with the cause still reachable.
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"nid": `))
	}))
	defer mockServer.Close()

	service := &HTTPClient{
		DrupalOrgBaseURL: mockServer.URL,
		logger:           zaptest.NewLogger(t),
		client:           &http.Client{},
	}

	issue, err := service.GetIssue(context.Background(), "12345")
	require.Error(t, err)
	assert.Nil(t, issue)
	assert.Contains(t, err.Error(), "failed to decode response")
	require.ErrorIs(t, err, io.ErrUnexpectedEOF, "the decode error must survive the wrapper")
}

func TestFindIssueNumber(t *testing.T) {
	// Create an instance of DefaultDrupalOrgService
	service := &HTTPClient{}

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

func TestGetIssue_NonOKStatus(t *testing.T) {
	// drupal.org answers an unknown node or an outage with an HTML error page. Checking the
	// status first turns that into a message that names the real problem instead of an opaque
	// JSON decode failure.
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte("<html>error</html>"))
			}))
			defer mockServer.Close()

			service := &HTTPClient{
				DrupalOrgBaseURL: mockServer.URL,
				logger:           zaptest.NewLogger(t),
				client:           mockServer.Client(),
			}

			issue, err := service.GetIssue(context.Background(), "12345")
			require.Error(t, err)
			assert.Nil(t, issue)
			assert.Contains(t, err.Error(), "unexpected status")
		})
	}
}

func TestGetIssue_InvalidURL(t *testing.T) {
	service := &HTTPClient{
		DrupalOrgBaseURL: "://not-a-url",
		logger:           zaptest.NewLogger(t),
		client:           &http.Client{},
	}

	issue, err := service.GetIssue(context.Background(), "12345")
	require.Error(t, err)
	assert.Nil(t, issue)
}
