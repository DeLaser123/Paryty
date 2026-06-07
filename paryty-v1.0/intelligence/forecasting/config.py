"""
Forecasting engine configuration.

Loads from environment variables with sensible defaults.
Uses Pydantic for validation and type safety.
"""

from __future__ import annotations

import os
from pathlib import Path

from pydantic import BaseModel, Field


class LinearConfig(BaseModel):
    """Configuration for the Linear Regression forecaster."""

    window_size: int = Field(
        default=60,
        ge=3,
        description="Rolling window size in data points",
    )
    confidence_level: float = Field(
        default=0.95,
        ge=0.5,
        le=0.999,
        description="Confidence level for prediction intervals",
    )
    min_data_points: int = Field(default=3, ge=3)


class ProphetConfig(BaseModel):
    """Configuration for the Prophet forecaster."""

    daily_seasonality: bool = Field(default=True)
    weekly_seasonality: bool = Field(default=True)
    yearly_seasonality: bool = Field(default=False)
    changepoint_prior_scale: float = Field(
        default=0.05,
        ge=0.001,
        le=0.5,
        description="Regularization for changepoint detection",
    )
    confidence_level: float = Field(default=0.95, ge=0.5, le=0.999)
    min_data_points: int = Field(default=1440, ge=1440)


class XGBoostConfig(BaseModel):
    """Configuration for the XGBoost forecaster."""

    n_estimators: int = Field(default=100, ge=10, le=1000)
    max_depth: int = Field(default=6, ge=2, le=12)
    learning_rate: float = Field(default=0.1, ge=0.01, le=0.5)
    lag_features: list[int] = Field(
        default=[1, 5, 15, 60],
        description="Lag values to use as features",
    )
    rolling_windows: list[int] = Field(
        default=[5, 15, 60],
        description="Rolling statistic window sizes",
    )
    min_data_points: int = Field(default=1000, ge=100)
    confidence_level: float = Field(default=0.95, ge=0.5, le=0.999)


class EnsembleConfig(BaseModel):
    """Configuration for the weighted ensemble."""

    accuracy_history_size: int = Field(
        default=168,
        ge=24,
        description="Number of hourly MAPE entries to retain (168 = 7 days)",
    )
    min_weight: float = Field(default=0.1, ge=0.0, le=0.5)
    max_weight: float = Field(default=0.6, ge=0.3, le=1.0)
    default_weights: dict[str, float] = Field(
        default={"linear": 0.33, "prophet": 0.34, "xgboost": 0.33},
    )


class ModelStoreConfig(BaseModel):
    """Configuration for model persistence."""

    base_dir: Path = Field(
        default=Path("models/forecasting"),
        description="Base directory for model artifacts",
    )


class ForecastingConfig(BaseModel):
    """Top-level configuration for the forecasting engine."""

    linear: LinearConfig = Field(default_factory=LinearConfig)
    prophet: ProphetConfig = Field(default_factory=ProphetConfig)
    xgboost: XGBoostConfig = Field(default_factory=XGBoostConfig)
    ensemble: EnsembleConfig = Field(default_factory=EnsembleConfig)
    model_store: ModelStoreConfig = Field(default_factory=ModelStoreConfig)

    default_horizon_seconds: int = Field(
        default=604_800,
        ge=60,
        description="Default forecast horizon (7 days)",
    )
    default_step_seconds: int = Field(
        default=300,
        ge=10,
        description="Default step between forecast points (5 min)",
    )
    default_confidence_level: int = Field(default=95, ge=80, le=99)

    @classmethod
    def from_env(cls) -> ForecastingConfig:
        """Build configuration from environment variables with defaults."""
        return cls(
            linear=LinearConfig(
                window_size=int(os.getenv("FORECAST_LINEAR_WINDOW", "60")),
                confidence_level=float(
                    os.getenv("FORECAST_LINEAR_CONFIDENCE", "0.95")
                ),
            ),
            prophet=ProphetConfig(
                changepoint_prior_scale=float(
                    os.getenv("FORECAST_PROPRIET_CHANGEPOINT_SCALE", "0.05")
                ),
            ),
            xgboost=XGBoostConfig(
                n_estimators=int(os.getenv("FORECAST_XGB_N_ESTIMATORS", "100")),
                max_depth=int(os.getenv("FORECAST_XGB_MAX_DEPTH", "6")),
                learning_rate=float(
                    os.getenv("FORECAST_XGB_LEARNING_RATE", "0.1")
                ),
            ),
            ensemble=EnsembleConfig(
                min_weight=float(os.getenv("FORECAST_ENSEMBLE_MIN_WEIGHT", "0.1")),
                max_weight=float(os.getenv("FORECAST_ENSEMBLE_MAX_WEIGHT", "0.6")),
            ),
            model_store=ModelStoreConfig(
                base_dir=Path(
                    os.getenv(
                        "FORECAST_MODEL_DIR", "models/forecasting"
                    )
                ),
            ),
            default_horizon_seconds=int(
                os.getenv("FORECAST_DEFAULT_HORIZON", "604800")
            ),
            default_step_seconds=int(
                os.getenv("FORECAST_DEFAULT_STEP", "300")
            ),
        )
