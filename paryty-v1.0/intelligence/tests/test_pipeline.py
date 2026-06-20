"""
Integration tests for the Intelligence Pipeline.

Tests the MetricBuffer, PipelineConfig, and IntelligencePipeline
orchestrator with mocked dependencies (Dragonfly, AnomalyServicer,
ForecastingServicer, IntelligenceProducer).
"""

from __future__ import annotations

import time
import threading
from unittest.mock import MagicMock, patch

import pytest

from intelligence.pipeline.buffer import MetricBuffer
from intelligence.pipeline.config import PipelineConfig
from intelligence.proto.models import (
    AnomalyProto,
    AnomalyTypeProto,
    DetectAnomaliesResponse,
    ForecastMetricResponse,
    ModelInfo,
    SeverityProto,
)


# =====================================================================
# Fixtures
# =====================================================================


@pytest.fixture()
def config() -> PipelineConfig:
    return PipelineConfig(
        enabled=True,
        anomaly_enabled=True,
        forecast_enabled=True,
        drift_enabled=True,
        retrain_enabled=False,  # disable for tests (needs real retrain_fn)
        anomaly_min_points=3,
        forecast_interval_seconds=0,  # no delay for tests
        drain_interval_seconds=1,
        max_drift_detectors=10,
    )


@pytest.fixture()
def mock_dragonfly():
    client = MagicMock()
    client.connect = MagicMock()
    client.disconnect = MagicMock()
    client.store_metric = MagicMock()
    client.get_metric_window = MagicMock(return_value=([1, 2, 3], [10.0, 11.0, 12.0]))
    return client


@pytest.fixture()
def mock_anomaly_servicer():
    servicer = MagicMock()
    # Return no anomalies by default
    servicer._detect_sync = MagicMock(
        return_value=DetectAnomaliesResponse(
            agent_id="a1",
            tenant_id="t1",
            metric_name="cpu",
            anomalies=[],
            overall_score=0.0,
        )
    )
    return servicer


@pytest.fixture()
def mock_forecasting_servicer():
    servicer = MagicMock()
    servicer._forecast_sync = MagicMock(
        return_value=ForecastMetricResponse(
            agent_id="a1",
            tenant_id="t1",
            metric_name="cpu",
            overall_confidence=0.85,
            model_info=ModelInfo(best_model="ensemble"),
        )
    )
    return servicer


@pytest.fixture()
def mock_producer():
    producer = MagicMock()
    producer.connect = MagicMock()
    producer.close = MagicMock()
    producer.flush = MagicMock()
    producer.publish_anomaly_tenant = MagicMock()
    producer.publish_forecast_tenant = MagicMock()
    producer.publish_drift = MagicMock()
    return producer


@pytest.fixture()
def pipeline(config, mock_dragonfly, mock_anomaly_servicer, mock_forecasting_servicer, mock_producer):
    from intelligence.pipeline.orchestrator import IntelligencePipeline

    p = IntelligencePipeline(
        anomaly_servicer=mock_anomaly_servicer,
        forecasting_servicer=mock_forecasting_servicer,
        dragonfly=mock_dragonfly,
        producer=mock_producer,
        config=config,
    )
    yield p
    # Cleanup
    if p.is_running:
        p.stop()


# =====================================================================
# MetricBuffer tests
# =====================================================================


