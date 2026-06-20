// Package intelligence implements gRPC clients for the Python intelligence
// service. It provides forecasting, anomaly detection, and caching for
// intelligence results.
//
// These clients use the compiled protobuf stubs to communicate with the
// Python intelligence gRPC service over the network.
package intelligence

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
)

// ForecastClient defines the interface for forecasting operations.
// This interface allows testing with mock implementations.
type ForecastClient interface {
	// ForecastMetric generates a forecast for a single metric.
	ForecastMetric(ctx context.Context, req *ForecastRequest) (*ForecastResponse, error)
	// ForecastBatch generates forecasts for multiple metrics in a single call.
	ForecastBatch(ctx context.Context, req *ForecastBatchRequest) (*ForecastBatchResponse, error)
	// GetModelAccuracy returns the accuracy metrics for the forecasting model.
	GetModelAccuracy(ctx context.Context, req *ModelAccuracyRequest) (*ModelAccuracyResponse, error)
	// RetrainModels triggers a retraining of the forecasting models.
	RetrainModels(ctx context.Context, req *RetrainRequest) (*RetrainResponse, error)
	// Close closes the underlying gRPC connection.
	Close() error
}

// ForecastRequest is a request to forecast a single metric.
type ForecastRequest struct {
	// AgentID is the agent to forecast for.
	AgentID string `json:"agent_id"`
	// TenantID scopes the request to a tenant.
	TenantID string `json:"tenant_id"`
	// ServiceID is the service to forecast (mapped to metric_name).
	ServiceID string `json:"service_id"`
	// MetricName is the metric to forecast.
	MetricName string `json:"metric_name"`
	// Horizon is how far into the future to forecast.
	Horizon time.Duration `json:"horizon"`
	// ConfidenceLevel is the confidence interval (0.0 to 1.0).
	ConfidenceLevel float64 `json:"confidence_level"`
	// StepSeconds is the time step between forecast points.
	StepSeconds int32 `json:"step_seconds"`
}

// ForecastResponse contains the forecast result for a single metric.
type ForecastResponse struct {
	// ServiceID is the service that was forecasted.
	ServiceID string `json:"service_id"`
	// MetricName is the metric that was forecasted.
	MetricName string `json:"metric_name"`
	// Points are the forecasted data points.
	Points []ForecastPoint `json:"points"`
	// Model is the best model used for forecasting.
	Model string `json:"model"`
	// ModelWeights maps model names to ensemble weights.
	ModelWeights map[string]float64 `json:"model_weights"`
	// ModelAccuracy maps model names to accuracy (MAPE).
	ModelAccuracy map[string]float64 `json:"model_accuracy"`
	// ConfidenceScore is the model's confidence (0.0 to 1.0).
	ConfidenceScore float64 `json:"confidence_score"`
	// TrainingSamples is the number of training data points.
	TrainingSamples int64 `json:"training_samples"`
	// GeneratedAt is when the forecast was generated.
	GeneratedAt time.Time `json:"generated_at"`
}

// ForecastPoint is a single forecasted data point.
type ForecastPoint struct {
	// Timestamp is the forecasted time.
	Timestamp time.Time `json:"timestamp"`
	// PredictedValue is the predicted metric value.
	PredictedValue float64 `json:"predicted_value"`
	// LowerBound is the lower confidence bound.
	LowerBound float64 `json:"lower_bound"`
	// UpperBound is the upper confidence bound.
	UpperBound float64 `json:"upper_bound"`
}

// ForecastBatchRequest is a request to forecast multiple metrics.
type ForecastBatchRequest struct {
	// Requests contains individual forecast requests.
	Requests []ForecastRequest `json:"requests"`
}

// ForecastBatchResponse contains results for multiple forecasts.
type ForecastBatchResponse struct {
	// Results contains individual forecast responses.
	Results []ForecastResponse `json:"results"`
}

// ModelAccuracyRequest is a request to get model accuracy.
type ModelAccuracyRequest struct {
	// TenantID scopes the request to a tenant.
	TenantID string `json:"tenant_id"`
	// MetricName is the metric to check (empty = all).
	MetricName string `json:"metric_name"`
}

// ModelAccuracyResponse contains model accuracy metrics.
type ModelAccuracyResponse struct {
	// Models maps metric names to their model info.
	Models map[string]*ForecastResponse `json:"models"`
}

// RetrainRequest is a request to retrain forecasting models.
type RetrainRequest struct {
	// TenantID scopes the request to a tenant.
	TenantID string `json:"tenant_id"`
	// MetricName is the metric to retrain (empty = all).
	MetricName string `json:"metric_name"`
	// Force forces retraining even if the model is recent.
	Force bool `json:"force"`
}

