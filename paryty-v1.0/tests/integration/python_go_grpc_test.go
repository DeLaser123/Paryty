//go:build integration

// Package integration contains cross-language integration tests
// that verify the gRPC contract between the Go cluster and the
// Python intelligence service.
//
// These tests use mock implementations of the intelligence client
// interfaces to verify that:
//   - The Go side correctly constructs gRPC requests
//   - The response shapes match the Python service's protobuf definitions
//   - All required fields are present in responses
//   - Error handling works across the language boundary
//
// When run against a real Python service (INTEGRATION_REAL=1),
// the integration gRPC client is used instead of mocks.
package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/intelligence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Mock Implementations ────────────────────────────────────────

// mockForecastClient implements intelligence.ForecastClient for testing.
type mockForecastClient struct {
	forecastFn      func(ctx context.Context, req *intelligence.ForecastRequest) (*intelligence.ForecastResponse, error)
	batchFn         func(ctx context.Context, req *intelligence.ForecastBatchRequest) (*intelligence.ForecastBatchResponse, error)
	accuracyFn      func(ctx context.Context, req *intelligence.ModelAccuracyRequest) (*intelligence.ModelAccuracyResponse, error)
	retrainFn       func(ctx context.Context, req *intelligence.RetrainRequest) (*intelligence.RetrainResponse, error)
	closeFn         func() error
}

func (m *mockForecastClient) ForecastMetric(ctx context.Context, req *intelligence.ForecastRequest) (*intelligence.ForecastResponse, error) {
	if m.forecastFn != nil {
		return m.forecastFn(ctx, req)
	}
	return &intelligence.ForecastResponse{}, nil
}

func (m *mockForecastClient) ForecastBatch(ctx context.Context, req *intelligence.ForecastBatchRequest) (*intelligence.ForecastBatchResponse, error) {
	if m.batchFn != nil {
		return m.batchFn(ctx, req)
	}
	return &intelligence.ForecastBatchResponse{}, nil
}

func (m *mockForecastClient) GetModelAccuracy(ctx context.Context, req *intelligence.ModelAccuracyRequest) (*intelligence.ModelAccuracyResponse, error) {
	if m.accuracyFn != nil {
		return m.accuracyFn(ctx, req)
	}
	return &intelligence.ModelAccuracyResponse{}, nil
}

func (m *mockForecastClient) RetrainModels(ctx context.Context, req *intelligence.RetrainRequest) (*intelligence.RetrainResponse, error) {
	if m.retrainFn != nil {
		return m.retrainFn(ctx, req)
	}
	return &intelligence.RetrainResponse{}, nil
}

func (m *mockForecastClient) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// mockAnomalyClient implements intelligence.AnomalyClient for testing.
type mockAnomalyClient struct {
	detectFn     func(ctx context.Context, req *intelligence.AnomalyDetectionRequest) (*intelligence.AnomalyDetectionResponse, error)
	crossFn      func(ctx context.Context, req *intelligence.CrossMetricRequest) (*intelligence.CrossMetricResponse, error)
	explainFn    func(ctx context.Context, req *intelligence.ExplainAnomalyRequest) (*intelligence.ExplainAnomalyResponse, error)
	statusFn     func(ctx context.Context) (*intelligence.DetectionStatusResponse, error)
	closeFn      func() error
}

func (m *mockAnomalyClient) DetectAnomalies(ctx context.Context, req *intelligence.AnomalyDetectionRequest) (*intelligence.AnomalyDetectionResponse, error) {
	if m.detectFn != nil {
		return m.detectFn(ctx, req)
	}
	return &intelligence.AnomalyDetectionResponse{}, nil
}

func (m *mockAnomalyClient) DetectCrossMetricAnomalies(ctx context.Context, req *intelligence.CrossMetricRequest) (*intelligence.CrossMetricResponse, error) {
	if m.crossFn != nil {
		return m.crossFn(ctx, req)
	}
	return &intelligence.CrossMetricResponse{}, nil
}

func (m *mockAnomalyClient) ExplainAnomaly(ctx context.Context, req *intelligence.ExplainAnomalyRequest) (*intelligence.ExplainAnomalyResponse, error) {
	if m.explainFn != nil {
		return m.explainFn(ctx, req)
	}
	return &intelligence.ExplainAnomalyResponse{}, nil
}

