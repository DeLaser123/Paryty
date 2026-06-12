"""
File-based model persistence for forecasting models.

Uses joblib for efficient serialisation of scikit-learn, Prophet, and
XGBoost models.  Versioned filenames prevent accidental overwrites.

Directory layout::

    models/forecasting/{model_name}_{metric_name}.joblib
"""

from __future__ import annotations

import os
import time
from pathlib import Path
from typing import Any

import joblib
import structlog

from ..security import sanitize_filename_component

logger = structlog.get_logger(__name__)


class ForecastModelStore:
    """Manage persisted forecasting model artifacts on disk.

    Each model is saved as ``{model_name}_{metric_name}.joblib``.
    Older versions are kept with an incrementing suffix.
    """

    def __init__(self, base_dir: str | Path = "models/forecasting") -> None:
        self._base_dir = Path(base_dir)
        self._base_dir.mkdir(parents=True, exist_ok=True)

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def save(
        self,
        model_name: str,
        metric_name: str,
        artifact: Any,
    ) -> Path:
        """Persist a model artifact to disk.

        Args:
            model_name: Logical name (e.g. "linear", "prophet", "xgboost").
            metric_name: Metric this model was trained on (e.g. "cpu_usage").
            artifact: Any picklable object (model state, config, etc.).

        Returns:
            Path to the saved file.
        """
        filename = self._filename(model_name, metric_name)
        filepath = self._base_dir / filename

        # If existing, rotate to backup
        if filepath.exists():
            version = self._next_version(model_name, metric_name)
            backup = self._base_dir / self._versioned_filename(
                model_name, metric_name, version
            )
            filepath.rename(backup)
            logger.debug(
                "model_rotated",
                model=model_name,
                metric=metric_name,
                backup=str(backup),
            )

        joblib.dump(artifact, filepath)
        logger.info(
            "model_saved",
            model=model_name,
            metric=metric_name,
            path=str(filepath),
        )
        return filepath

    def load(self, model_name: str, metric_name: str) -> Any | None:
        """Load a model artifact from disk.

        Returns None if the file does not exist.
        """
        filepath = self._base_dir / self._filename(model_name, metric_name)
        if not filepath.exists():
            return None
        artifact = joblib.load(filepath)
        logger.debug(
            "model_loaded",
            model=model_name,
            metric=metric_name,
            path=str(filepath),
        )
        return artifact

    def exists(self, model_name: str, metric_name: str) -> bool:
        """Check whether a persisted model exists."""
        filepath = self._base_dir / self._filename(model_name, metric_name)
        return filepath.exists()

    def get_training_age(self, model_name: str, metric_name: str) -> float:
        """Return seconds since the model file was last modified.

        Returns ``float("inf")`` if the model does not exist.
        """
        filepath = self._base_dir / self._filename(model_name, metric_name)
        if not filepath.exists():
            return float("inf")
        mtime = filepath.stat().st_mtime
        return time.time() - mtime

    def list_models(self) -> list[dict[str, Any]]:
        """List all persisted models with metadata.

        Returns a list of dicts with keys: model_name, metric_name,
        path, size_bytes, age_seconds.
        """
        models: list[dict[str, Any]] = []
        for p in sorted(self._base_dir.glob("*.joblib")):
            name = p.stem
            parts = name.split("_", 1)
            model_name = parts[0] if parts else name
            metric_name = parts[1] if len(parts) > 1 else ""
            age = time.time() - p.stat().st_mtime
            models.append(
                {
                    "model_name": model_name,
                    "metric_name": metric_name,
                    "path": str(p),
                    "size_bytes": p.stat().st_size,
                    "age_seconds": age,
                }
            )
        return models

    def delete(self, model_name: str, metric_name: str) -> bool:
        """Delete a persisted model file.  Returns True if deleted."""
        filepath = self._base_dir / self._filename(model_name, metric_name)
        if filepath.exists():
            filepath.unlink()
            logger.info(
                "model_deleted",
                model=model_name,
                metric=metric_name,
            )
            return True
        return False

    # ------------------------------------------------------------------
    # Internal helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _filename(model_name: str, metric_name: str) -> str:
        # metric_name arrives from the network (RPC request); sanitize both
        # components so crafted names cannot escape the model directory.
        return (
            f"{sanitize_filename_component(model_name)}_"
            f"{sanitize_filename_component(metric_name)}.joblib"
        )

    @staticmethod
    def _versioned_filename(
        model_name: str, metric_name: str, version: int
    ) -> str:
        return (
            f"{sanitize_filename_component(model_name)}_"
            f"{sanitize_filename_component(metric_name)}_v{version}.joblib"
        )

    def _next_version(self, model_name: str, metric_name: str) -> int:
        pattern = (
            f"{sanitize_filename_component(model_name)}_"
            f"{sanitize_filename_component(metric_name)}_v*.joblib"
        )
        existing = list(self._base_dir.glob(pattern))
        versions: list[int] = []
        for p in existing:
            stem = p.stem  # e.g. "linear_cpu_usage_v3"
            parts = stem.split("_v")
            if len(parts) == 2:
                try:
                    versions.append(int(parts[-1]))
                except ValueError:
                    pass
        return max(versions, default=0) + 1
