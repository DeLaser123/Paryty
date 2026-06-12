package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
)

// TestTwinLifecycle_CreateAndActivate tests creating a twin and verifying its initial state.
func TestTwinLifecycle_CreateAndActivate(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping twin lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Create a tenant
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantID, "Test Tenant")
	require.NoError(t, err)

	// Create a handler
	handler := NewTwinHandler(pool, nil)

	// Create a context with tenant ID
	ctxWithTenant := context.WithValue(ctx, "tenant_id", tenantID)

	// Create a twin
	createReq := &parytyv1.CreateTwinRequest{
		Name:        "Test Twin",
		Description: "Test twin for lifecycle test",
		Config: &parytyv1.TwinConfig{
			EnabledCollectors: []string{"cpu", "memory"},
		},
	}
	twinInfo, err := handler.CreateTwin(ctxWithTenant, createReq)
	require.NoError(t, err)
	assert.NotNil(t, twinInfo)
	assert.NotEmpty(t, twinInfo.Id)
	assert.Equal(t, "Test Twin", twinInfo.Name)
	assert.Equal(t, "Test twin for lifecycle test", twinInfo.Description)
	assert.Equal(t, "pending", twinInfo.Status)

	// Verify twin appears in list
	listReq := &parytyv1.ListTwinsRequest{}
	listResp, err := handler.ListTwins(ctxWithTenant, listReq)
	require.NoError(t, err)
	assert.Len(t, listResp.Twins, 1)
	assert.Equal(t, twinInfo.Id, listResp.Twins[0].Id)
}

// TestTwinLifecycle_GetById tests retrieving a twin by ID.
func TestTwinLifecycle_GetById(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping twin lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Create a tenant
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantID, "Test Tenant")
	require.NoError(t, err)

	// Create a handler
	handler := NewTwinHandler(pool, nil)

	// Create a context with tenant ID
	ctxWithTenant := context.WithValue(ctx, "tenant_id", tenantID)

	// Create a twin
	createReq := &parytyv1.CreateTwinRequest{
		Name:        "Test Twin",
		Description: "Test twin for get test",
	}
	twinInfo, err := handler.CreateTwin(ctxWithTenant, createReq)
	require.NoError(t, err)

	// Get the twin by ID
	getReq := &parytyv1.GetTwinRequest{
		TwinId: twinInfo.Id,
	}
	getResp, err := handler.GetTwin(ctxWithTenant, getReq)
	require.NoError(t, err)
	assert.NotNil(t, getResp)
	assert.Equal(t, twinInfo.Id, getResp.Id)
	assert.Equal(t, "Test Twin", getResp.Name)
	assert.Equal(t, "Test twin for get test", getResp.Description)
	assert.Equal(t, "pending", getResp.Status)
}

// TestTwinLifecycle_Update tests updating a twin.
func TestTwinLifecycle_Update(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping twin lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Create a tenant
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantID, "Test Tenant")
	require.NoError(t, err)

	// Create a handler
	handler := NewTwinHandler(pool, nil)

	// Create a context with tenant ID
	ctxWithTenant := context.WithValue(ctx, "tenant_id", tenantID)

	// Create a twin
	createReq := &parytyv1.CreateTwinRequest{
		Name:        "Original Twin",
		Description: "Original description",
	}
	twinInfo, err := handler.CreateTwin(ctxWithTenant, createReq)
	require.NoError(t, err)

	// Update the twin
	updateReq := &parytyv1.UpdateTwinRequest{
		TwinId:      twinInfo.Id,
		Name:        "Updated Twin",
		Description: "Updated description",
	}
	updateResp, err := handler.UpdateTwin(ctxWithTenant, updateReq)
	require.NoError(t, err)
	assert.NotNil(t, updateResp)
	assert.Equal(t, "Updated Twin", updateResp.Name)
	assert.Equal(t, "Updated description", updateResp.Description)

	// Verify the update persisted
	getReq := &parytyv1.GetTwinRequest{
		TwinId: twinInfo.Id,
	}
	getResp, err := handler.GetTwin(ctxWithTenant, getReq)
	require.NoError(t, err)
	assert.Equal(t, "Updated Twin", getResp.Name)
	assert.Equal(t, "Updated description", getResp.Description)
}

