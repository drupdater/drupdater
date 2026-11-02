package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActiveRunTypeIsTheSecurityBlockOnlyForASecurityRun(t *testing.T) {
	c := Config{RunTypes: RunTypesConfig{
		Normal:   RunTypeConfig{Addons: []string{"normal"}},
		Security: RunTypeConfig{Addons: []string{"security"}, AutoMerge: true},
	}}

	assert.Equal(t, c.RunTypes.Normal, c.ActiveRunType())
	c.Security = true
	assert.Equal(t, c.RunTypes.Security, c.ActiveRunType())
}
