package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSettingsRejectsLegacyMCPServers(t *testing.T) {
	settings := GetDefaultSettings()
	settings.Model = "dummy"
	settings.LegacyMCPServers = []string{"stdio://old"}

	err := settings.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "deprecated")
}

func TestValidateSettingsMCPMissingFields(t *testing.T) {
	settings := GetDefaultSettings()
	settings.Model = "dummy"
	settings.MCP = &MCPConfig{Servers: map[string]MCPServerConfig{
		"http":  {Type: "http"},
		"stdio": {Type: "stdio"},
	}}

	err := settings.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "command is required")
	require.Contains(t, err.Error(), "url is required")
}

func TestValidateSettingsMCPHappyPath(t *testing.T) {
	settings := GetDefaultSettings()
	settings.Model = "dummy"
	settings.MCP = &MCPConfig{Servers: map[string]MCPServerConfig{
		"api": {
			Type:           "http",
			URL:            "https://api.example",
			Headers:        map[string]string{"Authorization": "Bearer x"},
			TimeoutSeconds: 2,
		},
	}}

	require.NoError(t, settings.Validate())
}

func TestValidateSettingsMCPHeaderAndTimeoutErrors(t *testing.T) {
	settings := GetDefaultSettings()
	settings.Model = "dummy"
	settings.MCP = &MCPConfig{Servers: map[string]MCPServerConfig{
		"bad": {
			Type:           "http",
			URL:            "https://example",
			TimeoutSeconds: -1,
			Headers:        map[string]string{"": "value"},
		},
	}}

	err := settings.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeoutSeconds")
	require.Contains(t, err.Error(), "headers")
}