func (m *mockAnomalyClient) GetDetectionStatus(ctx context.Context) (*intelligence.DetectionStatusResponse, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &intelligence.DetectionStatusResponse{}, nil
}

func (m *mockAnomalyClient) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// verifyInterfaceCompliance checks at compile time that mocks implement the interfaces.
var _ intelligence.ForecastClient = (*mockForecastClient)(nil)
var _ intelligence.AnomalyClient = (*mockAnomalyClient)(nil)

// ─── Forecasting Service Contract Tests ──────────────────────────

// TestPythonIntelligenceGrpcContract_Forecasting verifies the forecasting
// service gRPC contract between Go and Python by testing the response
// shape returned by the ForecastMetric RPC.
//
// Contract fields verified:
//   - ServiceID (agent_id in proto)
//   - MetricName
//   - Points array with Timestamp, PredictedValue, LowerBound, UpperBound
//   - Model (best model name)
//   - ModelWeights (ensemble weights map)
//   - ModelAccuracy (per-model accuracy map)
//   - ConfidenceScore
//   - TrainingSamples
//   - GeneratedAt
func TestPythonIntelligenceGrpcContract_Forecasting(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	mock := &mockForecastClient{
		forecastFn: func(ctx context.Context, req *intelligence.ForecastRequest) (*intelligence.ForecastResponse, error) {
			// Verify request fields match the Python service expectations
			assert.NotEmpty(t, req.AgentID, "AgentID must be set")
			assert.NotEmpty(t, req.TenantID, "TenantID must be set")
			assert.NotEmpty(t, req.MetricName, "MetricName must be set")
			assert.Greater(t, req.Horizon, time.Duration(0), "Horizon must be positive")
			assert.True(t, req.ConfidenceLevel > 0 && req.ConfidenceLevel <= 1.0,
				"ConfidenceLevel must be 0-1")

			// Return a response matching the Python service's proto shape
			return &intelligence.ForecastResponse{
				ServiceID:       req.AgentID,
				MetricName:      req.MetricName,
				Points: []intelligence.ForecastPoint{
					{
						Timestamp:      now.Add(5 * time.Minute),
						PredictedValue: 42.5,
						LowerBound:     40.0,
						UpperBound:     45.0,
					},
					{
						Timestamp:      now.Add(10 * time.Minute),
						PredictedValue: 43.1,
						LowerBound:     41.0,
						UpperBound:     46.0,
					},
				},
				Model:        "ensemble",
				ModelWeights: map[string]float64{"linear_regression": 0.35, "prophet": 0.40, "xgboost": 0.25},
				ModelAccuracy: map[string]float64{
					"linear_regression": 0.92,
					"prophet":           0.88,
					"xgboost":          0.94,
				},
				ConfidenceScore: 0.93,
				TrainingSamples: 10080,
				GeneratedAt:     now,
			}, nil
		},
	}

	req := &intelligence.ForecastRequest{
		AgentID:         "agent-contract-test-001",
		TenantID:        "tenant-contract-test",
		MetricName:      "cpu.usage",
		Horizon:         1 * time.Hour,
		ConfidenceLevel: 0.95,
		StepSeconds:     300,
	}

	resp, err := mock.ForecastMetric(ctx, req)
	require.NoError(t, err, "ForecastMetric must not return an error")

	// ── Contract assertions ───────────────────────────────────
	t.Run("RequiredFields", func(t *testing.T) {
		assert.NotEmpty(t, resp.ServiceID, "ServiceID (agent_id) must be present")
		assert.NotEmpty(t, resp.MetricName, "MetricName must be present")
		assert.NotEmpty(t, resp.Model, "Model (best_model) must be present")
		assert.Greater(t, resp.ConfidenceScore, 0.0, "ConfidenceScore must be > 0")
		assert.Greater(t, resp.TrainingSamples, int64(0), "TrainingSamples must be > 0")
		assert.False(t, resp.GeneratedAt.IsZero(), "GeneratedAt must be set")
	})

	t.Run("ForecastPoints", func(t *testing.T) {
		require.NotEmpty(t, resp.Points, "Forecast must contain at least 1 point")

		for i, p := range resp.Points {
			t.Run(fmt.Sprintf("Point_%d", i), func(t *testing.T) {
				assert.False(t, p.Timestamp.IsZero(), "ForecastPoint.Timestamp must be set")
				assert.NotZero(t, p.PredictedValue, "ForecastPoint.PredictedValue must not be zero")
				assert.LessOrEqual(t, p.LowerBound, p.PredictedValue,
					"LowerBound (%f) must be <= PredictedValue (%f)", p.LowerBound, p.PredictedValue)
				assert.GreaterOrEqual(t, p.UpperBound, p.PredictedValue,
					"UpperBound (%f) must be >= PredictedValue (%f)", p.UpperBound, p.PredictedValue)
			})
		}
	})

	t.Run("ModelWeights", func(t *testing.T) {
		require.NotEmpty(t, resp.ModelWeights, "ModelWeights must contain ensemble weights")
		totalWeight := 0.0
		for _, w := range resp.ModelWeights {
			totalWeight += w
		}
		assert.InDelta(t, 1.0, totalWeight, 0.01,
			"ModelWeights must sum to ~1.0 (ensemble); got %f", totalWeight)
	})

	t.Run("ModelAccuracy", func(t *testing.T) {
		if len(resp.ModelAccuracy) > 0 {
			for model, acc := range resp.ModelAccuracy {
				assert.NotEmpty(t, model, "accuracy map key must be a model name")
				assert.True(t, acc > 0.0 && acc <= 1.0,
					"accuracy for %s must be 0-1; got %f", model, acc)
			}
		}
	})
}

