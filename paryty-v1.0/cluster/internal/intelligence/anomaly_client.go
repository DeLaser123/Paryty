package intelligence

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	parytyv1 "github.com/paryty/paryty-v1.0/cluster/internal/proto"
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
	// AgentID is the agent to check.
	AgentID string `json:"agent_id"`
	// TenantID scopes the request to a tenant.
	TenantID string `json:"tenant_id"`
	// ServiceID is the service to analyze.
	ServiceID string `json:"service_id"`
	// MetricName is the metric to analyze.
	MetricName string `json:"metric_name"`
	// Sensitivity controls detection sensitivity (0.0 to 1.0, higher = more sensitive).
	Sensitivity float64 `json:"sensitivity"`
	// Values is the client-provided data points for detection (optional).
	Values []float64 `json:"values,omitempty"`
	// Timestamps are the timestamps corresponding to Values (optional, unix seconds).
	Timestamps []int64 `json:"timestamps,omitempty"`
}

// AnomalyDetectionResponse contains detected anomalies.
type AnomalyDetectionResponse struct {
	// Anomalies is the list of detected anomalies.
	Anomalies []Anomaly `json:"anomalies"`
	// OverallScore is the overall anomaly score (0.0 to 1.0).
	OverallScore float64 `json:"overall_score"`
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
	// Score is the anomaly score (0.0 to 1.0).
	Score float64 `json:"score"`
	// Type is the anomaly type (point, contextual, collective, trend).
	Type string `json:"type"`
	// Severity is the anomaly severity (info, medium, critical).
	Severity string `json:"severity"`
	// Description is a human-readable description.
	Description string `json:"description"`
	// DetectionMethod is the detector that found this anomaly.
	DetectionMethod string `json:"detection_method"`
	// ContributingFactors lists factors that contributed to the anomaly.
	ContributingFactors []string `json:"contributing_factors"`
}

// CrossMetricRequest is a request to detect anomalies across multiple metrics.
type CrossMetricRequest struct {
	// AgentID is the agent to check.
	AgentID string `json:"agent_id"`
	// TenantID scopes the request to a tenant.
	TenantID string `json:"tenant_id"`
	// ServiceID is the service to analyze.
	ServiceID string `json:"service_id"`
	// MetricData maps metric names to their values.
	MetricData map[string][]float64 `json:"metric_data"`
	// Timestamps are the timestamps for the data points.
	Timestamps []int64 `json:"timestamps"`
}

// CrossMetricResponse contains cross-metric anomaly results.
type CrossMetricResponse struct {
	// Anomalies is the list of detected anomalies.
	Anomalies []Anomaly `json:"anomalies"`
	// Correlations is the list of correlation anomalies.
	Correlations []MetricCorrelation `json:"correlations"`
}

// MetricCorrelation describes an anomalous correlation between metrics.
type MetricCorrelation struct {
	// MetricA is the first metric.
	MetricA string `json:"metric_a"`
	// MetricB is the second metric.
	MetricB string `json:"metric_b"`
	// Description is a human-readable description.
	Description string `json:"description"`
	// Severity is the severity score (0.0 to 1.0).
	Severity float64 `json:"severity"`
}

// ExplainAnomalyRequest is a request to explain an anomaly.
type ExplainAnomalyRequest struct {
	// AgentID is the agent where the anomaly occurred.
	AgentID string `json:"agent_id"`
	// TenantID scopes the request to a tenant.
	TenantID string `json:"tenant_id"`
	// MetricName is the metric that was anomalous.
	MetricName string `json:"metric_name"`
	// Timestamp is when the anomaly occurred (unix seconds).
	Timestamp int64 `json:"timestamp"`
}

// ExplainAnomalyResponse contains an explanation for an anomaly.
type ExplainAnomalyResponse struct {
	// Anomaly contains the full anomaly details.
	Anomaly *Anomaly `json:"anomaly"`
	// SimilarIncidents lists similar past incidents.
	SimilarIncidents []string `json:"similar_incidents"`
	// RecommendedActions lists suggested remediation actions.
	RecommendedActions []string `json:"recommended_actions"`
}

// DetectionStatusResponse contains the anomaly detection engine status.
type DetectionStatusResponse struct {
	// Models maps model names to their status.
	Models map[string]ModelStatus `json:"models"`
	// LastTraining is when the models were last trained.
	LastTraining time.Time `json:"last_training"`
	// AnomaliesDetected24h is the count in the last 24 hours.
	AnomaliesDetected24h int `json:"anomalies_detected_24h"`
	// FalsePositiveRate is the current false positive rate.
	FalsePositiveRate float64 `json:"false_positive_rate"`
}

