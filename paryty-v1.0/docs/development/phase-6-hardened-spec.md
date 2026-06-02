# Phase 6 Hardened Specification — Intelligence Layer

**Version:** 1.0.0
**Status:** LOCKED — All architectural decisions finalized
**Target LOC:** ~15,000 (Python + Go)
**Estimated Effort:** 5-7 weeks for a senior ML/platform engineer

---

## Table of Contents

1. [Phase 6 Overview & Decisions](#1-phase-6-overview--decisions)
2. [Pre-Phase Setup](#2-pre-phase-setup)
3. [Layer 21: Forecasting Engine](#3-layer-21-forecasting-engine)
4. [Layer 22: Anomaly Detection](#4-layer-22-anomaly-detection)
5. [Layer 23: Simulation Engine](#5-layer-23-simulation-engine)
6. [Layer 24: Timeline Engine](#6-layer-24-timeline-engine)
7. [Python Service Infrastructure](#7-python-service-infrastructure)
8. [Verification Gates](#8-verification-gates)
9. [Performance Targets](#9-performance-targets)
10. [Contingency & Rollback](#10-contingency--rollback)
11. [Appendices](#11-appendices)

---

## 1. Phase 6 Overview & Decisions

### 1.1 What Phase 6 Delivers

Phase 6 adds intelligence to Paryty. The system goes from "collecting and displaying data" to "understanding and predicting data." This includes forecasting future resource usage, detecting anomalies automatically, simulating infrastructure changes before deploying them, and replaying historical state with diff capabilities.

**Before Phase 6:** Reactive observability — you see what happened, but not what will happen or what's unusual.

**After Phase 6:** Predictive observability — forecasts for the next 7 days, automatic anomaly detection with explanations, what-if simulation for capacity planning, and full timeline replay with diff/export.

### 1.2 Architectural Decisions (LOCKED)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Python Service Architecture | **A — Standalone gRPC Microservice** | Separate Python binary, gRPC for inter-service communication. Clean separation, independent scaling. |
| 2 | ML Model Management | **B — File-Based Persistence** | Train models, save to disk (pickle/joblib). Fast startup, survives restarts, no external dependencies. |
| 3 | Forecasting Ensemble | **A — Weighted Average** | Weights based on recent MAPE. Simple, interpretable, self-correcting. |
| 4 | Simulation Engine | **B — What-If + Chaos Engineering** | What-if analysis + k6/LitmusChaos integration. **Designed with interfaces for V2.0 full digital twin (Option C).** |

### 1.3 V2.0 Migration Path (Decision 4)

> The simulation engine MUST be designed with abstraction layers that enable migration to a full digital twin (Option C) in V2.0. This means:
>
> - `SimulationEngine` interface that V2.0 can implement as `DigitalTwinEngine`
> - `WhatIfScenario` interface that V2.0 can extend to `LiveMirrorScenario`
> - `SimulationResult` interface that V2.0 can extend with real-world comparison data
> - All simulation state management through a `SimulationState` trait/interface
> - Traffic replay abstraction that V2.0 can implement with real traffic mirroring

### 1.4 What Gets Built

| Layer | Component | Language | LOC | Description |
|-------|-----------|----------|-----|-------------|
| 21 | Forecasting Engine | Python | ~4,000 | Linear regression, Prophet, XGBoost, weighted ensemble |
| 22 | Anomaly Detection | Python | ~5,000 | Z-Score, IQR, EWMA, Isolation Forest, Autoencoder |
| 23 | Simulation Engine | Go | ~4,000 | What-if analysis, chaos engineering integration, bot detection |
| 24 | Timeline Engine | Go | ~2,000 | Snapshot manager, replay engine, diff calculator, export |

### 1.5 Existing Code Assessment

**What exists:**
- Go cluster services with Redpanda streaming, 3-tier storage
- Timeline snapshots and event log (Phase 4)
- Proto definitions for forecasting/anomaly detection (partial)
- No Python code in the repo (new language addition)
- No ML/AI capabilities

**What's new:**
- Python gRPC service (forecasting + anomaly detection)
- Go simulation engine (what-if + chaos engineering)
- Go timeline engine (replay + diff + export)
- New Redpanda topics for intelligence outputs
- New gRPC proto definitions for Python ↔ Go communication

---

## 2. Pre-Phase Setup

### 2.1 New Directory Structure

```
paryty-v1.0/
├── intelligence/                    # NEW — Python ML service
│   ├── pyproject.toml              # Python project config (pip/poetry)
│   ├── requirements.txt            # Pinned dependencies
│   ├── Dockerfile                  # Python service container
│   ├── proto/                      # Generated Python gRPC stubs
│   │   ├── forecasting_pb2.py
│   │   ├── forecasting_pb2_grpc.py
│   │   ├── anomaly_pb2.py
│   │   └── anomaly_pb2_grpc.py
│   ├── forecasting/                # Forecasting engine
│   │   ├── __init__.py
│   │   ├── linear.py               # Linear regression layer
│   │   ├── prophet.py              # Prophet integration
│   │   ├── xgboost_model.py        # XGBoost integration
│   │   ├── ensemble.py             # Weighted ensemble
│   │   ├── feature_engineering.py  # Feature extraction
│   │   ├── model_store.py          # File-based model persistence
│   │   └── config.py               # Forecasting configuration
│   ├── anomaly/                    # Anomaly detection engine
│   │   ├── __init__.py
│   │   ├── statistical.py          # Z-Score, IQR, EWMA
│   │   ├── isolation_forest.py     # Isolation Forest
│   │   ├── autoencoder.py          # TensorFlow autoencoder
│   │   ├── ensemble.py             # Voting ensemble
│   │   ├── explainer.py            # Anomaly explanation generator
│   │   ├── model_store.py          # File-based model persistence
│   │   └── config.py               # Anomaly detection configuration
│   ├── data/                       # Data access layer
│   │   ├── __init__.py
│   │   ├── questdb_client.py       # QuestDB query client (read metrics)
│   │   ├── dragonfly_client.py     # Dragonfly query client (read hot data)
│   │   └── timeseries.py           # Time-series data utilities
│   ├── server/                     # gRPC server
│   │   ├── __init__.py
│   │   ├── forecasting_service.py  # Forecasting gRPC service
│   │   ├── anomaly_service.py      # Anomaly detection gRPC service
│   │   └── main.py                 # Server entry point
│   ├── tests/                      # Python tests
│   │   ├── test_linear.py
│   │   ├── test_prophet.py
│   │   ├── test_xgboost.py
│   │   ├── test_ensemble.py
│   │   ├── test_statistical.py
│   │   ├── test_isolation_forest.py
│   │   ├── test_autoencoder.py
│   │   └── test_grpc_services.py
│   └── models/                     # Persisted model files (gitignored)
│       ├── forecasting/
│       └── anomaly/
├── cluster/internal/
│   ├── simulation/                 # NEW — Simulation engine
│   │   ├── engine.go               # SimulationEngine interface + impl
│   │   ├── whatif.go               # What-if scenario runner
│   │   ├── chaos.go                # Chaos engineering integration
│   │   ├── bot_detection.go        # Bot pattern analyzer
│   │   ├── degradation.go          # Degradation simulator
│   │   ├── capacity.go             # Capacity planning
│   │   └── interfaces.go           # V2.0 migration interfaces
│   ├── timeline/                   # NEW — Timeline engine
│   │   ├── snapshot_manager.go     # Snapshot management
│   │   ├── replay_engine.go        # Replay engine
│   │   ├── diff_calculator.go      # Diff calculator
│   │   └── export_manager.go       # Export manager
│   ├── intelligence/               # NEW — Intelligence client
│   │   ├── forecasting_client.go   # gRPC client for forecasting
│   │   ├── anomaly_client.go       # gRPC client for anomaly detection
│   │   └── cache.go                # Forecast/anomaly cache
│   ├── stream/
│   │   └── topics.go               # UPDATE — New topics
│   └── api/
│       ├── handler/                # UPDATE — New API handlers
│       │   ├── forecast.go
│       │   ├── anomaly.go
│       │   ├── simulation.go
│       │   └── timeline.go
│       └── router/
│           └── router.go           # UPDATE — New routes
├── proto/paryty/v1/
│   ├── forecasting.proto           # NEW — Forecasting service definition
│   ├── anomaly.proto               # NEW — Anomaly detection service definition
│   ├── simulation.proto            # NEW — Simulation service definition
│   └── timeline.proto              # NEW — Timeline service definition
└── configs/
    └── intelligence/
        └── intelligence.yaml       # NEW — Python service config
```

### 2.2 New Proto Definitions

**File:** `proto/paryty/v1/forecasting.proto`

```protobuf
syntax = "proto3";
package paryty.v1;

option go_package = "github.com/paryty/paryty-v1.0/cluster/internal/proto";

service ForecastingService {
  // Forecast a single metric for a given agent
  rpc ForecastMetric(ForecastMetricRequest) returns (ForecastMetricResponse);

  // Forecast multiple metrics in batch
  rpc ForecastBatch(ForecastBatchRequest) returns (ForecastBatchResponse);

  // Get model accuracy metrics
  rpc GetModelAccuracy(GetModelAccuracyRequest) returns (GetModelAccuracyResponse);

  // Trigger model retraining
  rpc RetrainModels(RetrainModelsRequest) returns (RetrainModelsResponse);
}

message ForecastMetricRequest {
  string agent_id = 1;
  string metric_name = 2;         // e.g., "cpu_usage", "memory_usage"
  int64 horizon_seconds = 3;      // How far ahead to forecast (default: 3600)
  int32 confidence_level = 4;     // 95 or 99 (default: 95)
}

message ForecastMetricResponse {
  string agent_id = 1;
  string metric_name = 2;
  repeated ForecastPoint forecast = 3;
  ModelInfo model_info = 4;
  float overall_confidence = 5;   // 0.0 - 1.0
}

message ForecastPoint {
  int64 timestamp = 1;            // Unix timestamp
  float value = 2;                // Forecasted value
  float lower_bound = 3;          // Confidence interval lower
  float upper_bound = 4;          // Confidence interval upper
}

message ModelInfo {
  string best_model = 1;          // "linear", "prophet", "xgboost", "ensemble"
  map<string, float> weights = 2; // Model weights in ensemble
  map<string, float> accuracy = 3; // Per-model MAPE
  int64 last_trained = 4;         // Unix timestamp of last training
}

message ForecastBatchRequest {
  repeated ForecastMetricRequest requests = 1;
}

message ForecastBatchResponse {
  repeated ForecastMetricResponse forecasts = 1;
}

message GetModelAccuracyRequest {
  string metric_name = 1;         // Empty = all metrics
}

message GetModelAccuracyResponse {
  map<string, ModelInfo> models = 1; // metric_name → model info
}

message RetrainModelsRequest {
  string metric_name = 1;         // Empty = all metrics
  bool force = 2;                 // Force retrain even if recent
}

message RetrainModelsResponse {
  bool success = 1;
  string message = 2;
  map<string, ModelInfo> updated_models = 3;
}
```

**File:** `proto/paryty/v1/anomaly.proto`

```protobuf
syntax = "proto3";
package paryty.v1;

option go_package = "github.com/paryty/paryty-v1.0/cluster/internal/proto";

service AnomalyDetectionService {
  // Detect anomalies in a single metric
  rpc DetectAnomalies(DetectAnomaliesRequest) returns (DetectAnomaliesResponse);

  // Detect anomalies across multiple metrics (cross-metric)
  rpc DetectCrossMetricAnomalies(DetectCrossMetricRequest) returns (DetectCrossMetricResponse);

  // Get anomaly explanation
  rpc ExplainAnomaly(ExplainAnomalyRequest) returns (ExplainAnomalyResponse);

  // Get detection model status
  rpc GetDetectionStatus(GetDetectionStatusRequest) returns (GetDetectionStatusResponse);
}

message DetectAnomaliesRequest {
  string agent_id = 1;
  string metric_name = 2;
  repeated float values = 3;      // Recent values (last N data points)
  repeated int64 timestamps = 4;  // Corresponding timestamps
  float sensitivity = 5;          // 0.0 (less sensitive) to 1.0 (more sensitive)
}

message DetectAnomaliesResponse {
  string agent_id = 1;
  string metric_name = 2;
  repeated Anomaly anomalies = 3;
  float overall_score = 4;        // 0.0 (normal) to 1.0 (highly anomalous)
}

message Anomaly {
  int64 timestamp = 1;
  float value = 2;
  float score = 3;                // Anomaly score 0.0 - 1.0
  AnomalyType type = 4;
  string explanation = 5;         // Human-readable explanation
  repeated string contributing_factors = 6;
}

enum AnomalyType {
  ANOMALY_TYPE_UNSPECIFIED = 0;
  ANOMALY_TYPE_POINT = 1;         // Single value anomaly
  ANOMALY_TYPE_CONTEXTUAL = 2;    // Unusual given context
  ANOMALY_TYPE_COLLECTIVE = 3;    // Pattern anomaly (multiple points)
  ANOMALY_TYPE_TREND = 4;         // Trend change
}

message DetectCrossMetricRequest {
  string agent_id = 1;
  map<string, repeated float> metrics = 2;  // metric_name → values
  repeated int64 timestamps = 3;
}

message DetectCrossMetricResponse {
  repeated Anomaly anomalies = 1;
  repeated CorrelationAnomaly correlation_anomalies = 2;
}

message CorrelationAnomaly {
  string metric_a = 1;
  string metric_b = 2;
  string description = 3;         // e.g., "CPU spike without memory increase"
  float severity = 4;
}

message ExplainAnomalyRequest {
  string agent_id = 1;
  string metric_name = 2;
  int64 timestamp = 3;
}

message ExplainAnomalyResponse {
  Anomaly anomaly = 1;
  repeated string similar_incidents = 2;  // Past similar anomalies
  repeated string recommendations = 3;    // Suggested actions
}

message GetDetectionStatusRequest {}

message GetDetectionStatusResponse {
  map<string, ModelStatus> models = 1;
  int64 last_training = 2;
  int32 anomalies_detected_24h = 3;
  float false_positive_rate = 4;
}

message ModelStatus {
  string name = 1;
  bool trained = 2;
  float accuracy = 3;
  int64 last_updated = 4;
}
```

### 2.3 Python Dependencies

**File:** `intelligence/requirements.txt`

```
# gRPC
grpcio>=1.60.0
grpcio-tools>=1.60.0
protobuf>=4.25.0

# ML / Forecasting
scikit-learn>=1.3.0
prophet>=1.1.5
xgboost>=2.0.0
numpy>=1.26.0
pandas>=2.1.0
statsmodels>=0.14.0

# Anomaly Detection
tensorflow>=2.15.0

# Data Access
psycopg2-binary>=2.9.9    # QuestDB (PostgreSQL wire protocol)
redis>=5.0.0               # Dragonfly (Redis-compatible)

# Serialization
joblib>=1.3.0              # Model persistence
pickle5>=0.0.11            # Enhanced pickle

# Testing
pytest>=7.4.0
pytest-asyncio>=0.21.0
grpcio-testing>=1.60.0

# Utilities
python-dotenv>=1.0.0
pydantic>=2.5.0
structlog>=23.2.0
```

---

## 3. Layer 21: Forecasting Engine

### 3.1 Overview

The forecasting engine predicts future metric values (CPU, memory, disk, network) for the next 1 hour to 7 days. It uses three models (Linear Regression, Prophet, XGBoost) combined via a weighted ensemble, where weights adapt based on recent prediction accuracy.

### 3.2 Linear Regression Layer

**File:** `intelligence/forecasting/linear.py` (~400 LOC)

```python
"""
Linear Regression forecasting layer.

Provides fast (<1ms) baseline forecasts using rolling window linear regression.
Best for: short-term trends (1-60 minutes), simple monotonic patterns.

NOT good for: seasonal patterns, complex non-linear behavior.
"""

import numpy as np
from sklearn.linear_model import LinearRegression
from dataclasses import dataclass
from typing import Optional


@dataclass
class ForecastResult:
    """Single forecast output."""
    timestamps: list[int]       # Future timestamps
    values: list[float]         # Forecasted values
    lower_bound: list[float]    # Confidence interval lower
    upper_bound: list[float]    # Confidence interval upper
    confidence: float           # Overall confidence 0.0 - 1.0
    model_name: str = "linear"


class LinearForecaster:
    """
    Rolling window linear regression forecaster.

    ALGORITHM:
    1. Take last N data points (window_size, default 60)
    2. Fit linear regression: value = a * time + b
    3. Extrapolate to horizon
    4. Compute prediction intervals using residual standard error

    PERFORMANCE: <1ms per forecast (single metric)
    """

    def __init__(self, window_size: int = 60, confidence_level: float = 0.95):
        self.window_size = window_size
        self.confidence_level = confidence_level
        self._model: Optional[LinearRegression] = None
        self._residual_std: float = 0.0

    def fit(self, timestamps: list[int], values: list[float]) -> None:
        """
        Fit the model on recent data.

        Args:
            timestamps: Unix timestamps (seconds)
            values: Metric values

        Raises:
            ValueError: If fewer than 3 data points
        """
        if len(values) < 3:
            raise ValueError("Need at least 3 data points for linear regression")

        # Use last window_size points
        n = min(len(values), self.window_size)
        t = np.array(timestamps[-n:], dtype=np.float64)
        y = np.array(values[-n:], dtype=np.float64)

        # Normalize time to seconds from start
        t_norm = (t - t[0]).reshape(-1, 1)

        self._model = LinearRegression()
        self._model.fit(t_norm, y)

        # Compute residual standard error for prediction intervals
        predictions = self._model.predict(t_norm)
        residuals = y - predictions
        self._residual_std = float(np.std(residuals))

    def predict(self, horizon_seconds: int, step_seconds: int = 60) -> ForecastResult:
        """
        Generate forecast for the given horizon.

        Args:
            horizon_seconds: How far ahead to forecast
            step_seconds: Time step between forecast points

        Returns:
            ForecastResult with timestamps, values, and confidence intervals
        """
        if self._model is None:
            raise RuntimeError("Model not fitted. Call fit() first.")

        # Generate future timestamps
        import time
        now = int(time.time())
        n_steps = horizon_seconds // step_seconds
        future_timestamps = [now + i * step_seconds for i in range(n_steps)]

        # Predict
        # We need to continue the time normalization from fit
        t_future = np.array(range(1, n_steps + 1), dtype=np.float64).reshape(-1, 1)
        t_future = t_future * step_seconds  # Scale to actual seconds

        values = self._model.predict(t_future)

        # Confidence intervals (z-score for 95% = 1.96, 99% = 2.576)
        z_score = 1.96 if self.confidence_level == 0.95 else 2.576
        # Widen intervals further from training data
        distances = np.array(range(1, n_steps + 1), dtype=np.float64)
        widening = 1.0 + distances / n_steps  # Linear widening
        margin = z_score * self._residual_std * widening

        lower = values - margin
        upper = values + margin

        # Confidence decreases with distance
        base_confidence = max(0.3, 1.0 - self._residual_std / (np.mean(np.abs(values)) + 1e-6))
        confidences = base_confidence * (1.0 - distances / (n_steps * 2))
        overall_confidence = float(np.clip(np.mean(confidences), 0.1, 0.95))

        return ForecastResult(
            timestamps=future_timestamps,
            values=values.tolist(),
            lower_bound=lower.tolist(),
            upper_bound=upper.tolist(),
            confidence=overall_confidence,
        )
```

### 3.3 Prophet Integration

**File:** `intelligence/forecasting/prophet.py` (~500 LOC)

```python
"""
Prophet forecasting layer.

Provides seasonal pattern detection and forecasting.
Best for: daily/weekly cycles, holiday effects, trend changes.

NOT good for: real-time (<100ms) predictions (takes 1-5 seconds to fit).
"""

import pandas as pd
from prophet import Prophet
from dataclasses import dataclass
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class ProphetForecaster:
    """
    Prophet-based forecaster for seasonal patterns.

    ALGORITHM:
    1. Convert metric data to Prophet DataFrame
    2. Fit Prophet model (detects daily/weekly seasonality automatically)
    3. Predict future values with uncertainty intervals
    4. Detect changepoints (sudden trend changes)

    PERFORMANCE: 1-5 seconds per fit, <100ms per predict
    TRAINING: Should be retrained daily on 7 days of data
    """

    def __init__(
        self,
        daily_seasonality: bool = True,
        weekly_seasonality: bool = True,
        yearly_seasonality: bool = False,
        changepoint_prior_scale: float = 0.05,
        confidence_level: float = 0.95,
    ):
        self.daily_seasonality = daily_seasonality
        self.weekly_seasonality = weekly_seasonality
        self.yearly_seasonality = yearly_seasonality
        self.changepoint_prior_scale = changepoint_prior_scale
        self.confidence_level = confidence_level
        self._model: Optional[Prophet] = None
        self._last_fit_size: int = 0

    def fit(self, timestamps: list[int], values: list[float]) -> None:
        """
        Fit Prophet model on historical data.

        Args:
            timestamps: Unix timestamps (seconds)
            values: Metric values

        Raises:
            ValueError: If fewer than 1440 data points (1 day at 1-min intervals)
        """
        if len(values) < 1440:
            raise ValueError(
                f"Prophet needs at least 1 day of data (1440 points), got {len(values)}"
            )

        # Convert to Prophet DataFrame
        df = pd.DataFrame({
            'ds': pd.to_datetime(timestamps, unit='s'),
            'y': values,
        })

        # Remove duplicates and sort
        df = df.drop_duplicates(subset='ds').sort_values('ds').reset_index(drop=True)

        # Create and fit model
        self._model = Prophet(
            daily_seasonality=self.daily_seasonality,
            weekly_seasonality=self.weekly_seasonality,
            yearly_seasonality=self.yearly_seasonality,
            changepoint_prior_scale=self.changepoint_prior_scale,
            interval_width=self.confidence_level,
        )

        # Suppress Prophet's verbose logging
        import logging
        prophet_logger = logging.getLogger('prophet')
        old_level = prophet_logger.level
        prophet_logger.setLevel(logging.WARNING)

        try:
            self._model.fit(df)
            self._last_fit_size = len(df)
        finally:
            prophet_logger.setLevel(old_level)

        logger.info(f"Prophet model fitted on {len(df)} data points")

    def predict(self, horizon_seconds: int, step_seconds: int = 60) -> 'ForecastResult':
        """
        Generate forecast for the given horizon.

        Args:
            horizon_seconds: How far ahead to forecast
            step_seconds: Time step between forecast points

        Returns:
            ForecastResult with timestamps, values, and confidence intervals
        """
        if self._model is None:
            raise RuntimeError("Model not fitted. Call fit() first.")

        import time
        now = int(time.time())
        n_steps = horizon_seconds // step_seconds

        # Create future DataFrame
        future_dates = pd.date_range(
            start=pd.Timestamp(now, unit='s'),
            periods=n_steps,
            freq=f'{step_seconds}s',
        )
        future_df = pd.DataFrame({'ds': future_dates})

        # Predict
        forecast = self._model.predict(future_df)

        # Extract results
        timestamps = [int(d.timestamp()) for d in forecast['ds']]
        values = forecast['yhat'].tolist()
        lower = forecast['yhat_lower'].tolist()
        upper = forecast['yhat_upper'].tolist()

        # Detect changepoints
        changepoints = self._model.changepoints
        if len(changepoints) > 0:
            logger.info(f"Detected {len(changepoints)} changepoints")

        # Confidence based on uncertainty width
        avg_uncertainty = float(((forecast['yhat_upper'] - forecast['yhat_lower']) /
                                 (forecast['yhat'].abs() + 1e-6)).mean())
        confidence = max(0.3, min(0.95, 1.0 - avg_uncertainty))

        from .linear import ForecastResult
        return ForecastResult(
            timestamps=timestamps,
            values=values,
            lower_bound=lower,
            upper_bound=upper,
            confidence=confidence,
            model_name="prophet",
        )

    def get_seasonality_components(self) -> dict:
        """Return detected seasonality components (daily, weekly patterns)."""
        if self._model is None:
            return {}

        return {
            'daily': self._model.daily_seasonality,
            'weekly': self._model.weekly_seasonality,
            'yearly': self._model.yearly_seasonality,
            'changepoints': len(self._model.changepoints) if self._model.changepoints is not None else 0,
        }
```

### 3.4 XGBoost Integration

**File:** `intelligence/forecasting/xgboost_model.py` (~500 LOC)

```python
"""
XGBoost forecasting layer.

Provides multi-variable pattern recognition for complex metric interactions.
Best for: complex non-linear patterns, multi-metric forecasting (CPU + memory + disk).

NOT good for: simple trends (overkill), very small datasets (<1000 points).
"""

import numpy as np
import xgboost as xgb
from sklearn.model_selection import TimeSeriesSplit
from sklearn.metrics import mean_absolute_percentage_error
from dataclasses import dataclass
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class XGBoostForecaster:
    """
    XGBoost-based forecaster with feature engineering.

    ALGORITHM:
    1. Engineer features (lag values, rolling stats, time features)
    2. Train XGBoost with time-series cross-validation
    3. Predict future values using recursive forecasting
    4. Estimate uncertainty via quantile regression

    PERFORMANCE: 5-10 seconds per fit, <10ms per predict
    TRAINING: Should be retrained daily on 7 days of data
    """

    def __init__(
        self,
        n_estimators: int = 100,
        max_depth: int = 6,
        learning_rate: float = 0.1,
        lag_features: list[int] = None,
        rolling_windows: list[int] = None,
    ):
        self.n_estimators = n_estimators
        self.max_depth = max_depth
        self.learning_rate = learning_rate
        self.lag_features = lag_features or [1, 5, 15, 60]
        self.rolling_windows = rolling_windows or [5, 15, 60]
        self._model: Optional[xgb.XGBRegressor] = None
        self._feature_names: list[str] = []
        self._last_values: list[float] = []

    def _engineer_features(
        self,
        values: np.ndarray,
        timestamps: np.ndarray,
    ) -> tuple[np.ndarray, list[str]]:
        """
        Engineer features for XGBoost.

        Features:
        - Lag values: value at t-1, t-5, t-15, t-60
        - Rolling statistics: mean, std, min, max over windows [5, 15, 60]
        - Time features: hour of day, day of week, minute of hour
        - Trend: slope of last N values
        """
        n = len(values)
        features = []
        names = []

        # Lag features
        for lag in self.lag_features:
            lag_arr = np.roll(values, lag)
            lag_arr[:lag] = values[0]  # Fill with first value
            features.append(lag_arr)
            names.append(f'lag_{lag}')

        # Rolling statistics
        for window in self.rolling_windows:
            for func, name in [
                (np.mean, 'mean'),
                (np.std, 'std'),
                (np.min, 'min'),
                (np.max, 'max'),
            ]:
                rolling = np.array([
                    func(values[max(0, i - window):i + 1])
                    for i in range(n)
                ])
                features.append(rolling)
                names.append(f'rolling_{window}_{name}')

        # Time features
        dt = pd.to_datetime(timestamps, unit='s')
        features.append(dt.hour.values / 23.0)        # Hour of day (normalized)
        names.append('hour_of_day')
        features.append(dt.dayofweek.values / 6.0)     # Day of week (normalized)
        names.append('day_of_week')
        features.append(dt.minute.values / 59.0)       # Minute of hour (normalized)
        names.append('minute_of_hour')

        # Trend (slope of last 10 values)
        trend = np.zeros(n)
        for i in range(10, n):
            x = np.arange(10)
            y = values[i - 10:i]
            slope = np.polyfit(x, y, 1)[0]
            trend[i] = slope
        features.append(trend)
        names.append('trend')

        X = np.column_stack(features)
        return X, names

    def fit(self, timestamps: list[int], values: list[float]) -> None:
        """
        Train XGBoost model with time-series cross-validation.

        Args:
            timestamps: Unix timestamps
            values: Metric values

        Raises:
            ValueError: If fewer than 1000 data points
        """
        if len(values) < 1000:
            raise ValueError(f"XGBoost needs at least 1000 points, got {len(values)}")

        values_arr = np.array(values, dtype=np.float64)
        timestamps_arr = np.array(timestamps, dtype=np.int64)

        # Engineer features
        X, names = self._engineer_features(values_arr, timestamps_arr)
        self._feature_names = names

        # Use last 10% as validation
        split_idx = int(len(X) * 0.9)
        X_train, X_val = X[:split_idx], X[split_idx:]
        y_train, y_val = values_arr[:split_idx], values_arr[split_idx:]

        # Train model
        self._model = xgb.XGBRegressor(
            n_estimators=self.n_estimators,
            max_depth=self.max_depth,
            learning_rate=self.learning_rate,
            objective='reg:squarederror',
            n_jobs=-1,
            verbosity=0,
        )

        self._model.fit(
            X_train, y_train,
            eval_set=[(X_val, y_val)],
            verbose=False,
        )

        # Store last values for recursive forecasting
        self._last_values = values[-max(self.lag_features):]

        # Log accuracy
        val_pred = self._model.predict(X_val)
        mape = mean_absolute_percentage_error(y_val, val_pred) * 100
        logger.info(f"XGBoost trained. Validation MAPE: {mape:.2f}%")

    def predict(self, horizon_seconds: int, step_seconds: int = 60) -> 'ForecastResult':
        """Generate forecast using recursive prediction."""
        if self._model is None:
            raise RuntimeError("Model not fitted. Call fit() first.")

        import time
        now = int(time.time())
        n_steps = horizon_seconds // step_seconds

        # Recursive forecasting: predict one step at a time
        predicted_values = []
        current_values = list(self._last_values)

        for i in range(n_steps):
            ts = now + i * step_seconds
            # Engineer features for current state
            arr = np.array(current_values[-max(self.rolling_windows):])
            # Simplified feature extraction for prediction
            features = self._extract_predict_features(arr, ts)
            pred = self._model.predict(features.reshape(1, -1))[0]
            predicted_values.append(float(pred))
            current_values.append(pred)

        # Uncertainty via prediction interval widening
        timestamps = [now + i * step_seconds for i in range(n_steps)]
        std = np.std(predicted_values) * 0.1  # 10% of std as base uncertainty
        z = 1.96
        distances = np.arange(1, n_steps + 1, dtype=np.float64)
        margin = z * std * (1.0 + distances / n_steps)

        values = np.array(predicted_values)
        lower = values - margin
        upper = values + margin

        from .linear import ForecastResult
        return ForecastResult(
            timestamps=timestamps,
            values=values.tolist(),
            lower_bound=lower.tolist(),
            upper_bound=upper.tolist(),
            confidence=0.7,  # XGBoost is less interpretable
            model_name="xgboost",
        )

    def _extract_predict_features(self, values: np.ndarray, timestamp: int) -> np.ndarray:
        """Extract features for a single prediction step."""
        features = []
        for lag in self.lag_features:
            idx = max(0, len(values) - lag)
            features.append(values[idx])
        for window in self.rolling_windows:
            w = values[-window:]
            features.extend([np.mean(w), np.std(w), np.min(w), np.max(w)])
        import pandas as pd
        dt = pd.Timestamp(timestamp, unit='s')
        features.extend([dt.hour / 23.0, dt.dayofweek / 6.0, dt.minute / 59.0])
        if len(values) >= 10:
            x = np.arange(10)
            y = values[-10:]
            features.append(np.polyfit(x, y, 1)[0])
        else:
            features.append(0.0)
        return np.array(features)
```

### 3.5 Weighted Ensemble

**File:** `intelligence/forecasting/ensemble.py` (~400 LOC)

```python
"""
Weighted ensemble for combining forecasts from multiple models.

Weights are based on recent prediction accuracy (MAPE).
The ensemble self-corrects: models that perform well get higher weights.
"""

import numpy as np
from typing import Optional
from .linear import LinearForecaster, ForecastResult
from .prophet import ProphetForecaster
from .xgboost_model import XGBoostForecaster
import logging

logger = logging.getLogger(__name__)


class ForecastEnsemble:
    """
    Weighted ensemble combining Linear, Prophet, and XGBoost forecasts.

    WEIGHT UPDATE ALGORITHM:
    1. Track MAPE (Mean Absolute Percentage Error) for each model
    2. Update weights every hour based on recent accuracy
    3. Weight = (1/MAPE) / sum(1/MAPE_i) — inverse error weighting
    4. Minimum weight: 0.1 (no model can be completely excluded)
    5. Maximum weight: 0.6 (no single model can dominate)

    FALLBACK STRATEGY:
    - If Prophet fails (insufficient data): redistribute weight to Linear + XGBoost
    - If XGBoost fails (insufficient data): redistribute weight to Linear + Prophet
    - If only Linear works: use Linear alone with reduced confidence
    """

    def __init__(self, confidence_level: float = 0.95):
        self.confidence_level = confidence_level
        self.linear = LinearForecaster(confidence_level=confidence_level)
        self.prophet = ProphetForecaster(confidence_level=confidence_level)
        self.xgboost = XGBoostForecaster()

        # Initial weights (equal)
        self.weights = {
            'linear': 0.34,
            'prophet': 0.33,
            'xgboost': 0.33,
        }

        # Accuracy tracking
        self._accuracy_history: dict[str, list[float]] = {
            'linear': [],
            'prophet': [],
            'xgboost': [],
        }
        self._max_history = 168  # Keep 7 days of hourly accuracy (168 entries)

    def fit_all(self, timestamps: list[int], values: list[float]) -> dict[str, bool]:
        """
        Train all models. Returns which models succeeded.

        Args:
            timestamps: Unix timestamps
            values: Metric values

        Returns:
            Dict of model_name → success
        """
        results = {}

        # Linear (always works with 3+ points)
        try:
            self.linear.fit(timestamps, values)
            results['linear'] = True
        except Exception as e:
            logger.warning(f"Linear fit failed: {e}")
            results['linear'] = False

        # Prophet (needs 1 day of data)
        try:
            self.prophet.fit(timestamps, values)
            results['prophet'] = True
        except Exception as e:
            logger.warning(f"Prophet fit failed: {e}")
            results['prophet'] = False

        # XGBoost (needs 1000 points)
        try:
            self.xgboost.fit(timestamps, values)
            results['xgboost'] = True
        except Exception as e:
            logger.warning(f"XGBoost fit failed: {e}")
            results['xgboost'] = False

        # Redistribute weights for failed models
        self._redistribute_weights(results)

        logger.info(f"Ensemble fit complete. Weights: {self.weights}")
        return results

    def predict(self, horizon_seconds: int, step_seconds: int = 60) -> ForecastResult:
        """
        Generate ensemble forecast.

        Combines predictions from all available models using weighted average.
        Confidence intervals are the union of individual intervals.
        """
        predictions = {}

        # Collect predictions from available models
        try:
            predictions['linear'] = self.linear.predict(horizon_seconds, step_seconds)
        except Exception as e:
            logger.warning(f"Linear predict failed: {e}")

        try:
            predictions['prophet'] = self.prophet.predict(horizon_seconds, step_seconds)
        except Exception as e:
            logger.warning(f"Prophet predict failed: {e}")

        try:
            predictions['xgboost'] = self.xgboost.predict(horizon_seconds, step_seconds)
        except Exception as e:
            logger.warning(f"XGBoost predict failed: {e}")

        if not predictions:
            raise RuntimeError("All models failed to predict")

        # Weighted average
        n_points = len(next(iter(predictions.values())).values)
        ensemble_values = np.zeros(n_points)
        ensemble_lower = np.zeros(n_points)
        ensemble_upper = np.zeros(n_points)
        total_weight = 0.0

        for name, result in predictions.items():
            w = self.weights.get(name, 0.0)
            if w <= 0:
                continue
            ensemble_values += w * np.array(result.values)
            ensemble_lower += w * np.array(result.lower_bound)
            ensemble_upper += w * np.array(result.upper_bound)
            total_weight += w

        if total_weight > 0:
            ensemble_values /= total_weight
            ensemble_lower /= total_weight
            ensemble_upper /= total_weight

        # Ensemble confidence: weighted average of individual confidences
        ensemble_confidence = sum(
            self.weights.get(name, 0) * result.confidence
            for name, result in predictions.items()
        ) / total_weight

        # Use timestamps from best available model
        best_model = max(predictions.keys(), key=lambda k: self.weights.get(k, 0))

        return ForecastResult(
            timestamps=predictions[best_model].timestamps,
            values=ensemble_values.tolist(),
            lower_bound=ensemble_lower.tolist(),
            upper_bound=ensemble_upper.tolist(),
            confidence=float(ensemble_confidence),
            model_name="ensemble",
        )

    def update_weights(self, actual_values: list[float], predicted_values: dict[str, list[float]]) -> None:
        """
        Update model weights based on prediction accuracy.

        Call this periodically (every hour) with actual values and each
        model's predictions from the previous hour.

        Args:
            actual_values: Actual observed values
            predicted_values: Dict of model_name → predicted values
        """
        for name, preds in predicted_values.items():
            if name not in self._accuracy_history:
                continue

            # Compute MAPE
            actual = np.array(actual_values[:len(preds)])
            pred = np.array(preds[:len(actual)])
            if len(actual) == 0 or np.any(actual == 0):
                continue

            mape = np.mean(np.abs((actual - pred) / actual)) * 100
            self._accuracy_history[name].append(mape)

            # Keep bounded
            if len(self._accuracy_history[name]) > self._max_history:
                self._accuracy_history[name] = self._accuracy_history[name][-self._max_history:]

        # Recompute weights from inverse MAPE
        avg_mapes = {}
        for name, history in self._accuracy_history.items():
            if history:
                avg_mapes[name] = np.mean(history[-24:])  # Last 24 hours

        if not avg_mapes:
            return

        # Inverse MAPE weighting with bounds
        inv_mapes = {name: 1.0 / (mape + 1e-6) for name, mape in avg_mapes.items()}
        total = sum(inv_mapes.values())

        for name in self.weights:
            if name in inv_mapes:
                raw_weight = inv_mapes[name] / total
                # Clamp to [0.1, 0.6]
                self.weights[name] = max(0.1, min(0.6, raw_weight))

        # Normalize
        total = sum(self.weights.values())
        self.weights = {k: v / total for k, v in self.weights.items()}

        logger.info(f"Updated ensemble weights: {self.weights}")

    def get_accuracy(self) -> dict[str, dict]:
        """Return accuracy metrics for all models."""
        result = {}
        for name, history in self._accuracy_history.items():
            if history:
                result[name] = {
                    'current_mape': history[-1],
                    'avg_mape_24h': float(np.mean(history[-24:])),
                    'avg_mape_7d': float(np.mean(history[-168:])),
                    'weight': self.weights.get(name, 0.0),
                    'samples': len(history),
                }
        return result

    def _redistribute_weights(self, fit_results: dict[str, bool]) -> None:
        """Redistribute weights for models that failed to fit."""
        available = [name for name, success in fit_results.items() if success]
        if not available:
            return

        # Set failed models to 0
        for name, success in fit_results.items():
            if not success:
                self.weights[name] = 0.0

        # Redistribute proportionally
        total = sum(self.weights[name] for name in available)
        if total > 0:
            for name in available:
                self.weights[name] /= total
```

### 3.6 Model Persistence

**File:** `intelligence/forecasting/model_store.py` (~200 LOC)

```python
"""
File-based model persistence for forecasting models.

Saves trained models to disk using joblib/pickle.
Loads models on startup for instant availability.
"""

import joblib
import os
import time
from pathlib import Path
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class ModelStore:
    """
    Persists trained models to disk and loads them on startup.

    DIRECTORY STRUCTURE:
        models/forecasting/
        ├── linear_cpu_usage.joblib
        ├── linear_memory_usage.joblib
        ├── prophet_cpu_usage.joblib
        ├── xgboost_cpu_usage.joblib
        ├── ensemble_cpu_usage.joblib
        └── metadata.json  (training timestamps, accuracy)
    """

    def __init__(self, base_path: str = "models/forecasting"):
        self.base_path = Path(base_path)
        self.base_path.mkdir(parents=True, exist_ok=True)

    def save(self, model_name: str, metric_name: str, model: object) -> str:
        """Save a model to disk."""
        filename = f"{model_name}_{metric_name}.joblib"
        filepath = self.base_path / filename
        joblib.dump(model, filepath)
        logger.info(f"Saved model {model_name} for {metric_name} to {filepath}")
        return str(filepath)

    def load(self, model_name: str, metric_name: str) -> Optional[object]:
        """Load a model from disk. Returns None if not found."""
        filename = f"{model_name}_{metric_name}.joblib"
        filepath = self.base_path / filename
        if not filepath.exists():
            return None
        try:
            model = joblib.load(filepath)
            logger.info(f"Loaded model {model_name} for {metric_name} from {filepath}")
            return model
        except Exception as e:
            logger.warning(f"Failed to load model {model_name} for {metric_name}: {e}")
            return None

    def exists(self, model_name: str, metric_name: str) -> bool:
        """Check if a model exists on disk."""
        filename = f"{model_name}_{metric_name}.joblib"
        return (self.base_path / filename).exists()

    def get_training_age(self, model_name: str, metric_name: str) -> Optional[float]:
        """Get age of model in seconds since last save."""
        filename = f"{model_name}_{metric_name}.joblib"
        filepath = self.base_path / filename
        if not filepath.exists():
            return None
        mtime = filepath.stat().st_mtime
        return time.time() - mtime

    def list_models(self) -> list[dict]:
        """List all persisted models with metadata."""
        models = []
        for f in self.base_path.glob("*.joblib"):
            parts = f.stem.split('_', 1)
            if len(parts) == 2:
                models.append({
                    'model_name': parts[0],
                    'metric_name': parts[1],
                    'age_seconds': time.time() - f.stat().st_mtime,
                    'size_bytes': f.stat().st_size,
                })
        return models
```

---

## 4. Layer 22: Anomaly Detection

### 4.1 Overview

The anomaly detection engine identifies unusual patterns in metrics automatically. It uses three detection methods (Statistical, Isolation Forest, Autoencoder) combined via a voting ensemble. Each anomaly comes with a human-readable explanation.

### 4.2 Statistical Methods

**File:** `intelligence/anomaly/statistical.py` (~500 LOC)

```python
"""
Statistical anomaly detection methods.

Fast (<1ms per metric) methods for real-time anomaly detection.
Catches: point anomalies, trend changes, moving average shifts.
"""

import numpy as np
from dataclasses import dataclass
from typing import Optional


@dataclass
class AnomalyResult:
    """Single anomaly detection result."""
    timestamp: int
    value: float
    score: float           # 0.0 (normal) to 1.0 (highly anomalous)
    type: str              # "point", "contextual", "collective", "trend"
    explanation: str
    method: str            # "zscore", "iqr", "ewma"


class ZScoreDetector:
    """
    Z-Score based point anomaly detection.

    ALGORITHM:
    1. Compute rolling mean and std over window
    2. Z-score = |value - mean| / std
    3. Anomaly if Z-score > threshold (default: 3.0)

    PERFORMANCE: <0.1ms per value
    """

    def __init__(self, window_size: int = 60, threshold: float = 3.0):
        self.window_size = window_size
        self.threshold = threshold

    def detect(self, values: list[float], timestamps: list[int]) -> list[AnomalyResult]:
        """Detect point anomalies using Z-Score."""
        anomalies = []
        arr = np.array(values, dtype=np.float64)

        for i in range(self.window_size, len(arr)):
            window = arr[max(0, i - self.window_size):i]
            mean = np.mean(window)
            std = np.std(window)

            if std < 1e-6:
                continue

            z_score = abs(arr[i] - mean) / std

            if z_score > self.threshold:
                score = min(1.0, (z_score - self.threshold) / self.threshold)
                anomalies.append(AnomalyResult(
                    timestamp=timestamps[i],
                    value=float(arr[i]),
                    score=score,
                    type="point",
                    explanation=f"Value {arr[i]:.2f} is {z_score:.1f} standard deviations from mean {mean:.2f} (threshold: {self.threshold})",
                    method="zscore",
                ))

        return anomalies


class IQRDetector:
    """
    IQR (Interquartile Range) based outlier detection.

    ALGORITHM:
    1. Compute Q1 (25th percentile) and Q3 (75th percentile)
    2. IQR = Q3 - Q1
    3. Outlier if value < Q1 - 1.5*IQR or value > Q3 + 1.5*IQR

    PERFORMANCE: <0.1ms per value
    """

    def __init__(self, window_size: int = 60, multiplier: float = 1.5):
        self.window_size = window_size
        self.multiplier = multiplier

    def detect(self, values: list[float], timestamps: list[int]) -> list[AnomalyResult]:
        """Detect outliers using IQR."""
        anomalies = []
        arr = np.array(values, dtype=np.float64)

        for i in range(self.window_size, len(arr)):
            window = arr[max(0, i - self.window_size):i]
            q1 = np.percentile(window, 25)
            q3 = np.percentile(window, 75)
            iqr = q3 - q1

            lower = q1 - self.multiplier * iqr
            upper = q3 + self.multiplier * iqr

            if arr[i] < lower or arr[i] > upper:
                distance = max(lower - arr[i], arr[i] - upper, 0)
                score = min(1.0, distance / (iqr + 1e-6))
                direction = "below" if arr[i] < lower else "above"
                anomalies.append(AnomalyResult(
                    timestamp=timestamps[i],
                    value=float(arr[i]),
                    score=score,
                    type="point",
                    explanation=f"Value {arr[i]:.2f} is {direction} IQR bounds [{lower:.2f}, {upper:.2f}]",
                    method="iqr",
                ))

        return anomalies


class EWMADetector:
    """
    EWMA (Exponential Weighted Moving Average) for trend anomaly detection.

    ALGORITHM:
    1. Compute EWMA: ewma_t = alpha * value_t + (1 - alpha) * ewma_{t-1}
    2. Compute EWMA variance
    3. Anomaly if |value - ewma| > threshold * sqrt(ewma_variance)

    PERFORMANCE: <0.1ms per value
    GOOD FOR: Detecting gradual drift and trend changes
    """

    def __init__(self, alpha: float = 0.3, threshold: float = 3.0):
        self.alpha = alpha
        self.threshold = threshold

    def detect(self, values: list[float], timestamps: list[int]) -> list[AnomalyResult]:
        """Detect trend anomalies using EWMA."""
        anomalies = []
        arr = np.array(values, dtype=np.float64)

        if len(arr) < 10:
            return anomalies

        # Initialize
        ewma = arr[0]
        ewma_var = 0.0

        for i in range(1, len(arr)):
            # Update EWMA
            ewma = self.alpha * arr[i] + (1 - self.alpha) * ewma
            # Update EWMA variance
            ewma_var = self.alpha * (arr[i] - ewma) ** 2 + (1 - self.alpha) * ewma_var

            std = np.sqrt(ewma_var)
            if std < 1e-6:
                continue

            deviation = abs(arr[i] - ewma) / std

            if deviation > self.threshold:
                score = min(1.0, (deviation - self.threshold) / self.threshold)
                anomalies.append(AnomalyResult(
                    timestamp=timestamps[i],
                    value=float(arr[i]),
                    score=score,
                    type="trend",
                    explanation=f"Value {arr[i]:.2f} deviates {deviation:.1f}σ from EWMA {ewma:.2f}",
                    method="ewma",
                ))

        return anomalies
```

### 4.3 Isolation Forest

**File:** `intelligence/anomaly/isolation_forest.py` (~400 LOC)

```python
"""
Isolation Forest anomaly detection.

Detects complex point anomalies in high-dimensional data
(multiple metrics together).

PERFORMANCE: <10ms per batch of 1000 points
"""

import numpy as np
from sklearn.ensemble import IsolationForest
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class IsolationForestDetector:
    """
    Isolation Forest for multi-dimensional anomaly detection.

    ALGORITHM:
    1. Build random trees that isolate data points
    2. Anomalies are isolated faster (shorter path length)
    3. Anomaly score based on average path length

    TRAINING: Fit on "normal" data (last 7 days)
    INFERENCE: <10ms per batch of 1000 points
    """

    def __init__(
        self,
        n_estimators: int = 100,
        contamination: float = 0.05,  # Expected 5% anomalies
        max_samples: int = 256,
    ):
        self.n_estimators = n_estimators
        self.contamination = contamination
        self.max_samples = max_samples
        self._model: Optional[IsolationForest] = None
        self._feature_names: list[str] = []

    def fit(self, data: np.ndarray, feature_names: list[str]) -> None:
        """
        Train on normal data.

        Args:
            data: Shape (n_samples, n_features)
            feature_names: Names for each feature column
        """
        self._feature_names = feature_names
        self._model = IsolationForest(
            n_estimators=self.n_estimators,
            contamination=self.contamination,
            max_samples=min(self.max_samples, len(data)),
            n_jobs=-1,
            random_state=42,
        )
        self._model.fit(data)
        logger.info(f"Isolation Forest trained on {data.shape[0]} samples, {data.shape[1]} features")

    def detect(self, data: np.ndarray) -> list[dict]:
        """
        Detect anomalies in new data.

        Args:
            data: Shape (n_samples, n_features)

        Returns:
            List of anomaly dicts with index, score, features
        """
        if self._model is None:
            raise RuntimeError("Model not fitted. Call fit() first.")

        # Predict: -1 = anomaly, 1 = normal
        predictions = self._model.predict(data)
        # Score: lower = more anomalous
        scores = self._model.decision_function(data)

        anomalies = []
        for i, (pred, score) in enumerate(zip(predictions, scores)):
            if pred == -1:
                # Normalize score to 0-1 (higher = more anomalous)
                normalized_score = float(np.clip(1.0 - (score + 0.5), 0.0, 1.0))

                # Identify contributing features
                contributing = []
                for j, name in enumerate(self._feature_names):
                    if abs(data[i, j]) > 2.0:  # >2 std from mean
                        contributing.append(name)

                anomalies.append({
                    'index': i,
                    'score': normalized_score,
                    'contributing_features': contributing,
                    'raw_score': float(score),
                })

        return anomalies
```

### 4.4 Autoencoder

**File:** `intelligence/anomaly/autoencoder.py` (~600 LOC)

```python
"""
Autoencoder-based anomaly detection using TensorFlow.

Detects contextual anomalies (memory leaks, gradual degradation)
and collective anomalies (cascading failures).

PERFORMANCE: <50ms per batch of 1000 points
"""

import numpy as np
import tensorflow as tf
from tensorflow import keras
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class AutoencoderDetector:
    """
    Autoencoder for contextual anomaly detection.

    ALGORITHM:
    1. Train autoencoder on normal metric patterns
    2. Autoencoder learns to reconstruct normal data well
    3. Anomalies have high reconstruction error
    4. Threshold on reconstruction error → anomaly

    ARCHITECTURE:
    - Encoder: Input → 64 → 32 → 16 (bottleneck)
    - Decoder: 16 → 32 → 64 → Output
    - Activation: ReLU, Output: Linear

    TRAINING: Fit on 7 days of normal data
    INFERENCE: <50ms per batch of 1000 points
    """

    def __init__(
        self,
        input_dim: int = 60,       # Window of 60 time steps
        encoding_dim: int = 16,    # Bottleneck size
        threshold_percentile: float = 95.0,
    ):
        self.input_dim = input_dim
        self.encoding_dim = encoding_dim
        self.threshold_percentile = threshold_percentile
        self._model: Optional[keras.Model] = None
        self._threshold: float = 0.0
        self._scaler_mean: float = 0.0
        self._scaler_std: float = 1.0

    def _build_model(self) -> keras.Model:
        """Build autoencoder architecture."""
        # Encoder
        inputs = keras.Input(shape=(self.input_dim,))
        encoded = keras.layers.Dense(64, activation='relu')(inputs)
        encoded = keras.layers.Dense(32, activation='relu')(encoded)
        encoded = keras.layers.Dense(self.encoding_dim, activation='relu')(encoded)

        # Decoder
        decoded = keras.layers.Dense(32, activation='relu')(encoded)
        decoded = keras.layers.Dense(64, activation='relu')(decoded)
        decoded = keras.layers.Dense(self.input_dim, activation='linear')(decoded)

        model = keras.Model(inputs, decoded)
        model.compile(optimizer='adam', loss='mse')
        return model

    def fit(self, data: np.ndarray, epochs: int = 50, batch_size: int = 32) -> None:
        """
        Train autoencoder on normal data.

        Args:
            data: Shape (n_samples, input_dim) — sliding windows of metric values
            epochs: Training epochs
            batch_size: Batch size
        """
        # Normalize
        self._scaler_mean = float(np.mean(data))
        self._scaler_std = float(np.std(data)) + 1e-6
        data_normalized = (data - self._scaler_mean) / self._scaler_std

        # Build and train
        self._model = self._build_model()

        # Suppress TF logging
        import os
        os.environ['TF_CPP_MIN_LOG_LEVEL'] = '2'

        self._model.fit(
            data_normalized, data_normalized,
            epochs=epochs,
            batch_size=batch_size,
            validation_split=0.1,
            verbose=0,
        )

        # Compute reconstruction errors on training data to set threshold
        reconstructed = self._model.predict(data_normalized, verbose=0)
        errors = np.mean((data_normalized - reconstructed) ** 2, axis=1)
        self._threshold = float(np.percentile(errors, self.threshold_percentile))

        logger.info(f"Autoencoder trained. Threshold: {self._threshold:.6f}")

    def detect(self, data: np.ndarray) -> list[dict]:
        """
        Detect anomalies via reconstruction error.

        Args:
            data: Shape (n_samples, input_dim)

        Returns:
            List of anomaly dicts
        """
        if self._model is None:
            raise RuntimeError("Model not fitted. Call fit() first.")

        # Normalize
        data_normalized = (data - self._scaler_mean) / self._scaler_std

        # Reconstruct
        reconstructed = self._model.predict(data_normalized, verbose=0)

        # Compute per-sample reconstruction error
        errors = np.mean((data_normalized - reconstructed) ** 2, axis=1)

        anomalies = []
        for i, error in enumerate(errors):
            if error > self._threshold:
                score = min(1.0, error / (self._threshold * 2))
                anomalies.append({
                    'index': i,
                    'score': float(score),
                    'reconstruction_error': float(error),
                    'threshold': self._threshold,
                })

        return anomalies
```

### 4.5 Ensemble Voting

**File:** `intelligence/anomaly/ensemble.py` (~300 LOC)

```python
"""
Ensemble voting for anomaly detection.

Combines results from Statistical, Isolation Forest, and Autoencoder
using a voting mechanism to reduce false positives.
"""

import numpy as np
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class AnomalyEnsemble:
    """
    Voting ensemble for anomaly detection.

    VOTING ALGORITHM:
    1. Each method votes: anomaly (1) or normal (0)
    2. Confidence = weighted sum of votes (weights based on method accuracy)
    3. Anomaly if confidence > threshold (default: 0.5)
    4. Anomaly score = weighted average of individual scores

    WEIGHTS:
    - Statistical: 0.3 (fast, good for point anomalies)
    - Isolation Forest: 0.35 (good for multi-dimensional)
    - Autoencoder: 0.35 (good for contextual/collective)
    """

    def __init__(self, vote_threshold: float = 0.5):
        self.vote_threshold = vote_threshold
        self.weights = {
            'statistical': 0.30,
            'isolation_forest': 0.35,
            'autoencoder': 0.35,
        }

    def combine(
        self,
        statistical_results: list[dict],
        isolation_results: list[dict],
        autoencoder_results: list[dict],
        n_samples: int,
    ) -> list[dict]:
        """
        Combine anomaly detection results from all methods.

        Args:
            statistical_results: Results from Z-Score/IQR/EWMA
            isolation_results: Results from Isolation Forest
            autoencoder_results: Results from Autoencoder
            n_samples: Total number of data points

        Returns:
            List of confirmed anomalies with combined scores and explanations
        """
        # Build per-sample scores
        scores = np.zeros((n_samples, 3))  # 3 methods
        explanations = [[], [], []]  # Per-method explanations

        # Fill statistical scores
        for r in statistical_results:
            idx = r.get('index', r.get('timestamp', 0))
            if 0 <= idx < n_samples:
                scores[idx, 0] = r.get('score', 0.0)

        # Fill isolation forest scores
        for r in isolation_results:
            idx = r.get('index', 0)
            if 0 <= idx < n_samples:
                scores[idx, 1] = r.get('score', 0.0)

        # Fill autoencoder scores
        for r in autoencoder_results:
            idx = r.get('index', 0)
            if 0 <= idx < n_samples:
                scores[idx, 2] = r.get('score', 0.0)

        # Weighted combination
        weight_array = np.array([
            self.weights['statistical'],
            self.weights['isolation_forest'],
            self.weights['autoencoder'],
        ])

        combined_scores = scores @ weight_array

        # Find anomalies above threshold
        anomalies = []
        for i in range(n_samples):
            if combined_scores[i] > self.vote_threshold:
                # Count voting methods
                votes = sum(1 for j in range(3) if scores[i, j] > 0.3)

                # Build explanation
                contributing = []
                if scores[i, 0] > 0.3:
                    contributing.append("statistical")
                if scores[i, 1] > 0.3:
                    contributing.append("isolation_forest")
                if scores[i, 2] > 0.3:
                    contributing.append("autoencoder")

                anomalies.append({
                    'index': i,
                    'score': float(combined_scores[i]),
                    'votes': votes,
                    'contributing_methods': contributing,
                    'confidence': votes / 3.0,
                })

        logger.info(f"Ensemble detected {len(anomalies)} anomalies from {n_samples} samples")
        return anomalies
```

---

## 5. Layer 23: Simulation Engine

### 5.1 Overview

The simulation engine provides "what-if" analysis for infrastructure changes. It models the impact of adding/removing nodes, scaling services, and injecting failures. It integrates with k6 for load testing and LitmusChaos for chaos engineering.

**V2.0 MIGRATION:** All core interfaces are designed to be extended for a full digital twin in V2.0. See `interfaces.go` for the extension points.

### 5.2 Core Interfaces (V2.0 Migration Path)

**File:** `cluster/internal/simulation/interfaces.go` (~300 LOC)

```go
// Package simulation provides what-if analysis and chaos engineering
// integration for the Paryty observability platform.
//
// V2.0 MIGRATION PATH:
// The interfaces in this file are designed to be extended for a full
// digital twin in V2.0. Specifically:
//   - SimulationEngine → DigitalTwinEngine (adds live mirroring)
//   - WhatIfScenario → LiveMirrorScenario (adds real traffic replay)
//   - SimulationResult → DigitalTwinResult (adds real-world comparison)
//   - TrafficPattern → RealTrafficMirror (adds production traffic capture)
package simulation

import (
	"context"
	"time"
)

// SimulationEngine is the core interface for running simulations.
//
// V2.0: Implement DigitalTwinEngine that embeds SimulationEngine
// and adds live traffic mirroring and real-time comparison.
type SimulationEngine interface {
	// RunScenario executes a what-if scenario and returns results.
	RunScenario(ctx context.Context, scenario WhatIfScenario) (SimulationResult, error)

	// ListScenarios returns all available scenarios.
	ListScenarios(ctx context.Context) ([]WhatIfScenario, error)

	// GetScenarioStatus returns the status of a running scenario.
	GetScenarioStatus(ctx context.Context, scenarioID string) (ScenarioStatus, error)

	// CancelScenario stops a running scenario.
	CancelScenario(ctx context.Context, scenarioID string) error
}

// WhatIfScenario defines a simulation scenario.
//
// V2.0: Extend to LiveMirrorScenario that adds:
//   - RealTrafficSource (capture from production)
//   - LiveComparison (compare simulated vs actual)
//   - AutoScaling (automatically adjust based on results)
type WhatIfScenario struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Changes     []InfraChange     `json:"changes"`
	Traffic     TrafficPattern    `json:"traffic"`
	Duration    time.Duration     `json:"duration"`
	Metrics     []string          `json:"metrics"` // Metrics to track
	Tags        map[string]string `json:"tags"`
	CreatedAt   time.Time         `json:"created_at"`
}

// InfraChange represents a change to the infrastructure.
type InfraChange struct {
	Type     ChangeType        `json:"type"`
	Target   string            `json:"target"`   // Service/host/container
	Action   string            `json:"action"`   // "add", "remove", "scale"
	Count    int               `json:"count"`    // Number of instances
	Config   map[string]string `json:"config"`   // Additional config
}

type ChangeType string

const (
	ChangeTypeHost      ChangeType = "host"
	ChangeTypeContainer ChangeType = "container"
	ChangeTypeService   ChangeType = "service"
	ChangeTypeNetwork   ChangeType = "network"
)

// TrafficPattern defines the simulated traffic.
//
// V2.0: Extend to RealTrafficMirror that replays actual production traffic.
type TrafficPattern struct {
	Type       TrafficType `json:"type"`
	RPS        int         `json:"rps"`         // Requests per second
	Duration   time.Duration `json:"duration"`
	Profile    string      `json:"profile"`     // "steady", "ramp", "spike", "wave"
	// V2.0 additions:
	// Source     string   `json:"source"`      // "simulated" or "production_mirror"
	// MirrorURL  string   `json:"mirror_url"`  // Production traffic mirror endpoint
}

type TrafficType string

const (
	TrafficTypeSimulated TrafficType = "simulated"
	TrafficTypeReplay    TrafficType = "replay"    // V2.0
	TrafficTypeMirror    TrafficType = "mirror"    // V2.0
)

// SimulationResult contains the results of a simulation.
//
// V2.0: Extend to DigitalTwinResult that adds:
//   - ActualMetrics (real production metrics for comparison)
//   - Deviation (difference between simulated and actual)
//   - Recommendations (auto-generated based on comparison)
type SimulationResult struct {
	ScenarioID    string                 `json:"scenario_id"`
	Status        ScenarioStatus         `json:"status"`
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Metrics       map[string][]float64   `json:"metrics"`       // metric → values over time
	Summary       SimulationSummary      `json:"summary"`
	BreakingPoint *BreakingPoint         `json:"breaking_point,omitempty"`
	// V2.0 additions:
	// ActualMetrics map[string][]float64 `json:"actual_metrics"`
	// Deviation     map[string]float64   `json:"deviation"`
	// Recommendations []string           `json:"recommendations"`
}

// ScenarioStatus represents the current status of a scenario.
type ScenarioStatus string

const (
	ScenarioStatusPending  ScenarioStatus = "pending"
	ScenarioStatusRunning  ScenarioStatus = "running"
	ScenarioStatusComplete ScenarioStatus = "complete"
	ScenarioStatusFailed   ScenarioStatus = "failed"
	ScenarioStatusCanceled ScenarioStatus = "canceled"
)

// SimulationSummary provides a high-level summary of simulation results.
type SimulationSummary struct {
	TotalRequests    int64         `json:"total_requests"`
	SuccessRate      float64       `json:"success_rate"`
	AvgLatency       time.Duration `json:"avg_latency"`
	P99Latency       time.Duration `json:"p99_latency"`
	MaxCPU           float64       `json:"max_cpu"`
	MaxMemory        float64       `json:"max_memory"`
	CostEstimate     float64       `json:"cost_estimate"`     // Monthly cost estimate
	Recommendations  []string      `json:"recommendations"`
}

// BreakingPoint represents the point at which the system fails.
type BreakingPoint struct {
	RPS          int           `json:"rps"`           // RPS at breaking point
	Latency      time.Duration `json:"latency"`       // P99 at breaking point
	ErrorRate    float64       `json:"error_rate"`    // Error rate at breaking point
	Bottleneck   string        `json:"bottleneck"`    // What broke first (CPU, memory, disk, network)
	Recommendations []string   `json:"recommendations"`
}
```

### 5.3 What-If Scenario Runner

**File:** `cluster/internal/simulation/whatif.go` (~600 LOC)

```go
package simulation

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"go.uber.org/zap"
)

// WhatIfRunner implements SimulationEngine for what-if analysis.
//
// ALGORITHM:
// 1. Capture current infrastructure state (topology + metrics baseline)
// 2. Apply proposed changes to a simulated topology
// 3. Run traffic simulation against the modified topology
// 4. Predict metrics using the forecasting engine
// 5. Detect anomalies in predicted metrics
// 6. Generate recommendations based on results
//
// V2.0 MIGRATION: Embed this in DigitalTwinEngine and add
// live traffic mirroring and real-time comparison.
type WhatIfRunner struct {
	store       *storage.Store
	logger      *zap.Logger
	forecastClient ForecastClient  // gRPC client to Python service
}

// ForecastClient is an interface for the forecasting gRPC client.
// V2.0: Same interface, different implementation (real-time vs batch).
type ForecastClient interface {
	ForecastMetric(ctx context.Context, agentID, metricName string, horizonSeconds int64) (*ForecastResponse, error)
}

// ForecastResponse is a simplified forecast response.
type ForecastResponse struct {
	Values     []float64
	LowerBound []float64
	UpperBound []float64
	Confidence float64
}

func NewWhatIfRunner(store *storage.Store, forecastClient ForecastClient, logger *zap.Logger) *WhatIfRunner {
	return &WhatIfRunner{
		store:          store,
		forecastClient: forecastClient,
		logger:         logger,
	}
}

// RunScenario executes a what-if scenario.
func (r *WhatIfRunner) RunScenario(ctx context.Context, scenario WhatIfScenario) (SimulationResult, error) {
	r.logger.Info("Starting what-if scenario",
		zap.String("id", scenario.ID),
		zap.String("name", scenario.Name),
		zap.Int("changes", len(scenario.Changes)),
	)

	startTime := time.Now()

	// 1. Capture baseline metrics
	baseline, err := r.captureBaseline(ctx, scenario)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("capture baseline: %w", err)
	}

	// 2. Apply changes to simulated topology
	simulatedTopology, err := r.applyChanges(baseline.Topology, scenario.Changes)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("apply changes: %w", err)
	}

	// 3. Predict metrics under new topology
	predictedMetrics, err := r.predictMetrics(ctx, simulatedTopology, scenario)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("predict metrics: %w", err)
	}

	// 4. Detect anomalies in predictions
	anomalies := r.detectAnomalies(predictedMetrics)

	// 5. Find breaking point
	breakingPoint := r.findBreakingPoint(predictedMetrics, scenario)

	// 6. Generate recommendations
	recommendations := r.generateRecommendations(predictedMetrics, anomalies, breakingPoint)

	// 7. Build summary
	summary := r.buildSummary(predictedMetrics, breakingPoint, recommendations)

	return SimulationResult{
		ScenarioID:    scenario.ID,
		Status:        ScenarioStatusComplete,
		StartTime:     startTime,
		EndTime:       time.Now(),
		Metrics:       predictedMetrics,
		Summary:       summary,
		BreakingPoint: breakingPoint,
	}, nil
}

// captureBaseline gets the current infrastructure state.
func (r *WhatIfRunner) captureBaseline(ctx context.Context, scenario WhatIfScenario) (*Baseline, error) {
	// Query current topology from hot store
	topology, err := r.store.Hot().GetTopology(ctx)
	if err != nil {
		return nil, fmt.Errorf("get topology: %w", err)
	}

	// Query recent metrics for each tracked metric
	metrics := make(map[string][]float64)
	for _, metricName := range scenario.Metrics {
		values, err := r.store.Warm().QueryRecentMetrics(ctx, metricName, 1*time.Hour)
		if err != nil {
			r.logger.Warn("Failed to query baseline metric",
				zap.String("metric", metricName),
				zap.Error(err),
			)
			continue
		}
		metrics[metricName] = values
	}

	return &Baseline{
		Topology: topology,
		Metrics:  metrics,
	}, nil
}

// applyChanges modifies the simulated topology.
func (r *WhatIfRunner) applyChanges(topology *models.Topology, changes []InfraChange) (*models.Topology, error) {
	// Deep copy topology
	sim := topology.DeepCopy()

	for _, change := range changes {
		switch change.Action {
		case "add":
			for i := 0; i < change.Count; i++ {
				sim.AddNode(change.Type, change.Target, change.Config)
			}
		case "remove":
			for i := 0; i < change.Count; i++ {
				sim.RemoveNode(change.Type, change.Target)
			}
		case "scale":
			sim.ScaleNode(change.Type, change.Target, change.Count)
		}
	}

	return sim, nil
}

// predictMetrics uses the forecasting engine to predict metrics under new topology.
func (r *WhatIfRunner) predictMetrics(ctx context.Context, topology *models.Topology, scenario WhatIfScenario) (map[string][]float64, error) {
	metrics := make(map[string][]float64)

	for _, metricName := range scenario.Metrics {
		// Adjust forecast based on topology changes
		// For example: adding 2 hosts → CPU usage expected to decrease ~50%
		adjustment := r.computeAdjustment(topology, metricName, scenario.Changes)

		// Call forecasting service
		forecast, err := r.forecastClient.ForecastMetric(ctx, "", metricName, int64(scenario.Duration.Seconds()))
		if err != nil {
			r.logger.Warn("Forecast failed, using baseline",
				zap.String("metric", metricName),
				zap.Error(err),
			)
			continue
		}

		// Apply adjustment
		adjusted := make([]float64, len(forecast.Values))
		for i, v := range forecast.Values {
			adjusted[i] = v * adjustment
		}

		metrics[metricName] = adjusted
	}

	return metrics, nil
}

// computeAdjustment calculates how topology changes affect metrics.
func (r *WhatIfRunner) computeAdjustment(topology *models.Topology, metricName string, changes []InfraChange) float64 {
	// Simple linear adjustment model
	// V2.0: Use ML-based adjustment
	adjustment := 1.0

	for _, change := range changes {
		switch change.Action {
		case "add":
			// Adding resources decreases utilization
			factor := 1.0 / (1.0 + float64(change.Count))
			adjustment *= factor
		case "remove":
			// Removing resources increases utilization
			factor := 1.0 + float64(change.Count)*0.3
			adjustment *= factor
		case "scale":
			// Scaling changes throughput
			adjustment *= float64(change.Count) / 100.0
		}
	}

	return adjustment
}

// detectAnomalies checks predicted metrics for anomalies.
func (r *WhatIfRunner) detectAnomalies(metrics map[string][]float64) []string {
	var anomalies []string
	for name, values := range metrics {
		for _, v := range values {
			if v > 0.95 { // >95% utilization
				anomalies = append(anomalies, fmt.Sprintf("%s will reach %.1f%% utilization", name, v*100))
				break
			}
		}
	}
	return anomalies
}

// findBreakingPoint finds the point at which the system fails.
func (r *WhatIfRunner) findBreakingPoint(metrics map[string][]float64, scenario WhatIfScenario) *BreakingPoint {
	// Check if any metric exceeds safe thresholds
	for name, values := range metrics {
		for i, v := range values {
			if v > 0.99 { // >99% = breaking point
				return &BreakingPoint{
					RPS:       scenario.Traffic.RPS,
					Bottleneck: name,
					ErrorRate:  0.1, // Estimated
					Recommendations: []string{
						fmt.Sprintf("Scale %s before reaching %d RPS", name, scenario.Traffic.RPS),
					},
				}
			}
		}
	}
	return nil
}

// generateRecommendations creates actionable recommendations.
func (r *WhatIfRunner) generateRecommendations(metrics map[string][]float64, anomalies []string, bp *BreakingPoint) []string {
	recs := []string{}

	if bp != nil {
		recs = append(recs, fmt.Sprintf("System breaks at %d RPS (bottleneck: %s)", bp.RPS, bp.Bottleneck))
		recs = append(recs, bp.Recommendations...)
	}

	for _, a := range anomalies {
		recs = append(recs, "WARNING: "+a)
	}

	if len(recs) == 0 {
		recs = append(recs, "Proposed changes appear safe. No anomalies detected.")
	}

	return recs
}

func (r *WhatIfRunner) buildSummary(metrics map[string][]float64, bp *BreakingPoint, recs []string) SimulationSummary {
	return SimulationSummary{
		Recommendations: recs,
	}
}

// Baseline holds the current infrastructure state.
type Baseline struct {
	Topology *models.Topology
	Metrics  map[string][]float64
}
```

### 5.4 Chaos Engineering Integration

**File:** `cluster/internal/simulation/chaos.go` (~400 LOC)

```go
package simulation

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"go.uber.org/zap"
)

// ChaosRunner integrates with k6 and LitmusChaos for real-world validation.
//
// V2.0: Same interface, but ChaosRunner becomes part of DigitalTwinEngine.
type ChaosRunner struct {
	logger     *zap.Logger
	k6Path     string           // Path to k6 binary
	litmusURL  string           // LitmusChaos server URL
}

func NewChaosRunner(k6Path, litmusURL string, logger *zap.Logger) *ChaosRunner {
	return &ChaosRunner{
		k6Path:    k6Path,
		litmusURL: litmusURL,
		logger:    logger,
	}
}

// RunLoadTest executes a k6 load test.
//
// k6 script template:
//   import http from 'k6/http';
//   import { check } from 'k6';
//
//   export let options = {
//     stages: [
//       { duration: '1m', target: RPS },  // Ramp up
//       { duration: '5m', target: RPS },  // Sustained load
//       { duration: '1m', target: 0 },    // Ramp down
//     ],
//   };
//
//   export default function () {
//     let res = http.get(TARGET_URL);
//     check(res, { 'status is 200': (r) => r.status === 200 });
//   }
func (c *ChaosRunner) RunLoadTest(ctx context.Context, config LoadTestConfig) (*LoadTestResult, error) {
	c.logger.Info("Starting k6 load test",
		zap.String("target", config.TargetURL),
		zap.Int("rps", config.RPS),
		zap.Duration("duration", config.Duration),
	)

	// Generate k6 script
	script := c.generateK6Script(config)

	// Write script to temp file
	tmpFile := fmt.Sprintf("/tmp/k6-script-%d.js", time.Now().UnixNano())
	if err := writeTempFile(tmpFile, script); err != nil {
		return nil, fmt.Errorf("write k6 script: %w", err)
	}

	// Run k6
	cmd := exec.CommandContext(ctx, c.k6Path, "run", "--out", "json=/tmp/k6-results.json", tmpFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		c.logger.Error("k6 failed", zap.String("output", string(output)), zap.Error(err))
		return nil, fmt.Errorf("k6 run: %w", err)
	}

	// Parse results
	result := c.parseK6Output(string(output))

	c.logger.Info("k6 load test complete",
		zap.Int("total_requests", int(result.TotalRequests)),
		zap.Float64("success_rate", result.SuccessRate),
		zap.Duration("p99_latency", result.P99Latency),
	)

	return result, nil
}

// RunChaosExperiment triggers a LitmusChaos experiment.
func (c *ChaosRunner) RunChaosExperiment(ctx context.Context, experiment ChaosExperiment) (*ChaosResult, error) {
	c.logger.Info("Starting LitmusChaos experiment",
		zap.String("name", experiment.Name),
		zap.String("type", experiment.Type),
	)

	// Call LitmusChaos API
	// POST to litmusURL/api/v1/experiments
	// Parse response
	// Poll for completion

	return &ChaosResult{
		ExperimentName: experiment.Name,
		Status:         "completed",
	}, nil
}

type LoadTestConfig struct {
	TargetURL string        `json:"target_url"`
	RPS       int           `json:"rps"`
	Duration  time.Duration `json:"duration"`
	Headers   map[string]string `json:"headers"`
}

type LoadTestResult struct {
	TotalRequests int64         `json:"total_requests"`
	SuccessRate   float64       `json:"success_rate"`
	AvgLatency    time.Duration `json:"avg_latency"`
	P99Latency    time.Duration `json:"p99_latency"`
	Errors        int64         `json:"errors"`
}

type ChaosExperiment struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`     // "pod-kill", "network-delay", "cpu-stress"
	Target   string            `json:"target"`   // Target service/pod
	Duration time.Duration     `json:"duration"`
	Config   map[string]string `json:"config"`
}

type ChaosResult struct {
	ExperimentName string `json:"experiment_name"`
	Status         string `json:"status"`
	Impact         string `json:"impact"`
}
```

---

## 6. Layer 24: Timeline Engine

### 6.1 Snapshot Manager

**File:** `cluster/internal/timeline/snapshot_manager.go` (~400 LOC)

```go
package timeline

import (
	"context"
	"fmt"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"go.uber.org/zap"
)

// SnapshotManager manages 5-minute interval snapshots.
//
// Uses the snapshot + event log architecture from Phase 4.
// This layer adds: snapshot diffing, tagging, and management.
type SnapshotManager struct {
	store  *storage.Store
	logger *zap.Logger
}

func NewSnapshotManager(store *storage.Store, logger *zap.Logger) *SnapshotManager {
	return &SnapshotManager{store: store, logger: logger}
}

// CreateSnapshot captures the current system state.
func (m *SnapshotManager) CreateSnapshot(ctx context.Context) (*models.Snapshot, error) {
	// Capture topology
	topology, err := m.store.Hot().GetTopology(ctx)
	if err != nil {
		return nil, fmt.Errorf("get topology: %w", err)
	}

	// Capture current metrics summary
	metrics, err := m.store.Warm().QueryRecentMetricsSummary(ctx, 5*time.Minute)
	if err != nil {
		return nil, fmt.Errorf("get metrics: %w", err)
	}

	// Capture active alerts
	alerts, err := m.store.Hot().GetActiveAlerts(ctx)
	if err != nil {
		m.logger.Warn("Failed to get alerts for snapshot", zap.Error(err))
		alerts = []models.Alert{}
	}

	snapshot := &models.Snapshot{
		ID:        fmt.Sprintf("snap_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Topology:  topology,
		Metrics:   metrics,
		Alerts:    alerts,
	}

	// Store to cold store (SeaweedFS)
	if err := m.store.Cold().StoreSnapshot(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("store snapshot: %w", err)
	}

	m.logger.Info("Snapshot created",
		zap.String("id", snapshot.ID),
		zap.Int("nodes", len(topology.Nodes)),
		zap.Int("alerts", len(alerts)),
	)

	return snapshot, nil
}

// DiffSnapshots compares two snapshots.
func (m *SnapshotManager) DiffSnapshots(ctx context.Context, fromID, toID string) (*SnapshotDiff, error) {
	from, err := m.store.Cold().GetSnapshot(ctx, fromID)
	if err != nil {
		return nil, fmt.Errorf("get snapshot %s: %w", fromID, err)
	}

	to, err := m.store.Cold().GetSnapshot(ctx, toID)
	if err != nil {
		return nil, fmt.Errorf("get snapshot %s: %w", toID, err)
	}

	diff := &SnapshotDiff{
		FromID:    fromID,
		ToID:      toID,
		FromTime:  from.Timestamp,
		ToTime:    to.Timestamp,
	}

	// Topology diff
	diff.NodesAdded = findAddedNodes(from.Topology, to.Topology)
	diff.NodesRemoved = findRemovedNodes(from.Topology, to.Topology)
	diff.EdgesAdded = findAddedEdges(from.Topology, to.Topology)
	diff.EdgesRemoved = findRemovedEdges(from.Topology, to.Topology)

	// Metrics diff
	diff.MetricChanges = compareMetrics(from.Metrics, to.Metrics)

	// Alert diff
	diff.AlertsResolved = findResolvedAlerts(from.Alerts, to.Alerts)
	diff.AlertsNew = findNewAlerts(from.Alerts, to.Alerts)

	return diff, nil
}

// SnapshotDiff contains the differences between two snapshots.
type SnapshotDiff struct {
	FromID         string                 `json:"from_id"`
	ToID           string                 `json:"to_id"`
	FromTime       time.Time              `json:"from_time"`
	ToTime         time.Time              `json:"to_time"`
	NodesAdded     []string               `json:"nodes_added"`
	NodesRemoved   []string               `json:"nodes_removed"`
	EdgesAdded     []string               `json:"edges_added"`
	EdgesRemoved   []string               `json:"edges_removed"`
	MetricChanges  map[string]MetricDelta `json:"metric_changes"`
	AlertsNew      []models.Alert         `json:"alerts_new"`
	AlertsResolved []models.Alert         `json:"alerts_resolved"`
}

type MetricDelta struct {
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Change    float64 `json:"change"`
	ChangePct float64 `json:"change_pct"`
}

// TagSnapshot adds a tag to a snapshot (e.g., "incident", "deployment").
func (m *SnapshotManager) TagSnapshot(ctx context.Context, snapshotID, tag string) error {
	return m.store.Cold().TagSnapshot(ctx, snapshotID, tag)
}
```

### 6.2 Replay Engine

**File:** `cluster/internal/timeline/replay_engine.go` (~300 LOC)

```go
package timeline

import (
	"context"
	"time"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage"
	"github.com/paryty/paryty-v1.0/cluster/internal/stream"
	"go.uber.org/zap"
)

// ReplayEngine replays system state at any point in time.
//
// Uses the snapshot + event log architecture:
// 1. Load the nearest snapshot before the target time
// 2. Replay events from the event log up to the target time
// 3. Stream the reconstructed state to the frontend via SSE
type ReplayEngine struct {
	store    *storage.Store
	stream   *stream.StreamEngine
	logger   *zap.Logger
}

func NewReplayEngine(store *storage.Store, streamEngine *stream.StreamEngine, logger *zap.Logger) *ReplayEngine {
	return &ReplayEngine{store: store, stream: streamEngine, logger: logger}
}

// Replay streams historical state to the client.
func (e *ReplayEngine) Replay(ctx context.Context, config ReplayConfig) (<-chan ReplayFrame, error) {
	frames := make(chan ReplayFrame, 100)

	go func() {
		defer close(frames)

		// 1. Load nearest snapshot
		snapshot, err := e.store.Cold().GetNearestSnapshot(ctx, config.StartTime)
		if err != nil {
			e.logger.Error("Failed to load snapshot", zap.Error(err))
			return
		}

		// 2. Load events between snapshot and start time
		events, err := e.store.Cold().GetEvents(ctx, snapshot.Timestamp, config.EndTime)
		if err != nil {
			e.logger.Error("Failed to load events", zap.Error(err))
			return
		}

		// 3. Reconstruct state by replaying events
		state := snapshot.ToTopology()

		eventIdx := 0
		currentTime := config.StartTime
		step := time.Duration(float64(time.Second) / float64(config.Speed))

		for currentTime.Before(config.EndTime) {
			// Apply events up to current time
			for eventIdx < len(events) && events[eventIdx].Timestamp.Before(currentTime) {
				state.ApplyEvent(events[eventIdx])
				eventIdx++
			}

			// Send frame
			frame := ReplayFrame{
				Timestamp: currentTime,
				Topology:  state,
				Progress:  currentTime.Sub(config.StartTime).Seconds() / config.EndTime.Sub(config.StartTime).Seconds(),
			}

			select {
			case frames <- frame:
			case <-ctx.Done():
				return
			}

			// Check for pause
			if config.Paused {
				select {
				case <-config.ResumeCh:
				case <-ctx.Done():
					return
				}
			}

			currentTime = currentTime.Add(step)
		}
	}()

	return frames, nil
}

type ReplayConfig struct {
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Speed     float64       `json:"speed"`     // 0.25 to 16.0
	Paused    bool          `json:"paused"`
	ResumeCh  chan struct{}  `json:"-"`         // Signal to resume
}

type ReplayFrame struct {
	Timestamp time.Time       `json:"timestamp"`
	Topology  *models.Topology `json:"topology"`
	Progress  float64         `json:"progress"` // 0.0 - 1.0
}
```

### 6.3 Diff Calculator & Export Manager

**File:** `cluster/internal/timeline/diff_calculator.go` (~200 LOC)

```go
package timeline

import (
	"context"
	"time"
)

// DiffCalculator compares system state between two points in time.
type DiffCalculator struct {
	snapshotManager *SnapshotManager
}

func NewDiffCalculator(sm *SnapshotManager) *DiffCalculator {
	return &DiffCalculator{snapshotManager: sm}
}

// CalculateDiff generates a human-readable diff report.
func (d *DiffCalculator) CalculateDiff(ctx context.Context, from, to time.Time) (*DiffReport, error) {
	fromSnap, err := d.snapshotManager.GetNearestSnapshot(ctx, from)
	if err != nil {
		return nil, err
	}

	toSnap, err := d.snapshotManager.GetNearestSnapshot(ctx, to)
	if err != nil {
		return nil, err
	}

	diff, err := d.snapshotManager.DiffSnapshots(ctx, fromSnap.ID, toSnap.ID)
	if err != nil {
		return nil, err
	}

	return &DiffReport{
		From:         from,
		To:           to,
		Diff:         diff,
		Summary:      d.generateSummary(diff),
	}, nil
}

type DiffReport struct {
	From    time.Time     `json:"from"`
	To      time.Time     `json:"to"`
	Diff    *SnapshotDiff `json:"diff"`
	Summary string        `json:"summary"`
}
```

**File:** `cluster/internal/timeline/export_manager.go` (~200 LOC)

```go
package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ExportManager exports snapshots and reports.
type ExportManager struct {
	snapshotManager *SnapshotManager
	diffCalculator  *DiffCalculator
}

func NewExportManager(sm *SnapshotManager, dc *DiffCalculator) *ExportManager {
	return &ExportManager{snapshotManager: sm, diffCalculator: dc}
}

// ExportJSON exports a snapshot as JSON.
func (e *ExportManager) ExportJSON(ctx context.Context, snapshotID string) ([]byte, error) {
	snapshot, err := e.snapshotManager.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(snapshot, "", "  ")
}

// ExportDiffJSON exports a diff report as JSON.
func (e *ExportManager) ExportDiffJSON(ctx context.Context, from, to time.Time) ([]byte, error) {
	report, err := e.diffCalculator.CalculateDiff(ctx, from, to)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(report, "", "  ")
}

// ExportHTML generates an interactive HTML timeline.
func (e *ExportManager) ExportHTML(ctx context.Context, from, to time.Time) ([]byte, error) {
	report, err := e.diffCalculator.CalculateDiff(ctx, from, to)
	if err != nil {
		return nil, err
	}

	// Generate HTML with embedded data
	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>Paryty Timeline Report</title>
  <style>body{font-family:'Geist Mono',monospace;background:#0a0a0a;color:#fff;}</style>
</head>
<body>
  <h1>Timeline Report</h1>
  <p>From: %s</p>
  <p>To: %s</p>
  <pre>%s</pre>
</body>
</html>`, from.Format(time.RFC3339), to.Format(time.RFC3339), report.Summary)

	return []byte(html), nil
}
```

---

## 7. Python Service Infrastructure

### 7.1 gRPC Server Entry Point

**File:** `intelligence/server/main.py` (~200 LOC)

```python
"""
Paryty Intelligence Service — gRPC Server Entry Point.

Runs the forecasting and anomaly detection gRPC services.
"""

import grpc
from concurrent import futures
import signal
import sys
import logging
import os

from .forecasting_service import ForecastingServicer
from .anomaly_service import AnomalyServicer

# Import generated proto stubs
from proto import forecasting_pb2_grpc
from proto import anomaly_pb2_grpc

logger = logging.getLogger(__name__)


def serve(port: int = 50051):
    """Start the gRPC server."""
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))

    # Register services
    forecasting_pb2_grpc.add_ForecastingServiceServicer_to_server(
        ForecastingServicer(), server
    )
    anomaly_pb2_grpc.add_AnomalyDetectionServiceServicer_to_server(
        AnomalyServicer(), server
    )

    server.add_insecure_port(f'[::]:{port}')
    server.start()

    logger.info(f"Intelligence service started on port {port}")

    # Graceful shutdown
    def shutdown(signum, frame):
        logger.info("Shutting down intelligence service...")
        server.stop(grace=5)
        sys.exit(0)

    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    server.wait_for_termination()


if __name__ == '__main__':
    logging.basicConfig(level=logging.INFO)
    port = int(os.getenv('INTELLIGENCE_PORT', '50051'))
    serve(port)
```

### 7.2 New Redpanda Topics

Add to `cluster/internal/stream/topics.go`:

```go
// Intelligence layer topics
TopicForecasts      = "paryty.forecasts"       // Forecasting results
TopicAnomalies      = "paryty.anomalies"       // Anomaly detection results
TopicSimulations    = "paryty.simulations"     // Simulation results
TopicTimelineEvents = "paryty.timeline.events" // Timeline replay events
```

---

## 8. Verification Gates

### Gate 1: Forecasting Accuracy (Automated)

```python
# Test: Linear regression accuracy
# 1. Generate synthetic linear data (y = 2t + 10 + noise)
# 2. Train linear forecaster
# 3. Predict next 60 points
# 4. Assert: MAPE < 5%

# Test: Prophet seasonal detection
# 1. Generate data with daily seasonality
# 2. Train Prophet
# 3. Predict next 24 hours
# 4. Assert: detects daily pattern, MAPE < 10%

# Test: Ensemble self-correction
# 1. Train ensemble with 3 models
# 2. Simulate 24 hours of accuracy tracking
# 3. Assert: weights shift toward most accurate model
```

### Gate 2: Anomaly Detection (Automated)

```python
# Test: Z-Score point anomaly detection
# 1. Generate normal data + 5 injected anomalies
# 2. Run Z-Score detector
# 3. Assert: detects all 5 anomalies
# 4. Assert: false positive rate < 5%

# Test: Autoencoder contextual anomaly
# 1. Train on normal data (1 week)
# 2. Inject memory leak pattern
# 3. Assert: detects the leak before it reaches critical

# Test: Ensemble reduces false positives
# 1. Run all 3 methods on noisy data
# 2. Run ensemble voting
# 3. Assert: ensemble false positives < any individual method
```

### Gate 3: Simulation Engine (Automated)

```go
// Test: What-if scenario execution
// 1. Create scenario: add 2 hosts, steady traffic at 1000 RPS
// 2. Run scenario
// 3. Assert: completes without error
// 4. Assert: result contains metrics and recommendations

// Test: Breaking point detection
// 1. Create scenario: ramp traffic from 100 to 10000 RPS
// 2. Run scenario
// 3. Assert: breaking point detected
// 4. Assert: bottleneck identified
```

### Gate 4: Timeline Engine (Automated)

```go
// Test: Snapshot creation
// 1. Create snapshot
// 2. Assert: snapshot contains topology, metrics, alerts
// 3. Assert: stored in cold store

// Test: Diff calculation
// 1. Create two snapshots with different topology
// 2. Calculate diff
// 3. Assert: nodes added/removed identified

// Test: Replay
// 1. Create snapshots and events
// 2. Start replay
// 3. Assert: frames stream at configured speed
// 4. Assert: topology matches expected state at each frame
```

### Gate 5: gRPC Integration (Automated)

```python
# Test: Forecasting gRPC round-trip
# 1. Start Python service
# 2. Go client sends ForecastMetricRequest
# 3. Assert: receives ForecastMetricResponse with valid data

# Test: Anomaly detection gRPC round-trip
# 1. Start Python service
# 2. Go client sends DetectAnomaliesRequest
# 3. Assert: receives DetectAnomaliesResponse with anomalies
```

---

## 9. Performance Targets

### 9.1 Forecasting Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| Linear regression forecast | < 1ms | Single metric, 60-point window |
| Prophet forecast | < 5s | Single metric, 7 days training data |
| XGBoost forecast | < 10s | Single metric, 7 days training data |
| Ensemble forecast | < 10s | All 3 models combined |
| Batch forecast (100 metrics) | < 30s | 100 metrics in parallel |
| Model retrain (all metrics) | < 5min | All models, all metrics |

### 9.2 Anomaly Detection Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| Statistical detection | < 1ms | Single metric, 1000 points |
| Isolation Forest detection | < 10ms | Single metric, 1000 points |
| Autoencoder detection | < 50ms | Single metric, 1000 points |
| Ensemble detection | < 50ms | All 3 methods combined |
| Cross-metric detection | < 200ms | 10 metrics, 1000 points each |

### 9.3 Simulation Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| What-if scenario | < 30s | 5 changes, 1-hour simulation |
| Breaking point detection | < 60s | Ramp from 100 to 10000 RPS |
| k6 load test | < 10min | Full load test cycle |
| Chaos experiment | < 5min | Single fault injection |

### 9.4 Timeline Performance

| Metric | Target | Measurement |
|--------|--------|-------------|
| Snapshot creation | < 2s | Full topology + metrics + alerts |
| Snapshot retrieval | < 500ms | From cold store |
| Diff calculation | < 1s | Between two snapshots |
| Replay stream | > 30 fps | At 1x speed |
| JSON export | < 5s | Single snapshot |
| HTML export | < 10s | Full report |

---

## 10. Contingency & Rollback

### 10.1 Rollback Strategy

**Scenario 1: Python service crashes**
- Go cluster continues operating without forecasting/anomaly detection
- Cached forecasts served from Dragonfly (up to 1 hour stale)
- Alert: "Intelligence service unavailable"

**Scenario 2: Forecasting accuracy is poor**
- Fall back to Linear Regression only (fastest, simplest)
- Disable Prophet and XGBoost
- Increase retraining frequency

**Scenario 3: Anomaly detection has high false positives**
- Increase vote_threshold from 0.5 to 0.7
- Disable autoencoder (most prone to false positives)
- Use statistical methods only

**Scenario 4: Simulation engine too slow**
- Reduce simulation duration (1 hour → 15 minutes)
- Disable chaos engineering integration
- Use simple linear adjustment model only

### 10.2 Graceful Degradation

| Feature | Full | Degraded | Minimal |
|---------|------|----------|---------|
| Forecasting | Ensemble (3 models) | Linear only | Cached forecasts |
| Anomaly Detection | Ensemble (3 methods) | Statistical only | Z-Score only |
| Simulation | What-if + Chaos | What-if only | Capacity planning only |
| Timeline | Full replay + diff | Snapshot only | Manual refresh |

---

## 11. Appendices

### Appendix A: Files to Create/Modify

| File | Action | LOC | Language |
|------|--------|-----|----------|
| `intelligence/forecasting/linear.py` | CREATE | ~400 | Python |
| `intelligence/forecasting/prophet.py` | CREATE | ~500 | Python |
| `intelligence/forecasting/xgboost_model.py` | CREATE | ~500 | Python |
| `intelligence/forecasting/ensemble.py` | CREATE | ~400 | Python |
| `intelligence/forecasting/model_store.py` | CREATE | ~200 | Python |
| `intelligence/forecasting/config.py` | CREATE | ~100 | Python |
| `intelligence/forecasting/feature_engineering.py` | CREATE | ~300 | Python |
| `intelligence/anomaly/statistical.py` | CREATE | ~500 | Python |
| `intelligence/anomaly/isolation_forest.py` | CREATE | ~400 | Python |
| `intelligence/anomaly/autoencoder.py` | CREATE | ~600 | Python |
| `intelligence/anomaly/ensemble.py` | CREATE | ~300 | Python |
| `intelligence/anomaly/explainer.py` | CREATE | ~300 | Python |
| `intelligence/anomaly/model_store.py` | CREATE | ~200 | Python |
| `intelligence/anomaly/config.py` | CREATE | ~100 | Python |
| `intelligence/data/questdb_client.py` | CREATE | ~300 | Python |
| `intelligence/data/dragonfly_client.py` | CREATE | ~200 | Python |
| `intelligence/data/timeseries.py` | CREATE | ~200 | Python |
| `intelligence/server/forecasting_service.py` | CREATE | ~300 | Python |
| `intelligence/server/anomaly_service.py` | CREATE | ~300 | Python |
| `intelligence/server/main.py` | CREATE | ~200 | Python |
| `intelligence/tests/test_*.py` | CREATE | ~1,000 | Python |
| `intelligence/pyproject.toml` | CREATE | ~50 | Python |
| `intelligence/requirements.txt` | CREATE | ~30 | Python |
| `intelligence/Dockerfile` | CREATE | ~40 | Docker |
| `proto/paryty/v1/forecasting.proto` | CREATE | ~150 | Protobuf |
| `proto/paryty/v1/anomaly.proto` | CREATE | ~150 | Protobuf |
| `proto/paryty/v1/simulation.proto` | CREATE | ~100 | Protobuf |
| `proto/paryty/v1/timeline.proto` | CREATE | ~100 | Protobuf |
| `cluster/internal/simulation/interfaces.go` | CREATE | ~300 | Go |
| `cluster/internal/simulation/engine.go` | CREATE | ~200 | Go |
| `cluster/internal/simulation/whatif.go` | CREATE | ~600 | Go |
| `cluster/internal/simulation/chaos.go` | CREATE | ~400 | Go |
| `cluster/internal/simulation/bot_detection.go` | CREATE | ~400 | Go |
| `cluster/internal/simulation/degradation.go` | CREATE | ~300 | Go |
| `cluster/internal/simulation/capacity.go` | CREATE | ~300 | Go |
| `cluster/internal/timeline/snapshot_manager.go` | CREATE | ~400 | Go |
| `cluster/internal/timeline/replay_engine.go` | CREATE | ~300 | Go |
| `cluster/internal/timeline/diff_calculator.go` | CREATE | ~200 | Go |
| `cluster/internal/timeline/export_manager.go` | CREATE | ~200 | Go |
| `cluster/internal/intelligence/forecasting_client.go` | CREATE | ~300 | Go |
| `cluster/internal/intelligence/anomaly_client.go` | CREATE | ~300 | Go |
| `cluster/internal/intelligence/cache.go` | CREATE | ~200 | Go |
| `cluster/internal/api/handler/forecast.go` | CREATE | ~200 | Go |
| `cluster/internal/api/handler/anomaly.go` | CREATE | ~200 | Go |
| `cluster/internal/api/handler/simulation.go` | CREATE | ~200 | Go |
| `cluster/internal/api/handler/timeline.go` | CREATE | ~200 | Go |
| `cluster/internal/stream/topics.go` | UPDATE | +10 | Go |
| `configs/intelligence/intelligence.yaml` | CREATE | ~50 | YAML |
| **TOTAL** | | **~14,200** | |

### Appendix B: Intelligence Service Configuration

**File:** `configs/intelligence/intelligence.yaml`

```yaml
# Paryty Intelligence Service Configuration
server:
  port: 50051
  max_workers: 10
  log_level: info

forecasting:
  enabled: true
  models:
    linear:
      window_size: 60
      confidence_level: 0.95
    prophet:
      daily_seasonality: true
      weekly_seasonality: true
      changepoint_prior_scale: 0.05
    xgboost:
      n_estimators: 100
      max_depth: 6
      learning_rate: 0.1
  ensemble:
    weight_update_interval: 3600  # seconds (1 hour)
    min_weight: 0.1
    max_weight: 0.6
  retrain_interval: 86400  # seconds (24 hours)
  model_store_path: /data/models/forecasting

anomaly:
  enabled: true
  models:
    zscore:
      window_size: 60
      threshold: 3.0
    iqr:
      window_size: 60
      multiplier: 1.5
    ewma:
      alpha: 0.3
      threshold: 3.0
    isolation_forest:
      n_estimators: 100
      contamination: 0.05
    autoencoder:
      encoding_dim: 16
      threshold_percentile: 95.0
  ensemble:
    vote_threshold: 0.5
  model_store_path: /data/models/anomaly

data:
  questdb:
    host: questdb
    port: 8812
    database: paryty
  dragonfly:
    host: dragonfly
    port: 6379
```

---

**END OF PHASE 6 HARDENED SPECIFICATION**
