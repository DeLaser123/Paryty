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

// TestTenantIsolation verifies that tenant A's data is not visible
// to tenant B — a fundamental multi-tenancy security guarantee.
//
// Flow:
//   1. Register Tenant A
//   2. Register Tenant B
//   3. Tenant A creates a twin
//   4. Tenant B lists twins — must NOT see Tenant A's twin
//   5. Tenant B tries to GET Tenant A's twin directly — must get 404/403
func TestTenantIsolation(t *testing.T) {
	client := &http.Client{Timeout: 15 * time.Second}

	password := "Isolation!Pass123"

	// ── Register two tenants ──────────────────────────────────────
	tenantAEmail := fmt.Sprintf("tenant-a-%d@paryty.io", time.Now().UnixNano())
	tenantBEmail := fmt.Sprintf("tenant-b-%d@paryty.io", time.Now().UnixNano())

	var tokenA, tokenB string
	var twinIDA string

	t.Run("Register_TenantA", func(t *testing.T) {
		regBody, _ := json.Marshal(map[string]string{
			"email":    tenantAEmail,
			"password": password,
			"name":     "Tenant A — Isolation Test",
		})
		resp, err := client.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(regBody))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.True(t, resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK)
	})

	t.Run("Register_TenantB", func(t *testing.T) {
		regBody, _ := json.Marshal(map[string]string{
			"email":    tenantBEmail,
			"password": password,
			"name":     "Tenant B — Isolation Test",
		})
		resp, err := client.Post(baseURL+"/api/v1/auth/register", "application/json", bytes.NewReader(regBody))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.True(t, resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK)
	})

	// ── Login both tenants ────────────────────────────────────────
	t.Run("Login_TenantA", func(t *testing.T) {
		loginBody, _ := json.Marshal(map[string]string{
			"email":    tenantAEmail,
			"password": password,
		})
		resp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		if !assert.Equal(t, http.StatusOK, resp.StatusCode) {
			t.FailNow()
		}
		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		tokenA = extractToken(t, result)
		require.NotEmpty(t, tokenA, "tenant A login must return an access token")
	})

	t.Run("Login_TenantB", func(t *testing.T) {
		loginBody, _ := json.Marshal(map[string]string{
			"email":    tenantBEmail,
			"password": password,
		})
		resp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
		require.NoError(t, err)
		defer resp.Body.Close()

		if !assert.Equal(t, http.StatusOK, resp.StatusCode) {
			t.FailNow()
		}
		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)
		tokenB = extractToken(t, result)
		require.NotEmpty(t, tokenB, "tenant B login must return an access token")
	})

	// ── Tenant A creates a twin ───────────────────────────────────
	t.Run("TenantA_CreateTwin", func(t *testing.T) {
		twinBody, _ := json.Marshal(map[string]interface{}{
			"name":        "A's Secret Twin",
			"description": "Should not be visible to Tenant B",
			"abilities":   []string{"topology_observation"},
		})
		req, _ := http.NewRequest("POST", baseURL+"/api/v1/twins", bytes.NewReader(twinBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tokenA)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		if !assert.Equal(t, http.StatusCreated, resp.StatusCode) {
			// If 404, twins API may not be deployed — skip the isolation test
			if resp.StatusCode == http.StatusNotFound {
				t.Skip("twins API not available — skipping isolation test")
			}
			t.FailNow()
		}

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Extract twin ID
		if data, ok := result["data"].(map[string]interface{}); ok {
			if id, ok := data["id"].(string); ok {
				twinIDA = id
			}
		}
		if twinIDA == "" {
			if id, ok := result["id"].(string); ok {
				twinIDA = id
			}
		}
		require.NotEmpty(t, twinIDA, "created twin must have an id")
	})

	// ── Tenant B lists twins — must NOT see A's twin ──────────────
	t.Run("TenantB_CannotSeeTwinsFromA", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/api/v1/twins", nil)
		req.Header.Set("Authorization", "Bearer "+tokenB)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		// Check that Tenant A's twin is NOT present in B's list
		checkDataForID(t, result, twinIDA, false,
			"Tenant B's twin list must NOT contain Tenant A's twin (id=%s)", twinIDA)
	})

	// ── Tenant B tries direct access to A's twin → 404 ────────────
	t.Run("TenantB_CannotDirectlyGetTwinFromA", func(t *testing.T) {
		if twinIDA == "" {
			t.Skip("no twin ID — skipping direct access test")
		}

		req, _ := http.NewRequest("GET", baseURL+"/api/v1/twins/"+twinIDA, nil)
		req.Header.Set("Authorization", "Bearer "+tokenB)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Must get 404 (not found) or 403 (forbidden) — never 200
		assert.True(t,
			resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden,
			"Tenant B accessing A's twin directly must return 404 or 403; got %d; body: %s",
			resp.StatusCode, readBody(resp))

		t.Logf("Tenant B direct access to A's twin returned: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	})

	// ── Tenant A can still see their own twin ─────────────────────
	t.Run("TenantA_CanSeeOwnTwin", func(t *testing.T) {
		if twinIDA == "" {
			t.Skip("no twin ID — skipping own-access test")
		}

		req, _ := http.NewRequest("GET", baseURL+"/api/v1/twins", nil)
		req.Header.Set("Authorization", "Bearer "+tokenA)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&result)
		require.NoError(t, err)

		checkDataForID(t, result, twinIDA, true,
			"Tenant A's own twin list must contain their twin (id=%s)", twinIDA)
	})

	t.Log("Tenant isolation verified: Tenant B cannot access Tenant A's resources")
}

// ─── Helpers ──────────────────────────────────────────────────────

// checkDataForID asserts that an id is present (or not present) in a
// response payload. The payload may contain a "data" wrapper array or
// be a bare array at the top level.
func checkDataForID(t *testing.T, result map[string]interface{}, targetID string, shouldExist bool, msg string, args ...interface{}) {
	t.Helper()

	found := findIDInResponse(result, targetID)

	if shouldExist {
		assert.True(t, found, msg, args...)
	} else {
		assert.False(t, found, msg, args...)
	}
}

// findIDInResponse searches a JSON response payload (map or array)
// for an object with the specified "id" field.
// Returns true if found.
func findIDInResponse(result interface{}, targetID string) bool {
	switch v := result.(type) {
	case map[string]interface{}:
		// Check if the map itself has the target ID
		if id, ok := v["id"].(string); ok && id == targetID {
			return true
		}
		// Check nested "data" array
		if data, ok := v["data"].([]interface{}); ok {
			for _, item := range data {
				if findIDInResponse(item, targetID) {
					return true
				}
			}
		}
		// Check nested "data" map
		if data, ok := v["data"].(map[string]interface{}); ok {
			if findIDInResponse(data, targetID) {
				return true
			}
		}
		// Check "items", "twins", "agents" etc.
		for _, key := range []string{"items", "twins", "agents", "results"} {
			if arr, ok := v[key].([]interface{}); ok {
				for _, item := range arr {
					if findIDInResponse(item, targetID) {
						return true
					}
				}
			}
		}
	case []interface{}:
		for _, item := range v {
			if findIDInResponse(item, targetID) {
				return true
			}
		}
	}
	return false
}
