"""
Autoencoder anomaly detector.

Uses a neural network autoencoder to learn the normal pattern of metric
data.  Points with high reconstruction error are flagged as anomalies.

ALGORITHM:
    1. Build sliding windows of features (lag values, rolling stats, time).
    2. Train autoencoder: Input → 64 → 32 → 16 → 32 → 64 → Output.
    3. Compute reconstruction error per point.
    4. Threshold at the 95th percentile of training reconstruction errors.
    5. Points above the threshold are anomalies.

Detects:
    * Contextual anomalies (e.g. memory leaks): gradual deviation from normal.
    * Collective anomalies (e.g. cascading failures): patterns spanning
      multiple consecutive points.

PERFORMANCE: <50ms per 1000 points (inference only).
MIN DATA: 200 points.

NOTE: Uses numpy-only implementation to avoid TensorFlow dependency issues
in production.  The autoencoder is implemented with pure numpy for portability
and reduced dependency footprint.
"""

from __future__ import annotations

import time as _time
from typing import Sequence

import numpy as np
import structlog

from .config import AnomalyConfig, AutoencoderConfig
from .statistical import AnomalyResult, AnomalyType, Severity, _severity_from_score

logger = structlog.get_logger(__name__)


# ---------------------------------------------------------------------------
# Simple numpy autoencoder
# ---------------------------------------------------------------------------


