"""
Anomaly detection engine configuration.

Loads from environment variables with sensible defaults.
Uses Pydantic for validation and type safety.
"""

from __future__ import annotations

import os
from pathlib import Path

from pydantic import BaseModel, Field


class ZScoreConfig(BaseModel):
    """Configuration for the Z-Score detector."""

    threshold: float = Field(
        default=3.0,
        ge=1.0,
        le=6.0,
        description="Number of standard deviations for anomaly",
    )
    window_size: int = Field(
        default=60,
        ge=10,
        description="Rolling window for mean/std computation",
    )
    min_data_points: int = Field(default=10, ge=3)


class IQRConfig(BaseModel):
    """Configuration for the IQR detector."""

    multiplier: float = Field(
        default=1.5,
        ge=0.5,
        le=5.0,
        description="IQR multiplier for bounds (1.5 = standard, 3.0 = extreme)",
    )
    window_size: int = Field(
        default=100,
        ge=20,
        description="Rolling window for Q1/Q3 computation",
    )


class EWMAConfig(BaseModel):
    """Configuration for the EWMA detector."""

    alpha: float = Field(
        default=0.3,
        ge=0.01,
        le=1.0,
        description="Smoothing factor (higher = more responsive)",
    )
    threshold: float = Field(
        default=3.0,
        ge=1.0,
        le=6.0,
        description="Number of EWMA std deviations for anomaly",
    )
    min_data_points: int = Field(default=10, ge=3)


class IsolationForestConfig(BaseModel):
    """Configuration for the Isolation Forest detector."""

    n_estimators: int = Field(default=100, ge=10, le=500)
    contamination: float = Field(
        default=0.05,
        ge=0.001,
        le=0.5,
        description="Expected proportion of anomalies",
    )
    max_samples: int = Field(
        default=256,
        ge=32,
        le=2048,
        description="Max samples per tree",
    )
    min_data_points: int = Field(default=100, ge=50)


class AutoencoderConfig(BaseModel):
    """Configuration for the Autoencoder detector."""

    encoding_dim: int = Field(default=16, ge=4, le=64)
    hidden_layers: list[int] = Field(
        default=[64, 32],
        description="Encoder hidden layer sizes",
    )
    epochs: int = Field(default=50, ge=10, le=200)
    batch_size: int = Field(default=32, ge=8, le=256)
    threshold_percentile: float = Field(
        default=95.0,
        ge=80.0,
        le=99.9,
        description="Reconstruction error percentile for anomaly threshold",
    )
    min_data_points: int = Field(default=200, ge=100)


class EnsembleConfig(BaseModel):
    """Configuration for the anomaly detection ensemble."""

    weights: dict[str, float] = Field(
        default={
            "statistical": 0.30,
            "isolation_forest": 0.35,
            "autoencoder": 0.35,
        },
    )
    vote_threshold: float = Field(
        default=0.5,
        ge=0.0,
        le=1.0,
        description="Minimum weighted vote fraction to flag anomaly",
    )


class ModelStoreConfig(BaseModel):
    """Configuration for anomaly model persistence."""

    base_dir: Path = Field(
        default=Path("models/anomaly"),
        description="Base directory for anomaly model artifacts",
    )


class AnomalyConfig(BaseModel):
    """Top-level configuration for anomaly detection."""

    zscore: ZScoreConfig = Field(default_factory=ZScoreConfig)
    iqr: IQRConfig = Field(default_factory=IQRConfig)
    ewma: EWMAConfig = Field(default_factory=EWMAConfig)
    isolation_forest: IsolationForestConfig = Field(
        default_factory=IsolationForestConfig
    )
    autoencoder: AutoencoderConfig = Field(default_factory=AutoencoderConfig)
    ensemble: EnsembleConfig = Field(default_factory=EnsembleConfig)
    model_store: ModelStoreConfig = Field(default_factory=ModelStoreConfig)

    default_sensitivity: float = Field(
        default=0.5,
        ge=0.0,
        le=1.0,
        description="Default detection sensitivity",
    )

    @classmethod
    def from_env(cls) -> AnomalyConfig:
        """Build configuration from environment variables."""
        return cls(
            zscore=ZScoreConfig(
                threshold=float(os.getenv("ANOMALY_ZSCORE_THRESHOLD", "3.0")),
                window_size=int(os.getenv("ANOMALY_ZSCORE_WINDOW", "60")),
            ),
            iqr=IQRConfig(
                multiplier=float(os.getenv("ANOMALY_IQR_MULTIPLIER", "1.5")),
            ),
            ewma=EWMAConfig(
                alpha=float(os.getenv("ANOMALY_EWMA_ALPHA", "0.3")),
                threshold=float(os.getenv("ANOMALY_EWMA_THRESHOLD", "3.0")),
            ),
            isolation_forest=IsolationForestConfig(
                n_estimators=int(
                    os.getenv("ANOMALY_IF_N_ESTIMATORS", "100")
                ),
                contamination=float(
                    os.getenv("ANOMALY_IF_CONTAMINATION", "0.05")
                ),
            ),
            autoencoder=AutoencoderConfig(
                epochs=int(os.getenv("ANOMALY_AE_EPOCHS", "50")),
                threshold_percentile=float(
                    os.getenv("ANOMALY_AE_THRESHOLD_PCT", "95.0")
                ),
            ),
            ensemble=EnsembleConfig(
                vote_threshold=float(
                    os.getenv("ANOMALY_ENSEMBLE_VOTE_THRESHOLD", "0.5")
                ),
            ),
            model_store=ModelStoreConfig(
                base_dir=Path(os.getenv("ANOMALY_MODEL_DIR", "models/anomaly")),
            ),
            default_sensitivity=float(
                os.getenv("ANOMALY_DEFAULT_SENSITIVITY", "0.5")
            ),
        )
