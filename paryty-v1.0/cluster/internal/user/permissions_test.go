package user

import (
	"encoding/json"
	"testing"
)

func TestDefaultPermissions_Admin(t *testing.T) {
	perms := DefaultPermissions("admin")
	if !perms["tenants:read"] {
		t.Error("admin should have tenants:read")
	}
	if !perms["users:write"] {
		t.Error("admin should have users:write")
	}
	if !perms["api_keys:read"] {
		t.Error("admin should have api_keys:read")
	}
	if perms["nonexistent"] {
		t.Error("admin should not have nonexistent permission")
	}
}

func TestDefaultPermissions_Operator(t *testing.T) {
	perms := DefaultPermissions("operator")
	if !perms["twins:write"] {
		t.Error("operator should have twins:write")
	}
	if !perms["alerts:acknowledge"] {
		t.Error("operator should have alerts:acknowledge")
	}
	if perms["users:write"] {
		t.Error("operator should NOT have users:write")
	}
}

func TestDefaultPermissions_Viewer(t *testing.T) {
	perms := DefaultPermissions("viewer")
	if !perms["twins:read"] {
		t.Error("viewer should have twins:read")
	}
	if perms["twins:write"] {
		t.Error("viewer should NOT have twins:write")
	}
}

func TestDefaultPermissions_UnknownRole(t *testing.T) {
	perms := DefaultPermissions("nonexistent")
	if len(perms) != 0 {
		t.Errorf("unknown role should have empty permissions, got %d", len(perms))
	}
}

func TestMergePermissions_EmptyOverrides(t *testing.T) {
	merged := MergePermissions("admin", nil)
	if !merged["tenants:read"] {
		t.Error("admin+empty should still have tenants:read")
	}
}

func TestMergePermissions_OverrideToFalse(t *testing.T) {
	// An override of false should deny a permission even if role grants it
	overrides := map[string]bool{"twins:write": false}
	merged := MergePermissions("admin", overrides)
	if merged["twins:write"] {
		t.Error("twins:write should be overridden to false")
	}
	if !merged["users:read"] {
		t.Error("users:read should still be true from role defaults")
	}
}

func TestMergePermissions_OverrideToTrue(t *testing.T) {
	overrides := map[string]bool{"custom:permission": true}
	merged := MergePermissions("viewer", overrides)
	if !merged["custom:permission"] {
		t.Error("custom:permission should be true from override")
	}
	if !merged["twins:read"] {
		t.Error("twins:read should still be true from viewer defaults")
	}
}

func TestMarshalPermissions_Nil(t *testing.T) {
	data, err := MarshalPermissions(nil)
	if err != nil {
		t.Fatalf("MarshalPermissions(nil) should not error: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("expected {}, got %s", string(data))
	}
}

func TestMarshalPermissions_Empty(t *testing.T) {
	data, err := MarshalPermissions(map[string]bool{})
	if err != nil {
		t.Fatalf("MarshalPermissions({}) should not error: %v", err)
	}
	if string(data) != "{}" {
		t.Errorf("expected {}, got %s", string(data))
	}
}

func TestMarshalPermissions_WithData(t *testing.T) {
	perms := map[string]bool{"twins:read": true, "twins:write": false}
	data, err := MarshalPermissions(perms)
	if err != nil {
		t.Fatalf("MarshalPermissions failed: %v", err)
	}
	// Verify it's valid JSON
	var result map[string]bool
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("marshaled data is not valid JSON: %v", err)
	}
	if result["twins:read"] != true {
		t.Error("twins:read should be true")
	}
	if result["twins:write"] != false {
		t.Error("twins:write should be false")
	}
}

func TestUnmarshalPermissions_Valid(t *testing.T) {
	data := []byte(`{"twins:read":true,"users:write":false}`)
	perms, err := UnmarshalPermissions(data)
	if err != nil {
		t.Fatalf("UnmarshalPermissions failed: %v", err)
	}
	if !perms["twins:read"] {
		t.Error("twins:read should be true")
	}
	if perms["users:write"] {
		t.Error("users:write should be false")
	}
}

func TestUnmarshalPermissions_Empty(t *testing.T) {
	perms, err := UnmarshalPermissions([]byte{})
	if err != nil {
		t.Fatalf("UnmarshalPermissions(empty) should not error: %v", err)
	}
	if len(perms) != 0 {
		t.Errorf("expected empty map, got %d entries", len(perms))
	}
}

func TestUnmarshalPermissions_InvalidJSON(t *testing.T) {
	_, err := UnmarshalPermissions([]byte("not-json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestHasPermission_Granted(t *testing.T) {
	if !HasPermission("admin", nil, "users:read") {
		t.Error("admin should have users:read")
	}
}

func TestHasPermission_Denied(t *testing.T) {
	if HasPermission("viewer", nil, "users:write") {
		t.Error("viewer should NOT have users:write")
	}
}

func TestHasPermission_OverrideDenies(t *testing.T) {
	overrides := map[string]bool{"users:read": false}
	if HasPermission("admin", overrides, "users:read") {
		t.Error("override should deny users:read for admin")
	}
}

func TestHasPermission_OverrideGrants(t *testing.T) {
	overrides := map[string]bool{"users:write": true}
	if !HasPermission("viewer", overrides, "users:write") {
		t.Error("override should grant users:write for viewer")
	}
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	original := map[string]bool{
		"tenants:read":  true,
		"tenants:write": false,
		"users:read":    true,
		"twins:write":   false,
	}
	data, err := MarshalPermissions(original)
	if err != nil {
		t.Fatalf("MarshalPermissions: %v", err)
	}
	restored, err := UnmarshalPermissions(data)
	if err != nil {
		t.Fatalf("UnmarshalPermissions: %v", err)
	}
	for k, v := range original {
		if restored[k] != v {
			t.Errorf("key %s: expected %v, got %v", k, v, restored[k])
		}
	}
}
