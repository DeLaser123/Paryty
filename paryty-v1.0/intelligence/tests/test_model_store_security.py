"""
Regression tests for model artifact filename sanitization.

Pins the fix for the path-traversal class: ``metric_name`` arrives from
the network (RPC request) and was previously interpolated raw into model
file paths. Combined with joblib (pickle) deserialization, traversal =
arbitrary file write = remote code execution.
"""

from __future__ import annotations

from intelligence.anomaly.model_store import AnomalyModelStore
from intelligence.forecasting.model_store import ForecastModelStore
from intelligence.security import sanitize_filename_component


class TestSanitizeFilenameComponent:
    def test_plain_name_unchanged(self):
        assert sanitize_filename_component("cpu.usage_percent") == "cpu.usage_percent"

    def test_path_traversal_neutralized(self):
        out = sanitize_filename_component("../../etc/cron.d/evil")
        assert "/" not in out
        assert "\\" not in out
        assert ".." not in out

    def test_windows_separators_neutralized(self):
        out = sanitize_filename_component(r"..\..\windows\system32")
        assert "\\" not in out
        assert ".." not in out

    def test_empty_becomes_unnamed(self):
        assert sanitize_filename_component("") == "unnamed"

    def test_dots_only_becomes_unnamed(self):
        assert sanitize_filename_component("....") == "unnamed"

    def test_length_bounded(self):
        assert len(sanitize_filename_component("x" * 1000)) <= 100

    def test_null_bytes_removed(self):
        out = sanitize_filename_component("evil\x00name")
        assert "\x00" not in out


class TestModelStorePathConfinement:
    """Model files must stay inside the store's base directory."""

    def test_anomaly_store_confines_traversal(self, tmp_path):
        store = AnomalyModelStore(base_dir=tmp_path)
        path = store.save("isolation_forest", "../../escape", {"weights": [1, 2, 3]})
        assert path.resolve().is_relative_to(tmp_path.resolve()), (
            f"artifact escaped the model directory: {path}"
        )

    def test_forecast_store_confines_traversal(self, tmp_path):
        store = ForecastModelStore(base_dir=tmp_path)
        path = store.save("linear", "..\\..\\escape", {"coef": [0.5]})
        assert path.resolve().is_relative_to(tmp_path.resolve()), (
            f"artifact escaped the model directory: {path}"
        )

    def test_round_trip_with_hostile_name(self, tmp_path):
        store = AnomalyModelStore(base_dir=tmp_path)
        artifact = {"model": "test", "values": [1.0, 2.0]}
        store.save("autoencoder", "../hostile", artifact)
        loaded = store.load("autoencoder", "../hostile")
        assert loaded == artifact, "sanitized save/load must round-trip consistently"