// ModelStatus represents the status of a single detection model.
type ModelStatus struct {
	// Name is the model name.
	Name string `json:"name"`
	// Trained indicates whether the model is ready.
	Trained bool `json:"trained"`
	// Accuracy is the model accuracy (0.0 to 1.0).
	Accuracy float64 `json:"accuracy"`
	// LastUpdated is when the model was last updated.
	LastUpdated time.Time `json:"last_updated"`
}

// AnomalyClientConfig configures the anomaly detection gRPC client.
type AnomalyClientConfig struct {
	// Address is the gRPC server address (e.g., "localhost:50051").
	Address string `yaml:"address" json:"address"`
	// Timeout is the default RPC timeout.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	// UseTLS enables TLS for the gRPC connection.
	UseTLS bool `yaml:"use_tls" json:"use_tls"`
	// TLSCertPath is the path to the TLS certificate file.
	TLSCertPath string `yaml:"tls_cert_path" json:"tls_cert_path"`
}

// DefaultAnomalyClient is the production gRPC client for anomaly detection.
type DefaultAnomalyClient struct {
	conn    *grpc.ClientConn
	client  parytyv1.AnomalyDetectionServiceClient
	address string
	timeout time.Duration
	logger  *zap.Logger
	cache   *IntelligenceCache
}

// NewAnomalyClient creates a new gRPC anomaly detection client.
func NewAnomalyClient(cfg AnomalyClientConfig, logger *zap.Logger) (*DefaultAnomalyClient, error) {
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
		return nil, fmt.Errorf("dial anomaly service at %s: %w", cfg.Address, err)
	}

	client := parytyv1.NewAnomalyDetectionServiceClient(conn)
	logger.Info("Connected to anomaly detection service", zap.String("address", cfg.Address))

	return &DefaultAnomalyClient{
		conn:    conn,
		client:  client,
		address: cfg.Address,
		timeout: cfg.Timeout,
		logger:  logger,
		cache:   NewIntelligenceCache(1*time.Minute, 1000),
	}, nil
}

