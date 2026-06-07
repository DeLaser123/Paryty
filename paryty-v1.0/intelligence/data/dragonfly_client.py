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

    def get_latest_metrics(
        self,
        agent_id: str,
        tenant_id: str,
    ) -> dict[str, float]:
        """Get the latest value for each metric of an agent.

        Args:
            agent_id: Agent identifier.
            tenant_id: Tenant scope.

        Returns:
            Dict of metric_name → latest_value.
        """
        self._ensure_connected()
        assert self._client is not None

        # Scan for keys matching this agent
        pattern = f"metrics:{tenant_id}:{agent_id}:*"
        result: dict[str, float] = {}

        try:
            cursor = 0
            while True:
                cursor, keys = self._client.scan(
                    cursor=cursor, match=pattern, count=100
                )
                for key in keys:
                    # Extract metric name from key
                    parts = key.split(":")
                    if len(parts) >= 4:
                        metric_name = ":".join(parts[3:])
                        # Get the latest entry (highest score = most recent)
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
        self._ensure_connected()
        assert self._client is not None

        key = self._metric_key(metric_name, agent_id, tenant_id)

        try:
            # Get all entries in the sorted set
            entries = self._client.zrange(key, 0, -1, withscores=True)

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
        entry = json.dumps({"value": value, "ts": timestamp})

        try:
            pipe = self._client.pipeline()
            pipe.zadd(key, {entry: timestamp})
            pipe.zremrangebyrank(key, 0, -(max_entries + 1))
            pipe.execute()
        except redis.RedisError as exc:
            logger.error("dragonfly_store_failed", error=str(exc))

    def __enter__(self) -> DragonflyClient:
        self.connect()
        return self

    def __exit__(self, *_: Any) -> None:
        self.disconnect()
