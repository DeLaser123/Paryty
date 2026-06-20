"""
Backtesting framework for forecasting models.

Provides walk-forward validation with sliding windows, computing
accuracy metrics (MAPE, RMSE, MAE) across historical data.

Usage:
    backtester = Backtester(config)
    report = backtester.run(timestamps, values, horizon_seconds=3600)
"""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Any

import numpy as np
import structlog

from .config import ForecastingConfig
from .ensemble import ForecastEnsemble
from .linear import ForecastResult

logger = structlog.get_logger(__name__)


@dataclass
class BacktestFoldResult:
    """Result for a single backtest fold."""

    fold_idx: int
    train_start: int
    train_end: int
    test_start: int
    test_end: int
    n_train: int
    n_test: int
    predictions: list[float]
    actuals: list[float]
    timestamps: list[int]
    mape: float
    rmse: float
    mae: float
    model_name: str


@dataclass
class BacktestReport:
    """Aggregated backtesting report."""

    folds: list[BacktestFoldResult] = field(default_factory=list)
    overall_mape: float = 0.0
    overall_rmse: float = 0.0
    overall_mae: float = 0.0
    per_fold_mape: list[float] = field(default_factory=list)
    per_fold_rmse: list[float] = field(default_factory=list)
    per_fold_mae: list[float] = field(default_factory=list)
    n_folds: int = 0
    total_test_points: int = 0
    run_duration_seconds: float = 0.0
    metadata: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        """Convert report to serializable dictionary."""
        return {
            "overall_mape": self.overall_mape,
            "overall_rmse": self.overall_rmse,
            "overall_mae": self.overall_mae,
            "per_fold_mape": self.per_fold_mape,
            "per_fold_rmse": self.per_fold_rmse,
            "per_fold_mae": self.per_fold_mae,
            "n_folds": self.n_folds,
            "total_test_points": self.total_test_points,
            "run_duration_seconds": self.run_duration_seconds,
            "metadata": self.metadata,
            "folds": [
                {
                    "fold_idx": f.fold_idx,
                    "train_range": [f.train_start, f.train_end],
                    "test_range": [f.test_start, f.test_end],
                    "n_train": f.n_train,
                    "n_test": f.n_test,
                    "mape": f.mape,
                    "rmse": f.rmse,
                    "mae": f.mae,
                    "model_name": f.model_name,
                }
                for f in self.folds
            ],
        }