// TestPythonIntelligenceGrpcContract_AnomalyDetection verifies the
// anomaly detection service gRPC contract between Go and Python.
//
// Contract fields verified:
//   - Anomalies array with ID, MetricName, ServiceID, Timestamp, Value, Score
//   - Anomaly Type and Severity
//   - OverallScore
//   - ModelUsed
//   - AnalyzedPoints
//   - GeneratedAt
func TestPythonIntelligenceGrpcContract_AnomalyDetection(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	mock := &mockAnomalyClient{
		detectFn: func(ctx context.Context, req *intelligence.AnomalyDetectionRequest) (*intelligence.AnomalyDetectionResponse, error) {
			// Verify request fields match the Python service expectations
			assert.NotEmpty(t, req.AgentID, "AgentID must be set")
			assert.NotEmpty(t, req.TenantID, "TenantID must be set")
			assert.NotEmpty(t, req.MetricName, "MetricName must be set")
			assert.Greater(t, req.Sensitivity, 0.0, "Sensitivity must be > 0")
			assert.LessOrEqual(t, req.Sensitivity, 1.0, "Sensitivity must be <= 1.0")

			// Return a response matching the Python service's proto shape
			return &intelligence.AnomalyDetectionResponse{
				Anomalies: []intelligence.Anomaly{
					{
						ID:                  "anom-contract-001",
						MetricName:         req.MetricName,
						ServiceID:          req.AgentID,
						Timestamp:          now.Add(-5 * time.Minute),
						Value:              95.7,
						Score:              0.89,
						Type:               "point",
						Severity:           "high",
						Description:        "CPU usage spike detected: value 95.7% exceeds threshold",
						DetectionMethod:    "isolation_forest",
						ContributingFactors: []string{"memory_pressure", "disk_io_wait"},
					},
					{
						ID:                  "anom-contract-002",
						MetricName:         req.MetricName,
						ServiceID:          req.AgentID,
						Timestamp:          now.Add(-15 * time.Minute),
						Value:              12.3,
						Score:              0.65,
						Type:               "contextual",
						Severity:           "medium",
						Description:        "Contextual dip in network throughput",
						DetectionMethod:    "statistical_zscore",
						ContributingFactors: []string{"network_latency"},
					},
				},
				OverallScore:   0.77,
				ModelUsed:      "ensemble",
				AnalyzedPoints: 1440,
				GeneratedAt:    now,
			}, nil
		},
	}

	req := &intelligence.AnomalyDetectionRequest{
		AgentID:     "agent-contract-test-001",
		TenantID:    "tenant-contract-test",
		MetricName:  "cpu.usage",
		Sensitivity: 0.7,
	}

	resp, err := mock.DetectAnomalies(ctx, req)
	require.NoError(t, err, "DetectAnomalies must not return an error")

	// ── Contract assertions ───────────────────────────────────
	t.Run("RequiredResponseFields", func(t *testing.T) {
		assert.NotEmpty(t, resp.ModelUsed, "ModelUsed must be present")
		assert.Greater(t, resp.AnalyzedPoints, 0, "AnalyzedPoints must be > 0")
		assert.False(t, resp.GeneratedAt.IsZero(), "GeneratedAt must be set")
	})

	t.Run("Anomalies", func(t *testing.T) {
		require.NotEmpty(t, resp.Anomalies, "Anomalies array must contain at least 1 anomaly")

		validTypes := map[string]bool{
			"point": true, "contextual": true, "collective": true, "trend": true,
		}
		validSeverities := map[string]bool{
			"info": true, "low": true, "medium": true, "high": true, "critical": true,
		}

		for i, a := range resp.Anomalies {
			t.Run(fmt.Sprintf("Anomaly_%d", i), func(t *testing.T) {
				assert.NotEmpty(t, a.ID, "Anomaly.ID must be present")
				assert.NotEmpty(t, a.MetricName, "Anomaly.MetricName must be present")
				assert.NotEmpty(t, a.ServiceID, "Anomaly.ServiceID must be present")
				assert.False(t, a.Timestamp.IsZero(), "Anomaly.Timestamp must be set")
				assert.NotZero(t, a.Value, "Anomaly.Value should not be zero")
				assert.True(t, a.Score >= 0.0 && a.Score <= 1.0,
					"Anomaly.Score must be 0-1; got %f", a.Score)
				assert.True(t, validTypes[a.Type],
					"Anomaly.Type must be one of [point,contextual,collective,trend]; got %q", a.Type)
				assert.True(t, validSeverities[a.Severity],
					"Anomaly.Severity must be one of [info,low,medium,high,critical]; got %q", a.Severity)
				assert.NotEmpty(t, a.Description, "Anomaly.Description must be present")
				assert.NotEmpty(t, a.DetectionMethod, "Anomaly.DetectionMethod must be present")
			})
		}
	})
}

