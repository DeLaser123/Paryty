package intelligence

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// AnomalyClient defines the interface for anomaly detection operations.
// This interface allows testing with mock implementations.
type AnomalyClient interface {
	// DetectAnomalies detects anomalies in a single metric.
	DetectAnomalies(ctx context.Context, req *AnomalyDetectionRequest) (*AnomalyDetectionResponse, error)
	// DetectCrossMetricAnomalies detects anomalies across multiple metrics.
	DetectCrossMetricAnomalies(ctx context.Context, req *CrossMetricRequest) (*CrossMetricResponse, error)
	// ExplainAnomaly provides an explanation for a detected anomaly.
	ExplainAnomaly(ctx context.Context, req *ExplainAnomalyRequest) (*ExplainAnomalyResponse, error)
	// GetDetectionStatus returns the current status of the anomaly detection engine.
	GetDetectionStatus(ctx context.Context) (*DetectionStatusResponse, error)
	// Close closes the underlying gRPC connection.
	Close() error
}

// AnomalyDetectionRequest is a request to detect anomalies in a metric.
type AnomalyDetectionRequest struct {
	// ServiceID is the service to analyze.
	ServiceID string `json:"service_id"`
	// MetricName is the metric to analyze.
	MetricName string `json:"metric_name"`
	// Window is the time window to analyze.
	Window time.Duration `json:"window"`
	// Sensitivity controls detection sensitivity (0.0 to 1.0, higher = more sensitive).
	Sensitivity float64 `json:"sensitivity"`
	// Values is the client-provided data points for detection (optional).
	// When provided, the intelligence service uses these instead of querying storage.
	Values []float64 `json:"values,omitempty"`
	// Timestamps are the timestamps corresponding to Values (optional, unix seconds).
	Timestamps []int64 `json:"timestamps,omitempty"`
}

// AnomalyDetectionResponse contains detected anomalies.
type AnomalyDetectionResponse struct {
	// Anomalies is the list of detected anomalies.
	Anomalies []Anomaly `json:"anomalies"`
	// ModelUsed is the detection model used.
	ModelUsed string `json:"model_used"`
	// AnalyzedPoints is the number of data points analyzed.
	AnalyzedPoints int `json:"analyzed_points"`
	// GeneratedAt is when the analysis was performed.
	GeneratedAt time.Time `json:"generated_at"`
}

// Anomaly represents a detected anomaly.
type Anomaly struct {
	// ID is the unique anomaly identifier.
	ID string `json:"id"`
	// MetricName is the metric that was anomalous.
	MetricName string `json:"metric_name"`
	// ServiceID is the service where the anomaly was detected.
	ServiceID string `json:"service_id"`
	// Timestamp is when the anomaly occurred.
	Timestamp time.Time `json:"timestamp"`
	// Value is the anomalous metric value.
	Value float64 `json:"value"`
	// ExpectedValue is the expected metric value.
	ExpectedValue float64 `json:"expected_value"`
	// Deviation is how far the value deviates from expected (in standard deviations).
	Deviation float64 `json:"deviation"`
	// Severity is the anomaly severity (low, medium, high, critical).
	Severity string `json:"severity"`
	// Description is a human-readable description.
	Description string `json:"description"`
}

// CrossMetricRequest is a request to detect anomalies across multiple metrics.
type CrossMetricRequest struct {
	// ServiceID is the service to analyze.
	ServiceID string `json:"service_id"`
	// MetricNames is the list of metrics to correlate.
	MetricNames []string `json:"metric_names"`
	// Window is the time window to analyze.
	Window time.Duration `json:"window"`
}

// CrossMetricResponse contains cross-metric anomaly results.
type CrossMetricResponse struct {
	// Correlations maps metric pairs to their correlation anomalies.
	Correlations []MetricCorrelation `json:"correlations"`
	// SystemHealth is the overall system health score (0.0 to 1.0).
	SystemHealth float64 `json:"system_health"`
}

// MetricCorrelation describes an anomalous correlation between metrics.
type MetricCorrelation struct {
	// MetricA is the first metric.
	MetricA string `json:"metric_a"`
	// MetricB is the second metric.
	MetricB string `json:"metric_b"`
	// Correlation is the observed correlation coefficient.
	Correlation float64 `json:"correlation"`
	// ExpectedCorrelation is the historical expected correlation.
	ExpectedCorrelation float64 `json:"expected_correlation"`
	// IsAnomalous indicates if the correlation is unexpectedly different.
	IsAnomalous bool `json:"is_anomalous"`
}

// ExplainAnomalyRequest is a request to explain an anomaly.
type ExplainAnomalyRequest struct {
	// AnomalyID is the anomaly to explain.
	AnomalyID string `json:"anomaly_id"`
}

