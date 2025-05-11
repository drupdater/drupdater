package codehosting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultVcsProviderFactory_Create(t *testing.T) {
	// Table-driven test approach
	tests := []struct {
		name          string
		repositoryURL string
		token         string
		expectedType  interface{}
	}{
		{
			name:          "returns gitlab platform for gitlab URLs",
			repositoryURL: "https://gitlab.com/some/repo",
			token:         "dummy-token",
			expectedType:  &Gitlab{},
		},
		{
			name:          "returns github platform for github URLs",
			repositoryURL: "https://github.com/some/repo",
			token:         "dummy-token",
			expectedType:  &Github{},
		},
		{
			name:          "defaults to gitlab platform for unknown providers",
			repositoryURL: "https://gitfoo.com/some/repo",
			token:         "dummy-token",
			expectedType:  &Gitlab{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			factory := NewDefaultVcsProviderFactory()

			// Execute
			provider := factory.Create(tt.repositoryURL, tt.token)

			// Assert
			assert.IsType(t, tt.expectedType, provider)
		})
	}
}