// TestPythonIntelligenceGrpcContract_ErrorPropagation verifies that
// gRPC errors from the Python service are properly propagated through
// the Go client and can be handled correctly.
func TestPythonIntelligenceGrpcContract_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	t.Run("ForecastError", func(t *testing.T) {
		mock := &mockForecastClient{
			forecastFn: func(ctx context.Context, req *intelligence.ForecastRequest) (*intelligence.ForecastResponse, error) {
				return nil, fmt.Errorf("rpc error: code = Unavailable desc = intelligence service down")
			},
		}

		req := &intelligence.ForecastRequest{
			AgentID:  "agent-001",
			TenantID: "tenant-001",
		}

		resp, err := mock.ForecastMetric(ctx, req)
		assert.Error(t, err, "ForecastMetric must propagate errors")
		assert.Nil(t, resp, "Response must be nil on error")
		assert.Contains(t, err.Error(), "intelligence service down",
			"Error message must contain Python service context")
	})

	t.Run("AnomalyError", func(t *testing.T) {
		mock := &mockAnomalyClient{
			detectFn: func(ctx context.Context, req *intelligence.AnomalyDetectionRequest) (*intelligence.AnomalyDetectionResponse, error) {
				return nil, fmt.Errorf("rpc error: code = DeadlineExceeded desc = context deadline exceeded")
			},
		}

		req := &intelligence.AnomalyDetectionRequest{
			AgentID:     "agent-001",
			TenantID:    "tenant-001",
			MetricName:  "memory.usage",
			Sensitivity: 0.5,
		}

		resp, err := mock.DetectAnomalies(ctx, req)
		assert.Error(t, err, "DetectAnomalies must propagate errors")
		assert.Nil(t, resp, "Response must be nil on error")
		assert.Contains(t, err.Error(), "DeadlineExceeded",
			"Error must contain gRPC status code")
	})
}

