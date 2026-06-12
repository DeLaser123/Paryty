"""
Regression tests for tenant isolation in the intelligence gRPC services.

Pins the fix for the cross-tenant information leak: an empty ``tenant_id``
in GetModelAccuracy / GetDetectionStatus previously acted as a wildcard
that returned EVERY tenant's model metadata (metric names, accuracies,
training timestamps — all infrastructure-revealing).
"""

from __future__ import annotations

import pytest

from intelligence.proto.models import (
    GetDetectionStatusRequest,
    GetModelAccuracyRequest,
)
from intelligence.server.anomaly_service import AnomalyServicer
from intelligence.server.forecasting_service import ForecastingServicer


class _FakeEnsemble:
    """Minimal stand-in exposing get_accuracy like ForecastEnsemble."""

    def get_accuracy(self) -> dict:
        return {
            "weights": {"linear": 1.0},
            "mapes": {"linear": 0.1},
            "last_fit_ts": 123,
            "training_points": 10,
        }


@pytest.fixture()
def forecasting_with_two_tenants(tmp_path) -> ForecastingServicer:
    servicer = ForecastingServicer.__new__(ForecastingServicer)
    servicer._ensembles = {
        ("tenant-a", "cpu.usage"): _FakeEnsemble(),
        ("tenant-b", "secret.metric"): _FakeEnsemble(),
    }
    return servicer


@pytest.fixture()
def anomaly_with_two_tenants() -> AnomalyServicer:
    class _FakeDetector:
        is_fitted = True

    servicer = AnomalyServicer.__new__(AnomalyServicer)
    servicer._if_detectors = {
        ("tenant-a", "cpu.usage"): _FakeDetector(),
        ("tenant-b", "secret.metric"): _FakeDetector(),
    }
    servicer._ae_detectors = {
        ("tenant-b", "secret.metric"): _FakeDetector(),
    }
    servicer._anomalies_detected_24h = 0
    servicer._last_detection_time = 0.0
    servicer._false_positive_rate = 0.05
    return servicer


@pytest.mark.asyncio
async def test_model_accuracy_scoped_to_tenant(forecasting_with_two_tenants):
    resp = await forecasting_with_two_tenants.GetModelAccuracy(
        GetModelAccuracyRequest(tenant_id="tenant-a", metric_name="")
    )
    assert "cpu.usage" in resp.models
    assert "secret.metric" not in resp.models, (
        "tenant-b's model must never be visible to tenant-a"
    )


@pytest.mark.asyncio
async def test_model_accuracy_empty_tenant_returns_nothing(forecasting_with_two_tenants):
    # Regression: empty tenant_id used to be a wildcard over ALL tenants.
    resp = await forecasting_with_two_tenants.GetModelAccuracy(
        GetModelAccuracyRequest(tenant_id="", metric_name="")
    )
    assert resp.models == {}, "empty tenant_id must not leak any tenant's models"


@pytest.mark.asyncio
async def test_detection_status_scoped_to_tenant(anomaly_with_two_tenants):
    resp = await anomaly_with_two_tenants.GetDetectionStatus(
        GetDetectionStatusRequest(tenant_id="tenant-a")
    )
    model_names = set(resp.models.keys())
    assert "isolation_forest_cpu.usage" in model_names
    assert not any("secret.metric" in n for n in model_names), (
        "tenant-b's metric names must never be visible to tenant-a"
    )


@pytest.mark.asyncio
async def test_detection_status_empty_tenant_returns_nothing(anomaly_with_two_tenants):
    resp = await anomaly_with_two_tenants.GetDetectionStatus(
        GetDetectionStatusRequest(tenant_id="")
    )
    assert resp.models == {}, "empty tenant_id must not leak any tenant's models"