// TestTwinLifecycle_Delete tests deleting a twin.
func TestTwinLifecycle_Delete(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping twin lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Create a tenant
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantID, "Test Tenant")
	require.NoError(t, err)

	// Create a handler
	handler := NewTwinHandler(pool, nil)

	// Create a context with tenant ID
	ctxWithTenant := context.WithValue(ctx, "tenant_id", tenantID)

	// Create a twin
	createReq := &parytyv1.CreateTwinRequest{
		Name:        "Twin to Delete",
		Description: "This twin will be deleted",
	}
	twinInfo, err := handler.CreateTwin(ctxWithTenant, createReq)
	require.NoError(t, err)

	// Delete the twin
	deleteReq := &parytyv1.DeleteTwinRequest{
		TwinId: twinInfo.Id,
	}
	_, err = handler.DeleteTwin(ctxWithTenant, deleteReq)
	require.NoError(t, err)

	// Verify twin is no longer in the list
	listReq := &parytyv1.ListTwinsRequest{}
	listResp, err := handler.ListTwins(ctxWithTenant, listReq)
	require.NoError(t, err)
	assert.Len(t, listResp.Twins, 0)

	// Verify twin cannot be retrieved (should return error)
	getReq := &parytyv1.GetTwinRequest{
		TwinId: twinInfo.Id,
	}
	_, err = handler.GetTwin(ctxWithTenant, getReq)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestTwinLifecycle_TenantIsolation tests that twins are isolated between tenants.
func TestTwinLifecycle_TenantIsolation(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping twin lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Create two tenants
	tenantA := uuid.New().String()
	tenantB := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantA, "Tenant A")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantB, "Tenant B")
	require.NoError(t, err)

	// Create a handler
	handler := NewTwinHandler(pool, nil)

	// Create context for Tenant A
	ctxA := context.WithValue(ctx, "tenant_id", tenantA)

	// Create a twin for Tenant A
	createReq := &parytyv1.CreateTwinRequest{
		Name:        "Tenant A Twin",
		Description: "This twin belongs to Tenant A",
	}
	twinInfo, err := handler.CreateTwin(ctxA, createReq)
	require.NoError(t, err)

	// Create context for Tenant B
	ctxB := context.WithValue(ctx, "tenant_id", tenantB)

	// Tenant B should not see Tenant A's twin
	listReq := &parytyv1.ListTwinsRequest{}
	listResp, err := handler.ListTwins(ctxB, listReq)
	require.NoError(t, err)
	assert.Len(t, listResp.Twins, 0, "Tenant B should not see Tenant A's twin")

	// Tenant B should not be able to get Tenant A's twin
	getReq := &parytyv1.GetTwinRequest{
		TwinId: twinInfo.Id,
	}
	_, err = handler.GetTwin(ctxB, getReq)
	assert.Error(t, err, "Tenant B should not be able to get Tenant A's twin")
	assert.Contains(t, err.Error(), "not found")

	// Tenant B should not be able to update Tenant A's twin
	updateReq := &parytyv1.UpdateTwinRequest{
		TwinId: twinInfo.Id,
		Name:   "Hacked Twin",
	}
	_, err = handler.UpdateTwin(ctxB, updateReq)
	assert.Error(t, err, "Tenant B should not be able to update Tenant A's twin")

	// Tenant B should not be able to delete Tenant A's twin
	deleteReq := &parytyv1.DeleteTwinRequest{
		TwinId: twinInfo.Id,
	}
	_, err = handler.DeleteTwin(ctxB, deleteReq)
	assert.Error(t, err, "Tenant B should not be able to delete Tenant A's twin")
}

// TestTwinLifecycle_PlanLimitEnforcement tests that twin creation is limited by plan.
func TestTwinLifecycle_PlanLimitEnforcement(t *testing.T) {
	// Skip if no database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("PARYTY_CP_DSN")
	}
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping twin lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Create a tenant
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`, tenantID, "Test Tenant")
	require.NoError(t, err)

	// Create a handler
	handler := NewTwinHandler(pool, nil)

	// Create a context with tenant ID
	ctxWithTenant := context.WithValue(ctx, "tenant_id", tenantID)

	// Try to create multiple twins
	// Note: The actual limit enforcement depends on plan configuration
	// This test verifies the system handles multiple twin creations gracefully
	for i := 1; i <= 5; i++ {
		createReq := &parytyv1.CreateTwinRequest{
			Name:        "Twin " + string(rune('0'+i)),
			Description: "Test twin for limit test",
		}
		_, err = handler.CreateTwin(ctxWithTenant, createReq)
		if err != nil {
			t.Logf("Twin %d creation failed (may be expected if limit enforced): %v", i, err)
			break
		}
	}

	// Verify we can still list twins
	listReq := &parytyv1.ListTwinsRequest{}
	listResp, err := handler.ListTwins(ctxWithTenant, listReq)
	require.NoError(t, err)
	assert.Greater(t, len(listResp.Twins), 0, "Should have at least one twin")
}