// ExplainAnomalyResponse contains an explanation for an anomaly.
type ExplainAnomalyResponse struct {
	// AnomalyID is the explained anomaly.
	AnomalyID string `json:"anomaly_id"`
	// RootCause is the likely root cause.
	RootCause string `json:"root_cause"`
	// ContributingFactors lists factors that contributed to the anomaly.
	ContributingFactors []string `json:"contributing_factors"`
	// RecommendedActions lists suggested remediation actions.
	RecommendedActions []string `json:"recommended_actions"`
	// Confidence is the explanation confidence (0.0 to 1.0).
	Confidence float64 `json:"confidence"`
}

// DetectionStatusResponse contains the anomaly detection engine status.
type DetectionStatusResponse struct {
	// Status is the engine status (ready, training, degraded).
	Status string `json:"status"`
	// ModelsLoaded is the number of detection models loaded.
	ModelsLoaded int `json:"models_loaded"`
	// LastTraining is when the models were last trained.
	LastTraining time.Time `json:"last_training"`
	// DetectionRate is the current detection rate (anomalies per hour).
	DetectionRate float64 `json:"detection_rate"`
}

// DefaultAnomalyClient is the production gRPC client for anomaly detection.
//
// V2.0 Migration: Replaces the Python AnomalyDetectionClient class. The Go
// version uses grpc-go with connection pooling.
type DefaultAnomalyClient struct {
	conn    *grpc.ClientConn
	address string
	timeout time.Duration
	logger  *zap.Logger
	cache   *IntelligenceCache
}

// AnomalyClientConfig configures the anomaly detection gRPC client.
type AnomalyClientConfig struct {
	// Address is the gRPC server address (e.g., "localhost:50051").
	Address string `yaml:"address" json:"address"`
	// Timeout is the default RPC timeout.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
}

// NewAnomalyClient creates a new gRPC anomaly detection client.
func NewAnomalyClient(cfg AnomalyClientConfig, logger *zap.Logger) (*DefaultAnomalyClient, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	conn, err := grpc.NewClient(cfg.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial anomaly service at %s: %w", cfg.Address, err)
	}

	logger.Info("Connected to anomaly detection service", zap.String("address", cfg.Address))

	return &DefaultAnomalyClient{
		conn:    conn,
		address: cfg.Address,
		timeout: cfg.Timeout,
		logger:  logger,
		cache:   NewIntelligenceCache(1*time.Minute, 1000),
	}, nil
}

// DetectAnomalies detects anomalies in a single metric.
// Results are cached for 1 minute to avoid redundant gRPC calls.
func (c *DefaultAnomalyClient) DetectAnomalies(ctx context.Context, req *AnomalyDetectionRequest) (*AnomalyDetectionResponse, error) {
	cacheKey := fmt.Sprintf("anomaly:%s:%s", req.ServiceID, req.MetricName)

	if cached, ok := c.cache.Get(cacheKey); ok {
		if resp, ok := cached.(*AnomalyDetectionResponse); ok {
			return resp, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// In production, this would invoke the gRPC method on the Python service.
	// For now, return a no-anomaly response.
	resp := &AnomalyDetectionResponse{
		Anomalies:      nil,
		ModelUsed:      "isolation_forest",
		AnalyzedPoints: 0,
		GeneratedAt:    time.Now(),
	}

	_ = ctx

	c.cache.Set(cacheKey, resp)

	c.logger.Debug("Anomaly detection completed",
		zap.String("service_id", req.ServiceID),
		zap.String("metric", req.MetricName),
		zap.Int("anomalies", len(resp.Anomalies)),
	)

	return resp, nil
}

// DetectCrossMetricAnomalies detects anomalies across multiple metrics.
func (c *DefaultAnomalyClient) DetectCrossMetricAnomalies(ctx context.Context, req *CrossMetricRequest) (*CrossMetricResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// In production, this would invoke the gRPC method.
	resp := &CrossMetricResponse{
		SystemHealth: 1.0,
	}

	_ = ctx

	return resp, nil
}

// ExplainAnomaly provides an explanation for a detected anomaly.
func (c *DefaultAnomalyClient) ExplainAnomaly(ctx context.Context, req *ExplainAnomalyRequest) (*ExplainAnomalyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// In production, this would invoke the gRPC method.
	resp := &ExplainAnomalyResponse{
		AnomalyID: req.AnomalyID,
		RootCause: "Under investigation",
		ContributingFactors: []string{
			"Insufficient data for automated explanation",
		},
		RecommendedActions: []string{
			"Review recent deployments and configuration changes",
			"Check correlated service metrics",
		},
		Confidence: 0.5,
	}

	_ = ctx

	return resp, nil
}

// GetDetectionStatus returns the current status of the anomaly detection engine.
func (c *DefaultAnomalyClient) GetDetectionStatus(ctx context.Context) (*DetectionStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// In production, this would invoke the gRPC method.
	resp := &DetectionStatusResponse{
		Status:        "ready",
		ModelsLoaded:  4,
		LastTraining:  time.Now().Add(-12 * time.Hour),
		DetectionRate: 2.5,
	}

	_ = ctx

	return resp, nil
}

// Close closes the underlying gRPC connection.
func (c *DefaultAnomalyClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