class TestMetricBuffer:
    def test_below_threshold_returns_false(self):
        buf = MetricBuffer(min_points=5)
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a1", "cpu") is False

    def test_at_threshold_returns_true(self):
        buf = MetricBuffer(min_points=3)
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a1", "cpu") is True  # triggers

    def test_resets_after_trigger(self):
        buf = MetricBuffer(min_points=2)
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a1", "cpu") is True
        # Reset — next two should trigger again
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a1", "cpu") is True

    def test_separate_keys_independent(self):
        buf = MetricBuffer(min_points=2)
        assert buf.add("t1", "a1", "cpu") is False
        assert buf.add("t1", "a2", "mem") is False
        # Different keys, each at count 1
        assert buf.add("t1", "a1", "cpu") is True  # cpu triggers
        assert buf.add("t1", "a2", "mem") is False  # mem at 2 but different agent

    def test_forecast_time_gating(self):
        buf = MetricBuffer(min_points=3)
        assert buf.should_forecast("t1", "cpu", interval_seconds=1.0) is True  # never forecasted
        buf.mark_forecast("t1", "cpu")
        assert buf.should_forecast("t1", "cpu", interval_seconds=1.0) is False  # just forecasted
        time.sleep(1.1)
        assert buf.should_forecast("t1", "cpu", interval_seconds=1.0) is True  # interval elapsed

    def test_known_metrics_tracking(self):
        buf = MetricBuffer(min_points=100)
        buf.add("t1", "a1", "cpu")
        buf.add("t2", "a1", "mem")
        known = buf.get_known_metrics()
        assert ("t1", "cpu") in known
        assert ("t2", "mem") in known

    def test_active_metrics_count(self):
        buf = MetricBuffer(min_points=100)
        buf.add("t1", "a1", "cpu")
        buf.add("t1", "a1", "mem")
        assert buf.active_metrics == 2

    def test_thread_safety(self):
        buf = MetricBuffer(min_points=100)
        errors = []

        def add_many(prefix, count):
            try:
                for i in range(count):
                    buf.add(f"t{prefix}", f"a{i}", "cpu")
            except Exception as e:
                errors.append(e)

        threads = [threading.Thread(target=add_many, args=(i, 100)) for i in range(10)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        assert errors == []
        assert buf.active_metrics >= 10


# =====================================================================
# PipelineConfig tests
# =====================================================================


class TestPipelineConfig:
    def test_defaults(self):
        cfg = PipelineConfig()
        assert cfg.enabled is True  # always-on engine
        assert cfg.anomaly_min_points == 30
        assert cfg.forecast_interval_seconds == 900

    def test_from_env_overrides(self):
        with patch.dict("os.environ", {
            "PARYTY_INTEL_ENABLED": "true",
            "PARYTY_INTEL_ANOMALY_MIN_POINTS": "50",
            "PARYTY_INTEL_FORECAST_INTERVAL_SECONDS": "600",
        }):
            cfg = PipelineConfig.from_env()
            assert cfg.enabled is True
            assert cfg.anomaly_min_points == 50
            assert cfg.forecast_interval_seconds == 600

    def test_from_env_bool_parsing(self):
        for val in ("true", "1", "yes", "True", "YES"):
            with patch.dict("os.environ", {"PARYTY_INTEL_ENABLED": val}):
                assert PipelineConfig.from_env().enabled is True

        for val in ("false", "0", "no", "False"):
            with patch.dict("os.environ", {"PARYTY_INTEL_ENABLED": val}):
                assert PipelineConfig.from_env().enabled is False

    def test_frozen(self):
        cfg = PipelineConfig()
        with pytest.raises(AttributeError):
            cfg.enabled = True  # type: ignore[misc]


# =====================================================================
# IntelligencePipeline tests
# =====================================================================


class TestIntelligencePipeline:
    def test_disabled_pipeline_start_stop(self, config, mock_dragonfly, mock_anomaly_servicer,
                                          mock_forecasting_servicer, mock_producer):
        """Pipeline can start and stop cleanly."""
        from intelligence.pipeline.orchestrator import IntelligencePipeline

        cfg = PipelineConfig(enabled=True, retrain_enabled=False, drain_interval_seconds=1)
        p = IntelligencePipeline(
            mock_anomaly_servicer, mock_forecasting_servicer,
            mock_dragonfly, mock_producer, cfg,
        )
        p.start()
        assert p.is_running is True
        p.stop()
        assert p.is_running is False

    def test_on_metric_stores_in_dragonfly(self, pipeline, mock_dragonfly):
        """Each consumed metric is stored in Dragonfly."""
        pipeline.on_metric({
            "tenant_id": "t1",
            "agent_id": "a1",
            "metric_name": "cpu",
            "value": 75.0,
            "timestamp": 1000,
        })
        mock_dragonfly.store_metric.assert_called_once()

    def test_on_metric_triggers_anomaly_after_threshold(self, pipeline, mock_anomaly_servicer):
        """Anomaly detection is triggered after min_points metrics arrive."""
        # min_points is 3 from fixture
        for i in range(3):
            pipeline.on_metric({
                "tenant_id": "t1",
                "agent_id": "a1",
                "metric_name": "cpu",
                "value": float(i),
                "timestamp": 1000 + i,
            })
        # Give thread pool time to process
        time.sleep(0.5)
        mock_anomaly_servicer._detect_sync.assert_called()

    def test_on_metric_no_anomaly_below_threshold(self, pipeline, mock_anomaly_servicer):
        """No anomaly detection when below min_points."""
        for i in range(2):  # below 3
            pipeline.on_metric({
                "tenant_id": "t1",
                "agent_id": "a1",
                "metric_name": "cpu",
                "value": float(i),
                "timestamp": 1000 + i,
            })
        time.sleep(0.2)
        mock_anomaly_servicer._detect_sync.assert_not_called()

    def test_on_metric_publishes_anomalies(self, pipeline, mock_anomaly_servicer, mock_producer):
        """When anomalies are detected, they are published via producer."""
        # Make servicer return anomalies
        anomaly = AnomalyProto(
            timestamp=1000,
            value=99.0,
            score=0.9,
            type=AnomalyTypeProto.POINT,
            severity=SeverityProto.HIGH,
            explanation="Spike detected",
            detection_method="statistical",
        )
        mock_anomaly_servicer._detect_sync.return_value = DetectAnomaliesResponse(
            agent_id="a1",
            tenant_id="t1",
            metric_name="cpu",
            anomalies=[anomaly],
            overall_score=0.9,
        )

        for i in range(3):
            pipeline.on_metric({
                "tenant_id": "t1",
                "agent_id": "a1",
                "metric_name": "cpu",
                "value": 90.0 + i,
                "timestamp": 1000 + i,
            })
        time.sleep(0.5)
        mock_producer.publish_anomaly_tenant.assert_called()

    def test_on_metric_defaults_tenant_to_default(self, pipeline, mock_dragonfly):
        """Missing tenant_id defaults to 'default'."""
        pipeline.on_metric({
            "agent_id": "a1",
            "metric_name": "cpu",
            "value": 50.0,
        })
        call_args = mock_dragonfly.store_metric.call_args
        assert call_args.kwargs["tenant_id"] == "default"

    def test_on_metric_error_does_not_crash(self, pipeline, mock_dragonfly):
        """Errors in store_metric are caught — pipeline continues."""
        mock_dragonfly.store_metric.side_effect = RuntimeError("dragonfly down")
        # Should not raise
        pipeline.on_metric({
            "tenant_id": "t1",
            "agent_id": "a1",
            "metric_name": "cpu",
            "value": 50.0,
            "timestamp": 1000,
        })

    def test_shutdown_stops_cleanly(self, config, mock_dragonfly, mock_anomaly_servicer,
                                    mock_forecasting_servicer, mock_producer):
        """Pipeline stops all threads cleanly on shutdown."""
        from intelligence.pipeline.orchestrator import IntelligencePipeline

        cfg = PipelineConfig(enabled=True, retrain_enabled=False, drain_interval_seconds=1)
        p = IntelligencePipeline(
            mock_anomaly_servicer, mock_forecasting_servicer,
            mock_dragonfly, mock_producer, cfg,
        )
        p.start()
        time.sleep(0.5)
        p.stop()
        # Verify producer was flushed
        mock_producer.flush.assert_called()
