"""
Dragonfly client for reading hot metric data.

Uses redis-py (Dragonfly is Redis-compatible) on port 6379.
Provides access to the most recent metric values without querying
the cold storage (QuestDB).
"""

from __future__ import annotations

import json
import os
from typing import Any

import redis
import structlog

logger = structlog.get_logger(__name__)


class DragonflyClient:
    """Read hot metric data from Dragonfly (Redis-compatible).

    Dragonfly stores the most recent N data points per metric per agent
    in sorted sets (scored by timestamp).  This client provides fast
    reads for real-time anomaly detection and forecasting.
    """

    def __init__(
        self,
        host: str | None = None,
        port: int | None = None,
        db: int | None = None,
        password: str | None = None,
    ) -> None:
        self._host = host or os.getenv("DRAGONFLY_HOST", "localhost")
        self._port = port or int(os.getenv("DRAGONFLY_PORT", "6379"))
        self._db = db or int(os.getenv("DRAGONFLY_DB", "0"))
        self._password = password or os.getenv("DRAGONFLY_PASSWORD", "")
        self._client: redis.Redis | None = None

    def connect(self) -> None:
        """Establish connection to Dragonfly.

        Raises:
            ConnectionError: If the connection fails.
        """
        try:
            self._client = redis.Redis(
                host=self._host,
                port=self._port,
                db=self._db,
                password=self._password or None,
                decode_responses=True,
                socket_connect_timeout=5,
                socket_timeout=5,
            )
            self._client.ping()
            logger.info(
                "dragonfly_connected",
                host=self._host,
                port=self._port,
                db=self._db,
            )
        except redis.RedisError as exc:
            logger.error("dragonfly_connection_failed", error=str(exc))
            raise ConnectionError(
                f"Failed to connect to Dragonfly at {self._host}:{self._port}: {exc}"
            ) from exc

    def disconnect(self) -> None:
        """Close the connection."""
        if self._client is not None:
            try:
                self._client.close()
            except Exception:
                pass
            self._client = None
            logger.info("dragonfly_disconnected")

    @property
    def is_connected(self) -> bool:
        """True if the connection is alive."""
        if self._client is None:
            return False
        try:
            self._client.ping()
            return True
        except redis.RedisError:
            return False

    def _ensure_connected(self) -> None:
        """Reconnect if connection is lost."""
        if not self.is_connected:
            self.connect()

    # ------------------------------------------------------------------
    # Key scheme
    # ------------------------------------------------------------------

    @staticmethod
    def _metric_key(
        metric_name: str,
        agent_id: str,
        tenant_id: str,
    ) -> str:
        """Build the Redis key for a metric sorted set."""
        return f"metrics:{tenant_id}:{agent_id}:{metric_name}"

    # ------------------------------------------------------------------
    # Read API
    # ------------------------------------------------------------------

    @staticmethod
    def _metric_index_key(tenant_id: str, agent_id: str) -> str:
        """Build the Redis SET key that indexes metric names per agent."""
        return f"metrics_index:{tenant_id}:{agent_id}"

    def get_latest_metrics(
        self,
        agent_id: str,
        tenant_id: str,
    ) -> dict[str, float]:
        """Get the latest value for each metric of an agent.

        Uses an index SET for O(1) lookups instead of SCAN.
        Falls back to SCAN if the index does not exist, and populates
        the index for subsequent calls.

        Args:
            agent_id: Agent identifier.
            tenant_id: Tenant scope.

        Returns:
            Dict of metric_name → latest_value.
        """
        self._ensure_connected()
        assert self._client is not None

        result: dict[str, float] = {}
        index_key = self._metric_index_key(tenant_id, agent_id)

        try:
            # Try indexed lookup first (O(1) per metric)
            metric_names = self._client.smembers(index_key)
            if metric_names:
                for metric_name in metric_names:
                    key = self._metric_key(metric_name, agent_id, tenant_id)
                    entries = self._client.zrevrange(key, 0, 0, withscores=True)
                    if entries:
                        value_str = entries[0][0]
                        try:
                            value_data = json.loads(value_str)
                            result[metric_name] = float(
                                value_data.get("value", 0)
                            )
                        except (json.JSONDecodeError, TypeError):
                            result[metric_name] = float(value_str)
                return result

            # Fallback: SCAN and populate the index for next call
            pattern = f"metrics:{tenant_id}:{agent_id}:*"
            pipe = self._client.pipeline()
            cursor = 0
            while True:
                cursor, keys = self._client.scan(
                    cursor=cursor, match=pattern, count=100
                )
                for key in keys:
                    parts = key.split(":")
                    if len(parts) >= 4:
                        metric_name = ":".join(parts[3:])
                        pipe.sadd(index_key, metric_name)
                        entries = self._client.zrevrange(key, 0, 0, withscores=True)
                        if entries:
                            value_str = entries[0][0]
                            try:
                                value_data = json.loads(value_str)
                                result[metric_name] = float(
                                    value_data.get("value", 0)
                                )
                            except (json.JSONDecodeError, TypeError):
                                result[metric_name] = float(value_str)
                if cursor == 0:
                    break
            # Execute the index population pipeline
            try:
                pipe.execute()
            except redis.RedisError:
                pass  # non-critical

            return result

        except redis.RedisError as exc:
            logger.error(
                "dragonfly_latest_metrics_failed",
                agent=agent_id,
                tenant=tenant_id,
                error=str(exc),
            )
            return {}

    def get_metric_window(
        self,
        metric_name: str,
        agent_id: str,
        tenant_id: str,
        window_minutes: int = 60,
    ) -> tuple[list[int], list[float]]:
        """Get a window of recent metric values.

        Args:
            metric_name: Name of the metric.
            agent_id: Agent identifier.
            tenant_id: Tenant scope.
            window_minutes: How many minutes of data to fetch.

        Returns:
            Tuple of (timestamps, values) in ascending order.
        """
        import time as _time

        self._ensure_connected()
        assert self._client is not None

        key = self._metric_key(metric_name, agent_id, tenant_id)

        try:
            # Filter by window: only fetch entries scored >= (now - window_minutes)
            cutoff = int(_time.time()) - (window_minutes * 60)
            entries = self._client.zrangebyscore(
                key, min=cutoff, max="+inf", withscores=True
            )

            timestamps: list[int] = []
            values: list[float] = []

            for value_str, score in entries:
                ts = int(score)
                try:
                    value_data = json.loads(value_str)
                    val = float(value_data.get("value", 0))
                except (json.JSONDecodeError, TypeError):
                    val = float(value_str)
                timestamps.append(ts)
                values.append(val)

            return timestamps, values

        except redis.RedisError as exc:
            logger.error(
                "dragonfly_metric_window_failed",
                metric=metric_name,
                agent=agent_id,
                tenant=tenant_id,
                error=str(exc),
            )
            return [], []

    def store_metric(
        self,
        metric_name: str,
        agent_id: str,
        tenant_id: str,
        timestamp: int,
        value: float,
        max_entries: int = 10000,
    ) -> None:
        """Store a metric value (for testing / local development).

        Args:
            metric_name: Metric name.
            agent_id: Agent identifier.
            tenant_id: Tenant scope.
            timestamp: Unix timestamp.
            value: Metric value.
            max_entries: Max entries to keep in the sorted set.
        """
        self._ensure_connected()
        assert self._client is not None

        key = self._metric_key(metric_name, agent_id, tenant_id)
        index_key = self._metric_index_key(tenant_id, agent_id)
        entry = json.dumps({"value": value, "ts": timestamp})

        try:
            pipe = self._client.pipeline()
            pipe.zadd(key, {entry: timestamp})
            pipe.zremrangebyrank(key, 0, -(max_entries + 1))
            pipe.sadd(index_key, metric_name)
            pipe.execute()
        except redis.RedisError as exc:
            logger.error("dragonfly_store_failed", error=str(exc))

    def __enter__(self) -> DragonflyClient:
        self.connect()
        return self

    def __exit__(self, *_: Any) -> None:
        self.disconnect()
