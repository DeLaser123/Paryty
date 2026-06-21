package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/paryty/paryty-v1.0/cluster/internal/plan"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// If encoding fails, we can't write more headers, but log-worthy.
		_ = err
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// parseFloat parses a string to a float64 pointer, returning an error if parsing fails.
func parseFloat(s string, dst *float64) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse float %q: %w", s, err)
	}
	*dst = v
	return v, nil
}

// tenantFromRequest extracts the authenticated tenant ID from the request
// context (set by JWT auth middleware). Returns empty string and an error
// if no tenant context exists.
func tenantFromRequest(r *http.Request) (string, error) {
	v := r.Context().Value(plan.CtxTenantID)
	if v == nil {
		return "", fmt.Errorf("missing tenant context")
	}
	tenantID, ok := v.(string)
	if !ok || tenantID == "" {
		return "", fmt.Errorf("missing tenant context")
	}
	return tenantID, nil
}