// RetrainResponse contains the result of a retrain request.
type RetrainResponse struct {
	// Success indicates whether retraining was initiated.
	Success bool `json:"success"`
	// Message contains additional information.
	Message string `json:"message"`
}

// ForecastClientConfig configures the forecast gRPC client.
type ForecastClientConfig struct {
	// Address is the gRPC server address (e.g., "localhost:50051").
	Address string `yaml:"address" json:"address"`
	// Timeout is the default RPC timeout.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	// UseTLS enables TLS for the gRPC connection.
	UseTLS bool `yaml:"use_tls" json:"use_tls"`
	// TLSCertPath is the path to the TLS certificate file.
	TLSCertPath string `yaml:"tls_cert_path" json:"tls_cert_path"`
}

// DefaultForecastClient is the production gRPC client for the forecasting service.
type DefaultForecastClient struct {
	conn    *grpc.ClientConn
	client  parytyv1.ForecastingServiceClient
	address string
	timeout time.Duration
	logger  *zap.Logger
	cache   *IntelligenceCache
}

// NewForecastClient creates a new gRPC forecast client.
func NewForecastClient(cfg ForecastClientConfig, logger *zap.Logger) (*DefaultForecastClient, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	var creds credentials.TransportCredentials
	if cfg.UseTLS {
		if cfg.TLSCertPath != "" {
			certPool, err := credentials.NewClientTLSFromFile(cfg.TLSCertPath, "")
			if err != nil {
				return nil, fmt.Errorf("load TLS cert %s: %w", cfg.TLSCertPath, err)
			}
			creds = certPool
		} else {
			creds = credentials.NewTLS(nil)
		}
	} else {
		creds = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("dial forecast service at %s: %w", cfg.Address, err)
	}

	client := parytyv1.NewForecastingServiceClient(conn)
	logger.Info("Connected to forecast service", zap.String("address", cfg.Address))

	return &DefaultForecastClient{
		conn:    conn,
		client:  client,
		address: cfg.Address,
		timeout: cfg.Timeout,
		logger:  logger,
		cache:   NewIntelligenceCache(5*time.Minute, 1000),
	}, nil
}

