package user

import (
	"encoding/json"
)

// DefaultPermissions returns the set of permissions associated with a given
// role. These serve as the baseline; per-user overrides (stored as JSONB in
// the users table) are merged on top of these defaults at authorization time.
func DefaultPermissions(role string) map[string]bool {
	switch role {
	case "admin":
		return map[string]bool{
			"tenants:read":  true,
			"tenants:write": true,
			"users:read":    true,
			"users:write":   true,
			"twins:read":    true,
			"twins:write":   true,
			"api_keys:read": true,
			"api_keys:write": true,
		}
	case "operator":
		return map[string]bool{
			"twins:read":          true,
			"twins:write":         true,
			"alerts:acknowledge":  true,
		}
	case "viewer":
		return map[string]bool{
			"twins:read": true,
		}
	default:
		return map[string]bool{}
	}
}

// MergePermissions merges role-based default permissions with per-user
// overrides. Overrides take precedence: an override of `false` explicitly
// denies a permission even if the role default grants it.
func MergePermissions(role string, overrides map[string]bool) map[string]bool {
	merged := DefaultPermissions(role)
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

// MarshalPermissions serializes a permissions map to JSONB-compatible JSON.
func MarshalPermissions(permissions map[string]bool) ([]byte, error) {
	if permissions == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(permissions)
}

// UnmarshalPermissions deserializes a JSONB permissions column into a map.
func UnmarshalPermissions(data []byte) (map[string]bool, error) {
	if len(data) == 0 {
		return map[string]bool{}, nil
	}
	var perms map[string]bool
	if err := json.Unmarshal(data, &perms); err != nil {
		return nil, err
	}
	return perms, nil
}

// HasPermission checks whether the effective permissions (after merging
// role defaults + overrides) grant the requested permission.
func HasPermission(role string, overrides map[string]bool, permission string) bool {
	merged := MergePermissions(role, overrides)
	return merged[permission]
}
