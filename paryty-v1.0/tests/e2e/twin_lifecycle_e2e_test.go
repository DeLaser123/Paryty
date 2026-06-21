//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwinLifecycleE2E(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	// First login to get token
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "e2e-test@paryty.io",
		"password": "E2ETest!Pass123",
	})
	loginResp, _ := client.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	var loginResult map[string]interface{}
	json.NewDecoder(loginResp.Body).Decode(&loginResult)
	token := loginResult["accessToken"].(string)

	// Create twin
	twinBody, _ := json.Marshal(map[string]interface{}{
		"name":        "E2E Test Twin",
		"description": "Created by E2E test",
		"abilities":   []string{"topology_observation", "metrics_monitoring"},
	})
	req, _ := http.NewRequest("POST", baseURL+"/api/v1/twins", bytes.NewReader(twinBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	createResp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, 201, createResp.StatusCode)

	var twinResult map[string]interface{}
	json.NewDecoder(createResp.Body).Decode(&twinResult)
	twinData := twinResult["data"].(map[string]interface{})
	twinID := twinData["id"].(string)
	assert.NotEmpty(t, twinID)
	assert.Equal(t, "E2E Test Twin", twinData["name"])
}