class _NumpyAutoencoder:
    """Lightweight autoencoder implemented with numpy.

    Architecture:
        Encoder: input_dim → 64 → 32 → encoding_dim
        Decoder: encoding_dim → 32 → 64 → input_dim

    Uses ReLU activations and Adam optimiser.
    """

    def __init__(
        self,
        input_dim: int,
        encoding_dim: int = 16,
        hidden_layers: list[int] | None = None,
    ) -> None:
        if hidden_layers is None:
            hidden_layers = [64, 32]
        self.input_dim = input_dim
        self.encoding_dim = encoding_dim
        self.hidden_layers = hidden_layers

        # Build layer dimensions
        self._enc_dims = [input_dim] + hidden_layers + [encoding_dim]
        self._dec_dims = [encoding_dim] + list(reversed(hidden_layers)) + [input_dim]

        # Initialise weights (He initialisation)
        rng = np.random.default_rng(42)
        self._enc_weights: list[np.ndarray] = []
        self._enc_biases: list[np.ndarray] = []
        for i in range(len(self._enc_dims) - 1):
            fan_in = self._enc_dims[i]
            fan_out = self._enc_dims[i + 1]
            std = np.sqrt(2.0 / fan_in)
            self._enc_weights.append(rng.standard_normal((fan_in, fan_out)) * std)
            self._enc_biases.append(np.zeros(fan_out))

        self._dec_weights: list[np.ndarray] = []
        self._dec_biases: list[np.ndarray] = []
        for i in range(len(self._dec_dims) - 1):
            fan_in = self._dec_dims[i]
            fan_out = self._dec_dims[i + 1]
            std = np.sqrt(2.0 / fan_in)
            self._dec_weights.append(rng.standard_normal((fan_in, fan_out)) * std)
            self._dec_biases.append(np.zeros(fan_out))

    @staticmethod
    def _relu(x: np.ndarray) -> np.ndarray:
        return np.maximum(0, x)

    @staticmethod
    def _relu_grad(x: np.ndarray) -> np.ndarray:
        return (x > 0).astype(np.float64)

    def _forward(self, x: np.ndarray) -> tuple[list[np.ndarray], list[np.ndarray]]:
        """Forward pass. Returns (all_activations, all_pre_activations)."""
        activations = [x]
        pre_acts = [x]

        # Encoder
        h = x
        for i, (w, b) in enumerate(zip(self._enc_weights, self._enc_biases)):
            z = h @ w + b
            pre_acts.append(z)
            if i < len(self._enc_weights) - 1:
                h = self._relu(z)
            else:
                h = z  # linear encoding
            activations.append(h)

        # Decoder
        for i, (w, b) in enumerate(zip(self._dec_weights, self._dec_biases)):
            z = h @ w + b
            pre_acts.append(z)
            if i < len(self._dec_weights) - 1:
                h = self._relu(z)
            else:
                h = z  # linear output
            activations.append(h)

        return activations, pre_acts

    def _backward(
        self,
        activations: list[np.ndarray],
        pre_acts: list[np.ndarray],
        x: np.ndarray,
    ) -> tuple[list[np.ndarray], list[np.ndarray], list[np.ndarray], list[np.ndarray]]:
        """Backward pass. Returns (enc_grads_w, enc_grads_b, dec_grads_w, dec_grads_b)."""
        n = x.shape[0]
        output = activations[-1]
        delta = 2.0 * (output - x) / n  # MSE gradient

        dec_gw: list[np.ndarray] = []
        dec_gb: list[np.ndarray] = []

        # Decoder gradients (reverse order)
        dec_start = len(self._enc_weights) + 1
        for i in range(len(self._dec_weights) - 1, -1, -1):
            a = activations[dec_start + i]
            gw = a.T @ delta
            gb = np.sum(delta, axis=0)
            dec_gw.insert(0, gw)
            dec_gb.insert(0, gb)

            if i > 0:
                delta = delta @ self._dec_weights[i].T
                delta *= self._relu_grad(pre_acts[dec_start + i])

        # Continue through encoder
        # delta is now at the encoding layer
        enc_gw: list[np.ndarray] = []
        enc_gb: list[np.ndarray] = []

        for i in range(len(self._enc_weights) - 1, -1, -1):
            a = activations[i]
            if i < len(self._enc_weights) - 1:
                gw = a.T @ delta
                gb = np.sum(delta, axis=0)
            else:
                gw = a.T @ delta
                gb = np.sum(delta, axis=0)
            enc_gw.insert(0, gw)
            enc_gb.insert(0, gb)

            if i > 0:
                delta = delta @ self._enc_weights[i].T
                delta *= self._relu_grad(pre_acts[i])

        return enc_gw, enc_gb, dec_gw, dec_gb

    def fit(
        self,
        X: np.ndarray,
        epochs: int = 50,
        batch_size: int = 32,
        learning_rate: float = 0.001,
    ) -> list[float]:
        """Train the autoencoder.

        Args:
            X: Training data, shape (n_samples, input_dim).
            epochs: Number of training epochs.
            batch_size: Mini-batch size.
            learning_rate: Adam learning rate.

        Returns:
            List of per-epoch MSE losses.
        """
        n = X.shape[0]
        rng = np.random.default_rng(42)
        losses: list[float] = []

        # Adam state
        beta1, beta2, eps = 0.9, 0.999, 1e-8
        m_enc_w = [np.zeros_like(w) for w in self._enc_weights]
        v_enc_w = [np.zeros_like(w) for w in self._enc_weights]
        m_enc_b = [np.zeros_like(b) for b in self._enc_biases]
        v_enc_b = [np.zeros_like(b) for b in self._enc_biases]
        m_dec_w = [np.zeros_like(w) for w in self._dec_weights]
        v_dec_w = [np.zeros_like(w) for w in self._dec_weights]
        m_dec_b = [np.zeros_like(b) for b in self._dec_biases]
        v_dec_b = [np.zeros_like(b) for b in self._dec_biases]
        t_step = 0

        for epoch in range(epochs):
            indices = rng.permutation(n)
            epoch_loss = 0.0
            n_batches = 0

            for start in range(0, n, batch_size):
                batch_idx = indices[start : start + batch_size]
                x_batch = X[batch_idx]

                # Forward
                activations, pre_acts = self._forward(x_batch)
                output = activations[-1]
                loss = np.mean((x_batch - output) ** 2)
                epoch_loss += loss
                n_batches += 1

                # Backward
                eg_w, eg_b, dg_w, dg_b = self._backward(
                    activations, pre_acts, x_batch
                )

                # Adam update
                t_step += 1
                for i in range(len(self._enc_weights)):
                    m_enc_w[i] = beta1 * m_enc_w[i] + (1 - beta1) * eg_w[i]
                    v_enc_w[i] = beta2 * v_enc_w[i] + (1 - beta2) * eg_w[i] ** 2
                    mh = m_enc_w[i] / (1 - beta1**t_step)
                    vh = v_enc_w[i] / (1 - beta2**t_step)
                    self._enc_weights[i] -= learning_rate * mh / (np.sqrt(vh) + eps)

                    m_enc_b[i] = beta1 * m_enc_b[i] + (1 - beta1) * eg_b[i]
                    v_enc_b[i] = beta2 * v_enc_b[i] + (1 - beta2) * eg_b[i] ** 2
                    mh_b = m_enc_b[i] / (1 - beta1**t_step)
                    vh_b = v_enc_b[i] / (1 - beta2**t_step)
                    self._enc_biases[i] -= learning_rate * mh_b / (np.sqrt(vh_b) + eps)

                for i in range(len(self._dec_weights)):
                    m_dec_w[i] = beta1 * m_dec_w[i] + (1 - beta1) * dg_w[i]
                    v_dec_w[i] = beta2 * v_dec_w[i] + (1 - beta2) * dg_w[i] ** 2
                    mh = m_dec_w[i] / (1 - beta1**t_step)
                    vh = v_dec_w[i] / (1 - beta2**t_step)
                    self._dec_weights[i] -= learning_rate * mh / (np.sqrt(vh) + eps)

                    m_dec_b[i] = beta1 * m_dec_b[i] + (1 - beta1) * dg_b[i]
                    v_dec_b[i] = beta2 * v_dec_b[i] + (1 - beta2) * dg_b[i] ** 2
                    mh_b = m_dec_b[i] / (1 - beta1**t_step)
                    vh_b = v_dec_b[i] / (1 - beta2**t_step)
                    self._dec_biases[i] -= learning_rate * mh_b / (np.sqrt(vh_b) + eps)

            avg_loss = epoch_loss / max(n_batches, 1)
            losses.append(avg_loss)

        return losses

    def reconstruct(self, X: np.ndarray) -> np.ndarray:
        """Reconstruct input through the autoencoder."""
        activations, _ = self._forward(X)
        return activations[-1]

    def reconstruction_error(self, X: np.ndarray) -> np.ndarray:
        """Per-sample MSE reconstruction error."""
        output = self.reconstruct(X)
        return np.mean((X - output) ** 2, axis=1)