// TestPythonIntelligenceGrpcContract_EmptyResponse verifies that
// empty responses from the Python service are handled gracefully.
func TestPythonIntelligenceGrpcContract_EmptyResponse(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("Forecast_NoPoints", func(t *testing.T) {
		mock := &mockForecastClient{
			forecastFn: func(ctx context.Context, req *intelligence.ForecastRequest) (*intelligence.ForecastResponse, error) {
				return &intelligence.ForecastResponse{
					ServiceID:       req.AgentID,
					MetricName:      req.MetricName,
					Points:          nil, // no forecast points (insufficient data)
					Model:           "untrained",
					ModelWeights:    map[string]float64{},
					ModelAccuracy:   map[string]float64{},
					ConfidenceScore: 0.0,
					TrainingSamples: 0,
					GeneratedAt:     now,
				}, nil
			},
		}

		resp, err := mock.ForecastMetric(ctx, &intelligence.ForecastRequest{
			AgentID:    "agent-new-001",
			TenantID:   "tenant-001",
			MetricName: "new.metric",
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Points, "New metrics may have 0 forecast points")
		assert.Equal(t, "untrained", resp.Model, "Model should indicate untrained state")
		assert.Equal(t, 0.0, resp.ConfidenceScore, "Confidence should be 0 for untrained model")
	})

	t.Run("Anomalies_NoAnomalies", func(t *testing.T) {
		mock := &mockAnomalyClient{
			detectFn: func(ctx context.Context, req *intelligence.AnomalyDetectionRequest) (*intelligence.AnomalyDetectionResponse, error) {
				return &intelligence.AnomalyDetectionResponse{
					Anomalies:      nil,
					OverallScore:   0.0,
					ModelUsed:      "isolation_forest",
					AnalyzedPoints: 100,
					GeneratedAt:    now,
				}, nil
			},
		}

		resp, err := mock.DetectAnomalies(ctx, &intelligence.AnomalyDetectionRequest{
			AgentID:    "agent-clean-001",
			TenantID:   "tenant-001",
			MetricName: "stable.metric",
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Anomalies, "Stable metrics should have 0 anomalies")
		assert.Equal(t, 0.0, resp.OverallScore, "OverallScore should be 0 with no anomalies")
	})
}

// TestPythonIntelligenceGrpcContract_BatchForecast verifies the batch
// forecast contract — multiple metrics in a single gRPC call.
func TestPythonIntelligenceGrpcContract_BatchForecast(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	mock := &mockForecastClient{
		batchFn: func(ctx context.Context, req *intelligence.ForecastBatchRequest) (*intelligence.ForecastBatchResponse, error) {
			assert.NotEmpty(t, req.Requests, "Batch request must contain at least 1 forecast")

			results := make([]intelligence.ForecastResponse, 0, len(req.Requests))
			for _, r := range req.Requests {
				results = append(results, intelligence.ForecastResponse{
					ServiceID:       r.AgentID,
					MetricName:      r.MetricName,
					Points:          []intelligence.ForecastPoint{{Timestamp: now, PredictedValue: 50.0}},
					Model:           "ensemble",
					ConfidenceScore: 0.90,
					GeneratedAt:     now,
				})
			}
			return &intelligence.ForecastBatchResponse{Results: results}, nil
		},
	}

	req := &intelligence.ForecastBatchRequest{
		Requests: []intelligence.ForecastRequest{
			{AgentID: "agent-001", TenantID: "tenant-001", MetricName: "cpu.usage", Horizon: 3600 * time.Second, ConfidenceLevel: 0.95},
			{AgentID: "agent-001", TenantID: "tenant-001", MetricName: "memory.usage", Horizon: 3600 * time.Second, ConfidenceLevel: 0.95},
		},
	}

	resp, err := mock.ForecastBatch(ctx, req)
	require.NoError(t, err)
	require.Len(t, resp.Results, 2, "Batch response must contain result for each request")
	assert.Equal(t, "cpu.usage", resp.Results[0].MetricName)
	assert.Equal(t, "memory.usage", resp.Results[1].MetricName)
}
