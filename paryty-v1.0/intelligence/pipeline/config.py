"""
Pipeline configuration with feature flags and env-var overrides.

The engine is always-on by default. Individual sub-features can be
disabled via environment variables with the ``PARYTY_INTEL_`` prefix.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass(frozen=True)
class PipelineConfig:
    """Configuration for the Intelligence Pipeline.

    Every field can be overridden via environment variable with the
    ``PARYTY_INTEL_`` prefix (e.g. ``PARYTY_INTEL_ENABLED=true``).
    Boolean values accept ``true/false/1/0/yes/no`` (case-insensitive).
    """

    # ── Feature flags (engine is always-on; sub-features individually tunable) ──
    enabled: bool = True
    anomaly_enabled: bool = True
    forecast_enabled: bool = True
    drift_enabled: bool = True
    retrain_enabled: bool = True

    # ── Anomaly detection tuning ───────────────────────────────────
    anomaly_window_minutes: int = 30
    anomaly_min_points: int = 30

    # ── Forecast tuning ────────────────────────────────────────────
    forecast_interval_seconds: int = 900      # 15 min between forecasts per metric
    forecast_horizon_seconds: int = 86400     # 24-hour forecast horizon
    forecast_step_seconds: int = 300          # 5-min step between forecast points

    # ── Pipeline tuning ────────────────────────────────────────────
    drain_interval_seconds: int = 30          # drain thread wake-up interval
    max_drift_detectors: int = 500            # LRU cap for drift detectors
    retrain_cooldown_seconds: int = 3600      # 1h between retrains per metric
    pipeline_workers: int = 4                 # thread pool size for ML work

    # ── Topic naming (matches Go: paryty.<tenant>.<suffix>) ───────
    input_topic_pattern: str = r"paryty\..*\.metrics\.raw"
    output_anomalies_suffix: str = "anomalies"
    output_forecasts_suffix: str = "forecasts"
    output_drift_suffix: str = "drift"

    # ── Env prefix for overrides ───────────────────────────────────
    _ENV_PREFIX: str = field(default="PARYTY_INTEL_", repr=False)

    # ------------------------------------------------------------------
    # Factory
    # ------------------------------------------------------------------

    @classmethod
    def from_env(cls) -> PipelineConfig:
        """Build a :class:`PipelineConfig` from environment variables.

        Only fields whose corresponding env var is set are overridden.
        """
        overrides: dict = {}
        prefix = "PARYTY_INTEL_"

        for fld in cls.__dataclass_fields__.values():
            if fld.name.startswith("_"):
                continue
            env_key = f"{prefix}{fld.name.upper()}"
            raw = os.environ.get(env_key)
            if raw is None:
                continue
            target_type = fld.type
            # Resolve forward-ref strings
            if isinstance(target_type, str):
                target_type = eval(target_type)  # noqa: S307 – controlled eval
            if target_type is bool:
                overrides[fld.name] = raw.strip().lower() in ("true", "1", "yes")
            elif target_type is int:
                overrides[fld.name] = int(raw)
            elif target_type is float:
                overrides[fld.name] = float(raw)
            else:
                overrides[fld.name] = raw

        return cls(**overrides)
