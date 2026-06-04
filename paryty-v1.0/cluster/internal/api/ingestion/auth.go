// API key authentication interceptors for the ingestion gRPC service.
//
// These interceptors validate the "x-api-key" metadata header against the
// control plane's API key store and inject the resolved tenant ID into the
// request context via the existing ctxKeyTenant key (defined in grpc_adapter.go).
//
// TenantFromContext (grpc_adapter.go) retrieves the tenant ID set by these
// interceptors -- no additional extraction function is needed.
package api

import (
	"context"

	"github.com/paryty/paryty-v1.0/cluster/internal/controlplane"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// apiMetadataKey is the gRPC metadata key that carries the raw API key.
const apiMetadataKey = "x-api-key"

// TenantIDKey is the exported context key for tenant identification.
// It is identical to the internal ctxKeyTenant used by the gRPC adapter
// and TenantFromContext.
var TenantIDKey = ctxKeyTenant

// AuthInterceptor returns a gRPC unary server interceptor that validates
// the "x-api-key" metadata header. On success the resolved tenant ID is
// injected into the request context. On failure an Unauthenticated error
// is returned.
func AuthInterceptor(apiKeyManager *controlplane.APIKeyManager) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		tenantID, err := extractAndValidate(ctx, apiKeyManager)
		if err != nil {
			return nil, err
		}

		ctx = context.WithValue(ctx, ctxKeyTenant, tenantID)
		return handler(ctx, req)
	}
}

// StreamAuthInterceptor returns a gRPC stream server interceptor that validates
// the "x-api-key" metadata header. On success the resolved tenant ID is injected
// into the stream context. On failure an Unauthenticated error is returned.
func StreamAuthInterceptor(apiKeyManager *controlplane.APIKeyManager) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		tenantID, err := extractAndValidate(ss.Context(), apiKeyManager)
		if err != nil {
			return err
		}

		wrapped := &tenantServerStream{
			ServerStream: ss,
			ctx:          context.WithValue(ss.Context(), ctxKeyTenant, tenantID),
		}
		return handler(srv, wrapped)
	}
}

// extractAndValidate pulls the "x-api-key" from incoming gRPC metadata
// and validates it against the API key store. Returns the owning tenant ID.
func extractAndValidate(ctx context.Context, mgr *controlplane.APIKeyManager) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing gRPC metadata")
	}

	vals := md.Get(apiMetadataKey)
	if len(vals) == 0 || vals[0] == "" {
		return "", status.Error(codes.Unauthenticated, "missing x-api-key header")
	}

	tenantID, err := mgr.ValidateKey(ctx, vals[0])
	if err != nil {
		return "", status.Errorf(codes.Unauthenticated, "authentication failed: %v", err)
	}

	return tenantID, nil
}

// tenantServerStream wraps grpc.ServerStream to override Context().
// This is the standard pattern for injecting values into stream contexts.
type tenantServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *tenantServerStream) Context() context.Context {
	return w.ctx
}
