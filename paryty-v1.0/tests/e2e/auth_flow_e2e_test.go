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

const baseURL = "http://localhost:8080"

func TestAuthFlowE2E(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	// Register
	regBody, _ := json.Marshal(map[string]string{
		"email":    "e2e-test@paryty.io",
		"password": "E2ETest!Pass123",
		"name":     "E2E Test User",
	})
	regResp, err := client.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(regBody))
	require.NoError(t, err)
	assert.Equal(t, 201, regResp.StatusCode)

	// Login
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "e2e-test@paryty.io",
		"password": "E2ETest!Pass123",
	})
	loginResp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	require.NoError(t, err)
	assert.Equal(t, 200, loginResp.StatusCode)

	var loginResult map[string]interface{}
	json.NewDecoder(loginResp.Body).Decode(&loginResult)
	assert.NotEmpty(t, loginResult["accessToken"])
}