# ---------------------------------------------------------------------------
# Public detector
# ---------------------------------------------------------------------------


class AutoencoderDetector:
    """Autoencoder-based anomaly detector for time-series metrics.

    Learns a compressed representation of normal metric behaviour.
    Points with high reconstruction error are flagged — this catches
    both point anomalies (spikes) and collective anomalies (gradual
    drift, cascading failures) that statistical methods miss.
    """

    def __init__(
        self,
        config: AutoencoderConfig | None = None,
    ) -> None:
        cfg = config or AutoencoderConfig()
        self._encoding_dim = cfg.encoding_dim
        self._hidden_layers = cfg.hidden_layers
        self._epochs = cfg.epochs
        self._batch_size = cfg.batch_size
        self._threshold_percentile = cfg.threshold_percentile

        self._autoencoder: _NumpyAutoencoder | None = None
        self._threshold: float = float("inf")
        self._feature_names: list[str] = []
        self._is_fitted: bool = False

        # Normalisation params
        self._mean: np.ndarray | None = None
        self._std: np.ndarray | None = None

    # ------------------------------------------------------------------
    # Feature engineering
    # ------------------------------------------------------------------

    @staticmethod
    def _build_features(
        values: np.ndarray,
        timestamps: np.ndarray,
    ) -> tuple[np.ndarray, list[str]]:
        """Build sliding-window features for the autoencoder."""
        n = len(values)
        features: list[np.ndarray] = []
        names: list[str] = []

        # Lag features
        for lag in [1, 2, 5, 10, 15]:
            arr = np.roll(values, lag)
            arr[:lag] = values[0]
            features.append(arr)
            names.append(f"lag_{lag}")

        # Rolling statistics (windows 5, 15, 30)
        for window in [5, 15, 30]:
            for fn, suffix in [
                (np.mean, "mean"),
                (np.std, "std"),
            ]:
                rolling = np.array(
                    [fn(values[max(0, i - window) : i + 1]) for i in range(n)],
                    dtype=np.float64,
                )
                features.append(rolling)
                names.append(f"rolling_{window}_{suffix}")

        # Rate of change
        roc = np.zeros(n, dtype=np.float64)
        roc[1:] = np.diff(values) / (np.abs(values[:-1]) + 1e-9)
        features.append(roc)
        names.append("rate_of_change")

        # Acceleration (second derivative)
        accel = np.zeros(n, dtype=np.float64)
        accel[2:] = np.diff(roc[1:])
        features.append(accel)
        names.append("acceleration")

        # Time features
        import pandas as pd

        dt = pd.to_datetime(timestamps, unit="s")
        features.append(dt.hour.values / 23.0)
        names.append("hour_of_day")
        features.append(dt.dayofweek.values / 6.0)
        names.append("day_of_week")

        X = np.column_stack(features)
        return X, names

    # ------------------------------------------------------------------
    # Public API
    # ------------------------------------------------------------------

    def fit(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
        epochs: int | None = None,
    ) -> None:
        """Train the autoencoder on normal data.

        Args:
            timestamps: Unix timestamps.
            values: Metric values.
            epochs: Override default epoch count.

        Raises:
            ValueError: Fewer than 200 data points.
        """
        n = len(values)
        if n < 200:
            raise ValueError(
                f"Autoencoder requires >=200 data points, got {n}"
            )

        values_arr = np.asarray(values, dtype=np.float64)
        timestamps_arr = np.asarray(timestamps, dtype=np.int64)

        X, names = self._build_features(values_arr, timestamps_arr)
        self._feature_names = names

        # Normalise
        self._mean = np.mean(X, axis=0)
        self._std = np.std(X, axis=0)
        self._std[self._std < 1e-9] = 1.0
        X_norm = (X - self._mean) / self._std

        # Train
        self._autoencoder = _NumpyAutoencoder(
            input_dim=X_norm.shape[1],
            encoding_dim=self._encoding_dim,
            hidden_layers=self._hidden_layers,
        )

        actual_epochs = epochs or self._epochs
        losses = self._autoencoder.fit(
            X_norm,
            epochs=actual_epochs,
            batch_size=self._batch_size,
        )

        # Set threshold from training reconstruction errors
        train_errors = self._autoencoder.reconstruction_error(X_norm)
        self._threshold = float(np.percentile(train_errors, self._threshold_percentile))
        self._is_fitted = True

        logger.info(
            "autoencoder_fitted",
            data_points=n,
            features=len(names),
            epochs=actual_epochs,
            final_loss=losses[-1] if losses else 0.0,
            threshold=f"{self._threshold:.6f}",
        )

    def detect(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
    ) -> list[AnomalyResult]:
        """Detect anomalies via reconstruction error.

        Args:
            timestamps: Unix timestamps.
            values: Metric values.

        Returns:
            List of :class:`AnomalyResult` for flagged points.
        """
        if not self._is_fitted or self._autoencoder is None:
            logger.warning("autoencoder_not_fitted")
            return []

        if self._mean is None or self._std is None:
            return []

        n = len(values)
        if n < 10:
            return []

        values_arr = np.asarray(values, dtype=np.float64)
        timestamps_arr = np.asarray(timestamps, dtype=np.int64)

        X, _ = self._build_features(values_arr, timestamps_arr)
        X_norm = (X - self._mean) / self._std

        errors = self._autoencoder.reconstruction_error(X_norm)

        anomalies: list[AnomalyResult] = []
        for i in range(n):
            if errors[i] > self._threshold:
                # Normalise score to 0-1
                score = float(np.clip(errors[i] / (self._threshold * 3), 0.0, 1.0))

                # Detect anomaly type
                if i >= 5:
                    recent_errors = errors[max(0, i - 5) : i + 1]
                    if np.all(recent_errors > self._threshold * 0.5):
                        anomaly_type = AnomalyType.COLLECTIVE
                        explanation = (
                            f"Collective anomaly: sustained high reconstruction "
                            f"error over recent window (err={errors[i]:.4f}, "
                            f"threshold={self._threshold:.4f})"
                        )
                    else:
                        anomaly_type = AnomalyType.CONTEXTUAL
                        explanation = (
                            f"Contextual anomaly: value {values_arr[i]:.2f} "
                            f"unexpected given recent patterns (err={errors[i]:.4f})"
                        )
                else:
                    anomaly_type = AnomalyType.POINT
                    explanation = (
                        f"Point anomaly: high reconstruction error "
                        f"{errors[i]:.4f} > threshold {self._threshold:.4f}"
                    )

                anomalies.append(
                    AnomalyResult(
                        timestamp=int(timestamps_arr[i]),
                        value=float(values_arr[i]),
                        score=score,
                        anomaly_type=anomaly_type,
                        explanation=explanation,
                        method="autoencoder",
                        severity=_severity_from_score(score),
                        contributing_factors=[
                            f"recon_error={errors[i]:.4f}",
                            f"threshold={self._threshold:.4f}",
                            f"error_ratio={errors[i] / self._threshold:.2f}x",
                        ],
                    )
                )

        return anomalies

    # ------------------------------------------------------------------
    # Properties
    # ------------------------------------------------------------------

    @property
    def is_fitted(self) -> bool:
        """True if the model has been trained."""
        return self._is_fitted

    @property
    def threshold(self) -> float:
        """Current reconstruction error threshold."""
        return self._threshold

    def get_reconstruction_errors(
        self,
        timestamps: Sequence[int],
        values: Sequence[float],
    ) -> np.ndarray:
        """Return per-point reconstruction errors (for diagnostics)."""
        if not self._is_fitted or self._autoencoder is None:
            return np.array([])
        if self._mean is None or self._std is None:
            return np.array([])

        values_arr = np.asarray(values, dtype=np.float64)
        timestamps_arr = np.asarray(timestamps, dtype=np.int64)
        X, _ = self._build_features(values_arr, timestamps_arr)
        X_norm = (X - self._mean) / self._std
        return self._autoencoder.reconstruction_error(X_norm)
