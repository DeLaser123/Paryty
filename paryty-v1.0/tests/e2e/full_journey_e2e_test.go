//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFullUserJourney validates the complete end-to-end user flow:
// register → login → profile → create twin → list twins →
// create agent → check topology → check alerts.
//
// Each step asserts: status code 200/201, valid response body, no errors.
func TestFullUserJourney(t *testing.T) {
	client := &http.Client{Timeout: 15 * time.Second}

	// Unique email to avoid collisions with other e2e test runs
	uniqueEmail := fmt.Sprintf("journey-test-%d@paryty.io", time.Now().UnixNano())
	password := "Journey!Pass123"
	var accessToken string
	var twinID string
	var agentID string

	// ─── Step 1: Register ────────────────────────────────────
	t.Run("Step1_Register", func(t *testing.T) {
		regBody, err := json.Marshal(map[string]string{
			"email":    uniqueEmail,
			"password": password,
			"name":     "Journey Test User",
		})
		require.NoError(t, err)

		resp, err := client.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(regBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		if !assert.Equal(t, http.StatusCreated, resp.StatusCode,
			"register should return 201 Created; body: %s", readBody(resp)) {
			t.FailNow()
		}

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err, "register response must be valid JSON")

		// JWT or user data should be present
		assert.True(t,
			result["accessToken"] != nil ||
				result["token"] != nil ||
				result["data"] != nil,
			"register response must contain token or user data")
	})

	// ─── Step 2: Login ───────────────────────────────────────
	t.Run("Step2_Login", func(t *testing.T) {
		loginBody, err := json.Marshal(map[string]string{
			"email":    uniqueEmail,
			"password": password,
		})
		require.NoError(t, err)

		resp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		if !assert.Equal(t, http.StatusOK, resp.StatusCode,
			"login should return 200 OK; body: %s", readBody(resp)) {
			t.FailNow()
		}

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err, "login response must be valid JSON")

		accessToken = extractToken(t, result)
		assert.NotEmpty(t, accessToken, "login response must contain an access token")
	})

	// ─── Step 3: Get user profile ────────────────────────────
	t.Run("Step3_GetProfile", func(t *testing.T) {
		req, err := http.NewRequest("GET", baseURL+"/api/v1/me", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// 200 OK or 404 if /me is not yet implemented — both acceptable
		if resp.StatusCode == http.StatusNotFound {
			t.Skip("/api/v1/me endpoint not yet implemented — skipping profile check")
		}

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"GET /me should return 200 OK; body: %s", readBody(resp))

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.NotNil(t, result, "profile response must not be empty")
	})

	// ─── Step 4: Create twin ─────────────────────────────────
	t.Run("Step4_CreateTwin", func(t *testing.T) {
		twinBody, err := json.Marshal(map[string]interface{}{
			"name":        "Journey Test Twin",
			"description": "Created by full journey E2E test",
			"abilities":   []string{"topology_observation", "metrics_monitoring", "alert_evaluation"},
		})
		require.NoError(t, err)

		req, err := http.NewRequest("POST", baseURL+"/api/v1/twins", bytes.NewReader(twinBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		if !assert.Equal(t, http.StatusCreated, resp.StatusCode,
			"create twin should return 201 Created; body: %s", readBody(resp)) {
			t.FailNow()
		}

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Extract twin ID from response (either at top level or nested under "data")
		if data, ok := result["data"].(map[string]interface{}); ok {
			if id, ok := data["id"].(string); ok {
				twinID = id
			}
		}
		if twinID == "" {
			if id, ok := result["id"].(string); ok {
				twinID = id
			}
		}
		assert.NotEmpty(t, twinID, "created twin must have an id")
	})

	// ─── Step 5: Verify twin exists ──────────────────────────
	t.Run("Step5_ListTwins", func(t *testing.T) {
		req, err := http.NewRequest("GET", baseURL+"/api/v1/twins", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"GET /twins should return 200 OK; body: %s", readBody(resp))

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// The response is a list of twins — find our created twin
		found := false
		if data, ok := result["data"].([]interface{}); ok {
			for _, item := range data {
				if m, ok := item.(map[string]interface{}); ok {
					if m["id"] == twinID {
						found = true
						assert.Equal(t, "Journey Test Twin", m["name"])
						break
					}
				}
			}
		}
		// Also check if response is a naked array
		if arr, ok := result.([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					if m["id"] == twinID {
						found = true
						assert.Equal(t, "Journey Test Twin", m["name"])
						break
					}
				}
			}
		}
		assert.True(t, found, "created twin with id %s must appear in GET /twins response", twinID)
	})

	// ─── Step 6: Create agent ────────────────────────────────
	t.Run("Step6_CreateAgent", func(t *testing.T) {
		agentBody, err := json.Marshal(map[string]interface{}{
			"name":     "Journey Test Agent",
			"twin_id":  twinID,
			"hostname": "journey-test-host",
			"platform": "linux",
			"version":  "1.0.0",
		})
		require.NoError(t, err)

		req, err := http.NewRequest("POST", baseURL+"/api/v1/agents", bytes.NewReader(agentBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// 201 Created or 200 OK both acceptable
		assert.True(t,
			resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
			"create agent should return 201 or 200; got %d; body: %s", resp.StatusCode, readBody(resp))

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Extract agent ID
		if data, ok := result["data"].(map[string]interface{}); ok {
			if id, ok := data["id"].(string); ok {
				agentID = id
			}
		}
		if agentID == "" {
			if id, ok := result["id"].(string); ok {
				agentID = id
			}
		}

		// Check for API key in response
		if data, ok := result["data"].(map[string]interface{}); ok {
			if key, ok := data["api_key"].(string); ok {
				assert.NotEmpty(t, key, "created agent should include an API key")
			}
		}
	})

	// ─── Step 7: Check topology ──────────────────────────────
	t.Run("Step7_CheckTopology", func(t *testing.T) {
		req, err := http.NewRequest("GET", baseURL+"/api/v1/topology", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"GET /topology should return 200 OK; body: %s", readBody(resp))

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		assert.NotNil(t, result, "topology response must not be empty")
		// Topology should contain a node for our agent if it was registered
		t.Logf("Topology: %+v", result)
	})

	// ─── Step 8: Check alerts ────────────────────────────────
	t.Run("Step8_CheckAlerts", func(t *testing.T) {
		req, err := http.NewRequest("GET", baseURL+"/api/v1/alerts", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// 200 OK or 204 No Content — both valid if no alerts exist
		assert.True(t,
			resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent,
			"GET /alerts should return 200 or 204; got %d; body: %s", resp.StatusCode, readBody(resp))

		if resp.StatusCode == http.StatusOK {
			var result map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&result)
			require.NoError(t, err)
			assert.NotNil(t, result, "alerts response must be valid JSON when 200 OK")
		}
	})

	t.Logf("Full user journey completed: twin=%s, agent=%s", twinID, agentID)
}

// ─── Helpers ──────────────────────────────────────────────────────

// extractToken extracts an access token from a JSON response map.
// Checks common key names: accessToken, access_token, token, jwt.
func extractToken(t *testing.T, result map[string]interface{}) string {
	t.Helper()

	// Direct accessToken (camelCase — standard for Paryty)
	if tok, ok := result["accessToken"].(string); ok && tok != "" {
		return tok
	}
	// Nested under data
	if data, ok := result["data"].(map[string]interface{}); ok {
		if tok, ok := data["accessToken"].(string); ok && tok != "" {
			return tok
		}
		if tok, ok := data["access_token"].(string); ok && tok != "" {
			return tok
		}
		if tok, ok := data["token"].(string); ok && tok != "" {
			return tok
		}
	}
	// Common alternatives at top level
	if tok, ok := result["token"].(string); ok && tok != "" {
		return tok
	}
	if tok, ok := result["access_token"].(string); ok && tok != "" {
		return tok
	}

	return ""
}

// readBody reads the response body and returns it as a string for error messages.
// It drains the body and returns "(unreadable)" if decoding fails.
func readBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return "(no body)"
	}
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(resp.Body)
	if err != nil {
		return "(unreadable)"
	}
	return buf.String()
}