class Backtester:
    """Walk-forward backtesting framework for forecasting models.

    Supports:
    - Fixed-size sliding window with configurable stride
    - Expanding window (train grows, test slides)
    - Multiple fold evaluation
    - Per-fold and aggregate metrics (MAPE, RMSE, MAE)
    """

    def __init__(self, config: ForecastingConfig | None = None) -> None:
        self._config = config or ForecastingConfig()

    def run(
        self,
        timestamps: list[int],
        values: list[float],
        horizon_seconds: int = 3600,
        step_seconds: int = 60,
        n_folds: int = 5,
        train_ratio: float = 0.7,
        expanding: bool = False,
        min_train_size: int = 100,
    ) -> BacktestReport:
        """Run walk-forward backtesting.

        Args:
            timestamps: Unix timestamps (ascending).
            values: Metric values.
            horizon_seconds: Forecast horizon for each fold.
            step_seconds: Step between forecast points.
            n_folds: Number of backtest folds.
            train_ratio: Fraction of data for initial training (used if not expanding).
            expanding: If True, use expanding window; otherwise sliding window.
            min_train_size: Minimum training samples required.

        Returns:
            BacktestReport with aggregated metrics.
        """
        start_time = time.time()
        n = len(timestamps)
        ts_arr = np.asarray(timestamps, dtype=np.int64)
        val_arr = np.asarray(values, dtype=np.float64)

        if n < min_train_size + 10:
            logger.warning("insufficient_data_for_backtesting", n=n, min_required=min_train_size + 10)
            return BacktestReport(
                metadata={"error": "insufficient_data", "n": n, "min_required": min_train_size + 10},
            )

        # Calculate fold boundaries
        folds = self._calculate_fold_boundaries(
            n=n,
            n_folds=n_folds,
            train_ratio=train_ratio,
            expanding=expanding,
            min_train_size=min_train_size,
        )

        fold_results: list[BacktestFoldResult] = []
        per_fold_mape: list[float] = []
        per_fold_rmse: list[float] = []
        per_fold_mae: list[float] = []

        for fold_idx, (train_end_idx, test_end_idx) in enumerate(folds):
            if expanding:
                train_start_idx = 0
            else:
                train_start_idx = max(0, train_end_idx - int(n * train_ratio))

            train_ts = ts_arr[train_start_idx:train_end_idx].tolist()
            train_vals = val_arr[train_start_idx:train_end_idx].tolist()
            test_ts = ts_arr[train_end_idx:test_end_idx].tolist()
            test_vals = val_arr[train_end_idx:test_end_idx].tolist()

            if len(train_ts) < min_train_size or len(test_ts) == 0:
                logger.warning(
                    "fold_skipped_insufficient_data",
                    fold_idx=fold_idx,
                    n_train=len(train_ts),
                    n_test=len(test_ts),
                )
                continue

            try:
                fold_result = self._evaluate_fold(
                    fold_idx=fold_idx,
                    train_ts=train_ts,
                    train_vals=train_vals,
                    test_ts=test_ts,
                    test_vals=test_vals,
                    horizon_seconds=horizon_seconds,
                    step_seconds=step_seconds,
                    train_start=int(ts_arr[train_start_idx]),
                    train_end=int(ts_arr[train_end_idx - 1]),
                    test_start=int(ts_arr[train_end_idx]),
                    test_end=int(ts_arr[min(test_end_idx - 1, n - 1)]),
                )
                fold_results.append(fold_result)
                per_fold_mape.append(fold_result.mape)
                per_fold_rmse.append(fold_result.rmse)
                per_fold_mae.append(fold_result.mae)
            except Exception as exc:
                logger.warning("fold_failed", fold_idx=fold_idx, error=str(exc))

        # Aggregate metrics
        overall_mape = float(np.mean(per_fold_mape)) if per_fold_mape else 100.0
        overall_rmse = float(np.mean(per_fold_rmse)) if per_fold_rmse else float("inf")
        overall_mae = float(np.mean(per_fold_mae)) if per_fold_mae else float("inf")
        total_test_points = sum(f.n_test for f in fold_results)

        run_duration = time.time() - start_time

        report = BacktestReport(
            folds=fold_results,
            overall_mape=overall_mape,
            overall_rmse=overall_rmse,
            overall_mae=overall_mae,
            per_fold_mape=per_fold_mape,
            per_fold_rmse=per_fold_rmse,
            per_fold_mae=per_fold_mae,
            n_folds=len(fold_results),
            total_test_points=total_test_points,
            run_duration_seconds=run_duration,
            metadata={
                "horizon_seconds": horizon_seconds,
                "step_seconds": step_seconds,
                "n_folds_requested": n_folds,
                "train_ratio": train_ratio,
                "expanding": expanding,
                "n_total": n,
            },
        )

        logger.info(
            "backtest_complete",
            n_folds=len(fold_results),
            overall_mape=overall_mape,
            overall_rmse=overall_rmse,
            duration=run_duration,
        )
        return report

    def _calculate_fold_boundaries(
        self,
        n: int,
        n_folds: int,
        train_ratio: float,
        expanding: bool,
        min_train_size: int,
    ) -> list[tuple[int, int]]:
        """Calculate (train_end_idx, test_end_idx) for each fold."""
        folds: list[tuple[int, int]] = []

        if expanding:
            # Expanding window: train grows, test slides
            test_size = max(10, (n - min_train_size) // n_folds)
            for i in range(n_folds):
                train_end = min_train_size + i * test_size
                test_end = min(n, train_end + test_size)
                if train_end >= n or test_end <= train_end:
                    break
                folds.append((train_end, test_end))
        else:
            # Sliding window: fixed train size, sliding test
            initial_train = int(n * train_ratio)
            remaining = n - initial_train
            test_size = max(10, remaining // n_folds)
            for i in range(n_folds):
                train_end = initial_train + i * test_size
                test_end = min(n, train_end + test_size)
                if train_end >= n or test_end <= train_end:
                    break
                folds.append((train_end, test_end))

        return folds

    def _evaluate_fold(
        self,
        fold_idx: int,
        train_ts: list[int],
        train_vals: list[float],
        test_ts: list[int],
        test_vals: list[float],
        horizon_seconds: int,
        step_seconds: int,
        train_start: int,
        train_end: int,
        test_start: int,
        test_end: int,
    ) -> BacktestFoldResult:
        """Evaluate a single backtest fold using the ensemble."""
        from ..data.timeseries import sanitize_series

        # Sanitize inputs
        clean_train_ts, clean_train_vals, _ = sanitize_series(train_ts, train_vals)
        clean_test_ts, clean_test_vals, _ = sanitize_series(test_ts, test_vals)

        if len(clean_train_ts) < 10 or len(clean_test_ts) == 0:
            raise ValueError(f"Insufficient clean data: train={len(clean_train_ts)}, test={len(clean_test_ts)}")

        # Generate predictions for test window using expanding window
        predictions: list[float] = []
        actuals: list[float] = []
        pred_timestamps: list[int] = []

        # Predict at each test point: train on (train + prior test points), predict next
        window_size = min(len(clean_train_ts), self._config.linear.window_size)
        for i in range(len(clean_test_ts)):
            try:
                # Combine training tail with test data up to current point
                context_ts = clean_train_ts[-window_size:] + clean_test_ts[:i]
                context_vals = clean_train_vals[-window_size:] + clean_test_vals[:i]

                if len(context_ts) < 3:
                    continue

                # Create temporary ensemble with context data
                temp_ensemble = ForecastEnsemble(config=self._config)
                temp_ensemble.fit_all(context_ts, context_vals)

                # Predict one step ahead
                result = temp_ensemble.predict(
                    horizon_seconds=step_seconds,
                    step_seconds=step_seconds,
                )

                if result.values:
                    predictions.append(result.values[0])
                    actuals.append(clean_test_vals[i])
                    pred_timestamps.append(clean_test_ts[i])
            except Exception:
                continue

        if not predictions:
            raise ValueError("No valid predictions generated")

        # Calculate metrics
        pred_arr = np.array(predictions, dtype=np.float64)
        actual_arr = np.array(actuals[:len(predictions)], dtype=np.float64)

        mape = self._compute_mape(actual_arr, pred_arr)
        rmse = self._compute_rmse(actual_arr, pred_arr)
        mae = self._compute_mae(actual_arr, pred_arr)

        return BacktestFoldResult(
            fold_idx=fold_idx,
            train_start=train_start,
            train_end=train_end,
            test_start=test_start,
            test_end=test_end,
            n_train=len(clean_train_ts),
            n_test=len(clean_test_ts),
            predictions=predictions,
            actuals=actuals[:len(predictions)],
            timestamps=pred_timestamps,
            mape=mape,
            rmse=rmse,
            mae=mae,
            model_name="ensemble",
        )

    @staticmethod
    def _compute_mape(actual: np.ndarray, predicted: np.ndarray) -> float:
        """Compute Mean Absolute Percentage Error."""
        denom = np.where(np.abs(actual) < 1e-9, 1e-9, np.abs(actual))
        return float(np.mean(np.abs(actual - predicted) / denom) * 100.0)

    @staticmethod
    def _compute_rmse(actual: np.ndarray, predicted: np.ndarray) -> float:
        """Compute Root Mean Squared Error."""
        return float(np.sqrt(np.mean((actual - predicted) ** 2)))

    @staticmethod
    def _compute_mae(actual: np.ndarray, predicted: np.ndarray) -> float:
        """Compute Mean Absolute Error."""
        return float(np.mean(np.abs(actual - predicted)))

    def quick_evaluate(
        self,
        timestamps: list[int],
        values: list[float],
        horizon_seconds: int = 3600,
    ) -> dict[str, float]:
        """Quick single-pass evaluation without full walk-forward.

        Useful for rapid model selection or parameter tuning.
        """
        from ..data.timeseries import sanitize_series

        clean_ts, clean_vals, _ = sanitize_series(timestamps, values)
        if len(clean_ts) < 100:
            return {"error": 100.0, "n": len(clean_ts)}

        # Simple 80/20 split
        split_idx = int(len(clean_ts) * 0.8)
        train_ts = clean_ts[:split_idx]
        train_vals = clean_vals[:split_idx]
        test_ts = clean_ts[split_idx:]
        test_vals = clean_vals[split_idx:]

        ensemble = ForecastEnsemble(config=self._config)
        ensemble.fit_all(train_ts, train_vals)

        # Generate predictions for test period
        result = ensemble.predict(
            horizon_seconds=horizon_seconds,
            step_seconds=60,
        )

        # Compute metrics on overlapping points
        n_compare = min(len(result.values), len(test_vals))
        if n_compare == 0:
            return {"error": 100.0, "n_compare": 0}

        pred = np.array(result.values[:n_compare], dtype=np.float64)
        actual = np.array(test_vals[:n_compare], dtype=np.float64)

        return {
            "mape": self._compute_mape(actual, pred),
            "rmse": self._compute_rmse(actual, pred),
            "mae": self._compute_mae(actual, pred),
            "n_compare": n_compare,
        }