// DetectAnomalies detects anomalies in a single metric.
// Results are cached for 1 minute to avoid redundant gRPC calls.
func (c *DefaultAnomalyClient) DetectAnomalies(ctx context.Context, req *AnomalyDetectionRequest) (*AnomalyDetectionResponse, error) {
	cacheKey := fmt.Sprintf("anomaly:%s:%s:%s", req.TenantID, req.AgentID, req.MetricName)

	if cached, ok := c.cache.Get(cacheKey); ok {
		if resp, ok := cached.(*AnomalyDetectionResponse); ok {
			return resp, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	protoReq := &parytyv1.DetectAnomaliesRequest{
		AgentId:    req.AgentID,
		TenantId:   req.TenantID,
		MetricName: req.MetricName,
		Values:     toFloat32Slice(req.Values),
		Timestamps: req.Timestamps,
		Sensitivity: float32(req.Sensitivity),
	}

	protoResp, err := c.client.DetectAnomalies(ctx, protoReq)
	if err != nil {
		return nil, fmt.Errorf("DetectAnomalies RPC failed: %w", err)
	}

	resp := convertAnomalyDetectionResponse(protoResp)
	c.cache.Set(cacheKey, resp)

	c.logger.Debug("Anomaly detection completed",
		zap.String("agent_id", req.AgentID),
		zap.String("metric", req.MetricName),
		zap.Int("anomalies", len(resp.Anomalies)),
	)

	return resp, nil
}

// DetectCrossMetricAnomalies detects anomalies across multiple metrics.
func (c *DefaultAnomalyClient) DetectCrossMetricAnomalies(ctx context.Context, req *CrossMetricRequest) (*CrossMetricResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	metrics := make(map[string]*parytyv1.FloatArray, len(req.MetricData))
	for name, vals := range req.MetricData {
		metrics[name] = &parytyv1.FloatArray{Values: toFloat32Slice(vals)}
	}

	protoResp, err := c.client.DetectCrossMetricAnomalies(ctx, &parytyv1.DetectCrossMetricRequest{
		AgentId:   req.AgentID,
		TenantId:  req.TenantID,
		Metrics:   metrics,
		Timestamps: req.Timestamps,
	})
	if err != nil {
		return nil, fmt.Errorf("DetectCrossMetricAnomalies RPC failed: %w", err)
	}

	anomalies := make([]Anomaly, 0, len(protoResp.Anomalies))
	for _, a := range protoResp.Anomalies {
		anomalies = append(anomalies, convertAnomaly(a))
	}

	correlations := make([]MetricCorrelation, 0, len(protoResp.CorrelationAnomalies))
	for _, c := range protoResp.CorrelationAnomalies {
		correlations = append(correlations, MetricCorrelation{
			MetricA:     c.MetricA,
			MetricB:     c.MetricB,
			Description: c.Description,
			Severity:    float64(c.Severity),
		})
	}

	return &CrossMetricResponse{
		Anomalies:   anomalies,
		Correlations: correlations,
	}, nil
}

// ExplainAnomaly provides an explanation for a detected anomaly.
func (c *DefaultAnomalyClient) ExplainAnomaly(ctx context.Context, req *ExplainAnomalyRequest) (*ExplainAnomalyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	protoResp, err := c.client.ExplainAnomaly(ctx, &parytyv1.ExplainAnomalyRequest{
		AgentId:    req.AgentID,
		TenantId:   req.TenantID,
		MetricName: req.MetricName,
		Timestamp:  req.Timestamp,
	})
	if err != nil {
		return nil, fmt.Errorf("ExplainAnomaly RPC failed: %w", err)
	}

	var anomaly *Anomaly
	if protoResp.Anomaly != nil {
		a := convertAnomaly(protoResp.Anomaly)
		anomaly = &a
	}

	return &ExplainAnomalyResponse{
		Anomaly:            anomaly,
		SimilarIncidents:   protoResp.SimilarIncidents,
		RecommendedActions: protoResp.Recommendations,
	}, nil
}

// GetDetectionStatus returns the current status of the anomaly detection engine.
func (c *DefaultAnomalyClient) GetDetectionStatus(ctx context.Context) (*DetectionStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	protoResp, err := c.client.GetDetectionStatus(ctx, &parytyv1.GetDetectionStatusRequest{})
	if err != nil {
		return nil, fmt.Errorf("GetDetectionStatus RPC failed: %w", err)
	}

	models := make(map[string]ModelStatus, len(protoResp.Models))
	for name, ms := range protoResp.Models {
		models[name] = ModelStatus{
			Name:        ms.Name,
			Trained:     ms.Trained,
			Accuracy:    float64(ms.Accuracy),
			LastUpdated: time.Unix(ms.LastUpdated, 0),
		}
	}

	return &DetectionStatusResponse{
		Models:               models,
		LastTraining:         time.Unix(protoResp.LastTraining, 0),
		AnomaliesDetected24h: int(protoResp.AnomaliesDetected_24H),
		FalsePositiveRate:    float64(protoResp.FalsePositiveRate),
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *DefaultAnomalyClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// --- Conversion helpers ---

func convertAnomaly(protoA *parytyv1.Anomaly) Anomaly {
	return Anomaly{
		Timestamp:           time.Unix(protoA.Timestamp, 0),
		Value:               float64(protoA.Value),
		Score:               float64(protoA.Score),
		Type:                anomalyTypeName(protoA.Type),
		Severity:            severityName(protoA.Severity),
		Description:         protoA.Explanation,
		DetectionMethod:     protoA.DetectionMethod,
		ContributingFactors: protoA.ContributingFactors,
	}
}

func convertAnomalyDetectionResponse(protoResp *parytyv1.DetectAnomaliesResponse) *AnomalyDetectionResponse {
	anomalies := make([]Anomaly, 0, len(protoResp.Anomalies))
	for _, a := range protoResp.Anomalies {
		anomalies = append(anomalies, convertAnomaly(a))
	}
	return &AnomalyDetectionResponse{
		Anomalies:    anomalies,
		OverallScore: float64(protoResp.OverallScore),
		GeneratedAt:  time.Now(),
	}
}

func anomalyTypeName(t parytyv1.AnomalyType) string {
	switch t {
	case parytyv1.AnomalyType_ANOMALY_TYPE_POINT:
		return "point"
	case parytyv1.AnomalyType_ANOMALY_TYPE_CONTEXTUAL:
		return "contextual"
	case parytyv1.AnomalyType_ANOMALY_TYPE_COLLECTIVE:
		return "collective"
	case parytyv1.AnomalyType_ANOMALY_TYPE_TREND:
		return "trend"
	default:
		return "unspecified"
	}
}

func severityName(s parytyv1.Severity) string {
	switch s {
	case parytyv1.Severity_SEVERITY_CRITICAL:
		return "critical"
	case parytyv1.Severity_SEVERITY_HIGH:
		return "high"
	case parytyv1.Severity_SEVERITY_MEDIUM:
		return "medium"
	case parytyv1.Severity_SEVERITY_LOW:
		return "low"
	case parytyv1.Severity_SEVERITY_INFO:
		return "info"
	default:
		return "unspecified"
	}
}

func toFloat32Slice(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}