// ForecastMetric generates a forecast for a single metric.
// Results are cached for 5 minutes to avoid redundant gRPC calls.
func (c *DefaultForecastClient) ForecastMetric(ctx context.Context, req *ForecastRequest) (*ForecastResponse, error) {
	cacheKey := fmt.Sprintf("forecast:%s:%s:%s", req.TenantID, req.AgentID, req.MetricName)

	if cached, ok := c.cache.Get(cacheKey); ok {
		c.logger.Debug("Forecast cache hit", zap.String("key", cacheKey))
		if resp, ok := cached.(*ForecastResponse); ok {
			return resp, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	protoReq := &parytyv1.ForecastMetricRequest{
		AgentId:          req.AgentID,
		TenantId:         req.TenantID,
		MetricName:       req.MetricName,
		HorizonSeconds:   int64(req.Horizon.Seconds()),
		ConfidenceLevel:  int32(req.ConfidenceLevel * 100), // 0.95 → 95
		StepSeconds:      req.StepSeconds,
	}
	if protoReq.StepSeconds == 0 {
		protoReq.StepSeconds = 300 // default 5 minutes
	}

	protoResp, err := c.client.ForecastMetric(ctx, protoReq)
	if err != nil {
		return nil, fmt.Errorf("ForecastMetric RPC failed: %w", err)
	}

	resp := convertForecastResponse(protoResp)
	c.cache.Set(cacheKey, resp)

	c.logger.Info("Forecast generated",
		zap.String("agent_id", req.AgentID),
		zap.String("metric", req.MetricName),
		zap.Int("points", len(resp.Points)),
		zap.Float64("confidence", resp.ConfidenceScore),
	)

	return resp, nil
}

// ForecastBatch generates forecasts for multiple metrics in a single call.
func (c *DefaultForecastClient) ForecastBatch(ctx context.Context, req *ForecastBatchRequest) (*ForecastBatchResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	protoReqs := make([]*parytyv1.ForecastMetricRequest, 0, len(req.Requests))
	for _, r := range req.Requests {
		protoReqs = append(protoReqs, &parytyv1.ForecastMetricRequest{
			AgentId:        r.AgentID,
			TenantId:       r.TenantID,
			MetricName:     r.MetricName,
			HorizonSeconds: int64(r.Horizon.Seconds()),
			ConfidenceLevel: int32(r.ConfidenceLevel * 100),
			StepSeconds:    r.StepSeconds,
		})
	}

	protoResp, err := c.client.ForecastBatch(ctx, &parytyv1.ForecastBatchRequest{
		Requests: protoReqs,
	})
	if err != nil {
		return nil, fmt.Errorf("ForecastBatch RPC failed: %w", err)
	}

	results := make([]ForecastResponse, 0, len(protoResp.Forecasts))
	for _, fr := range protoResp.Forecasts {
		results = append(results, *convertForecastResponse(fr))
	}

	return &ForecastBatchResponse{Results: results}, nil
}

// GetModelAccuracy returns accuracy metrics for the forecasting model.
func (c *DefaultForecastClient) GetModelAccuracy(ctx context.Context, req *ModelAccuracyRequest) (*ModelAccuracyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	protoResp, err := c.client.GetModelAccuracy(ctx, &parytyv1.GetModelAccuracyRequest{
		MetricName: req.MetricName,
		TenantId:   req.TenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("GetModelAccuracy RPC failed: %w", err)
	}

	models := make(map[string]*ForecastResponse, len(protoResp.Models))
	for metricName, mi := range protoResp.Models {
		weights := make(map[string]float64, len(mi.Weights))
		for k, v := range mi.Weights {
			weights[k] = float64(v)
		}
		accuracy := make(map[string]float64, len(mi.Accuracy))
		for k, v := range mi.Accuracy {
			accuracy[k] = float64(v)
		}
		models[metricName] = &ForecastResponse{
			Model:           mi.BestModel,
			ModelWeights:    weights,
			ModelAccuracy:   accuracy,
			TrainingSamples: mi.TrainingSamples,
			GeneratedAt:     time.Unix(mi.LastTrained, 0),
		}
	}

	return &ModelAccuracyResponse{Models: models}, nil
}

// RetrainModels triggers model retraining.
func (c *DefaultForecastClient) RetrainModels(ctx context.Context, req *RetrainRequest) (*RetrainResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Invalidate cache for this tenant/metric
	if req.MetricName != "" {
		c.cache.Delete(fmt.Sprintf("forecast:%s::%s", req.TenantID, req.MetricName))
	} else {
		c.cache.DeleteByPrefix(fmt.Sprintf("forecast:%s:", req.TenantID))
	}

	protoResp, err := c.client.RetrainModels(ctx, &parytyv1.RetrainModelsRequest{
		MetricName: req.MetricName,
		TenantId:   req.TenantID,
		Force:      req.Force,
	})
	if err != nil {
		return nil, fmt.Errorf("RetrainModels RPC failed: %w", err)
	}

	c.logger.Info("Model retraining completed",
		zap.String("tenant_id", req.TenantID),
		zap.String("metric", req.MetricName),
		zap.Bool("force", req.Force),
		zap.Bool("success", protoResp.Success),
	)

	return &RetrainResponse{
		Success: protoResp.Success,
		Message: protoResp.Message,
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *DefaultForecastClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// convertForecastResponse converts a proto ForecastMetricResponse to our local type.
func convertForecastResponse(protoResp *parytyv1.ForecastMetricResponse) *ForecastResponse {
	points := make([]ForecastPoint, 0, len(protoResp.Forecast))
	for _, fp := range protoResp.Forecast {
		points = append(points, ForecastPoint{
			Timestamp:      time.Unix(fp.Timestamp, 0),
			PredictedValue: float64(fp.Value),
			LowerBound:     float64(fp.LowerBound),
			UpperBound:     float64(fp.UpperBound),
		})
	}

	resp := &ForecastResponse{
		ServiceID:       protoResp.AgentId,
		MetricName:      protoResp.MetricName,
		Points:          points,
		ConfidenceScore: float64(protoResp.OverallConfidence),
		GeneratedAt:     time.Now(),
	}

	if protoResp.ModelInfo != nil {
		resp.Model = protoResp.ModelInfo.BestModel
		resp.ModelWeights = make(map[string]float64, len(protoResp.ModelInfo.Weights))
		for k, v := range protoResp.ModelInfo.Weights {
			resp.ModelWeights[k] = float64(v)
		}
		resp.ModelAccuracy = make(map[string]float64, len(protoResp.ModelInfo.Accuracy))
		for k, v := range protoResp.ModelInfo.Accuracy {
			resp.ModelAccuracy[k] = float64(v)
		}
		resp.TrainingSamples = protoResp.ModelInfo.TrainingSamples
		if protoResp.ModelInfo.LastTrained > 0 {
			resp.GeneratedAt = time.Unix(protoResp.ModelInfo.LastTrained, 0)
		}
	}

	return resp
}

// isRetryable returns true if the error represents a transient gRPC failure.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
		return true
	default:
		return false
	}
}
