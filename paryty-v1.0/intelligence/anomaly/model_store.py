"""
File-based model persistence for anomaly detection models.

Uses joblib for serialisation.  Stores Isolation Forest models and
Autoencoder weights.  Statistical detectors are stateless and do not
require persistence.

Directory layout::

    models/anomaly/{model_name}_{metric_name}.joblib
"""

from __future__ import annotations

import time
from pathlib import Path
from typing import Any

import joblib
import structlog

from ..security import sanitize_filename_component

logger = structlog.get_logger(__name__)


class AnomalyModelStore:
    """Manage persisted anomaly detection model artifacts on disk.

    Supports:
        * Isolation Forest (sklearn model)
        * Autoencoder weights and config
        * Scaler parameters (StandardScaler)
        * Ensemble configuration
    """

    def __init__(self, base_dir: str | Path = "models/anomaly") -> None:
        self._base_dir = Path(base_dir)
        self._base_dir.mkdir(parents=True, exist_ok=True)

    def save(
        self,
        model_name: str,
        metric_name: str,
        artifact: Any,
    ) -> Path:
        """Persist a model artifact.

        Args:
            model_name: Logical name (e.g. "isolation_forest", "autoencoder").
            metric_name: Metric this model was trained on.
            artifact: Any picklable object.

        Returns:
            Path to the saved file.
        """
        filename = self._filename(model_name, metric_name)
        filepath = self._base_dir / filename

        if filepath.exists():
            version = self._next_version(model_name, metric_name)
            backup = self._base_dir / self._versioned_filename(
                model_name, metric_name, version
            )
            filepath.rename(backup)
            logger.debug(
                "anomaly_model_rotated",
                model=model_name,
                metric=metric_name,
                backup=str(backup),
            )

        joblib.dump(artifact, filepath)
        logger.info(
            "anomaly_model_saved",
            model=model_name,
            metric=metric_name,
            path=str(filepath),
        )
        return filepath

    def load(self, model_name: str, metric_name: str) -> Any | None:
        """Load a model artifact.  Returns None if not found."""
        filepath = self._base_dir / self._filename(model_name, metric_name)
        if not filepath.exists():
            return None
        artifact = joblib.load(filepath)
        logger.debug(
            "anomaly_model_loaded",
            model=model_name,
            metric=metric_name,
        )
        return artifact

    def exists(self, model_name: str, metric_name: str) -> bool:
        """Check whether a persisted model exists."""
        return (self._base_dir / self._filename(model_name, metric_name)).exists()

    def get_training_age(self, model_name: str, metric_name: str) -> float:
        """Return seconds since the model file was last modified.

        Returns ``float("inf")`` if the model does not exist.
        """
        filepath = self._base_dir / self._filename(model_name, metric_name)
        if not filepath.exists():
            return float("inf")
        return time.time() - filepath.stat().st_mtime

    def list_models(self) -> list[dict[str, Any]]:
        """List all persisted anomaly models with metadata."""
        models: list[dict[str, Any]] = []
        for p in sorted(self._base_dir.glob("*.joblib")):
            name = p.stem
            parts = name.split("_", 1)
            model_name = parts[0] if parts else name
            metric_name = parts[1] if len(parts) > 1 else ""
            models.append(
                {
                    "model_name": model_name,
                    "metric_name": metric_name,
                    "path": str(p),
                    "size_bytes": p.stat().st_size,
                    "age_seconds": time.time() - p.stat().st_mtime,
                }
            )
        return models

    def delete(self, model_name: str, metric_name: str) -> bool:
        """Delete a persisted model.  Returns True if deleted."""
        filepath = self._base_dir / self._filename(model_name, metric_name)
        if filepath.exists():
            filepath.unlink()
            logger.info(
                "anomaly_model_deleted",
                model=model_name,
                metric=metric_name,
            )
            return True
        return False

    # ------------------------------------------------------------------
    # Internal
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
            stem = p.stem
            parts = stem.split("_v")
            if len(parts) == 2:
                try:
                    versions.append(int(parts[-1]))
                except ValueError:
                    pass
        return max(versions, default=0) + 1
