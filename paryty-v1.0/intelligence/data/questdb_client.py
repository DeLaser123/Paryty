"""
QuestDB client for querying historical metrics.

Uses psycopg2 (PostgreSQL wire protocol) to connect to QuestDB on
port 8812.  All queries are tenant-scoped.

QuestDB supports standard PostgreSQL wire protocol for queries, making
psycopg2 compatible.
"""

from __future__ import annotations

import os
from typing import Any

import psycopg2
import psycopg2.extras
import structlog

logger = structlog.get_logger(__name__)


class QuestDBClient:
    """Read historical metrics from QuestDB.

    Connects via PostgreSQL wire protocol (port 8812 by default).
    All queries filter by ``tenant_id`` to enforce multi-tenancy.

    Usage::

        client = QuestDBClient()
        client.connect()
        timestamps, values = client.query_metric_history(
            "cpu_usage", tenant_id="t1", hours_back=24
        )
    """

    def __init__(
        self,
        host: str | None = None,
        port: int | None = None,
        database: str | None = None,
        user: str | None = None,
        password: str | None = None,
    ) -> None:
        self._host = host or os.getenv("QUESTDB_HOST", "localhost")
        self._port = port or int(os.getenv("QUESTDB_PORT", "8812"))
        self._database = database or os.getenv("QUESTDB_DATABASE", "paryty")
        self._user = user or os.getenv("QUESTDB_USER", "admin")
        self._password = password or os.getenv("QUESTDB_PASSWORD", "quest")
        self._conn: Any | None = None

    def connect(self) -> None:
        """Establish connection to QuestDB.

        Raises:
            ConnectionError: If the connection fails.
        """
        try:
            self._conn = psycopg2.connect(
                host=self._host,
                port=self._port,
                dbname=self._database,
                user=self._user,
                password=self._password,
                connect_timeout=10,
            )
            self._conn.autocommit = True
            logger.info(
                "questdb_connected",
                host=self._host,
                port=self._port,
                database=self._database,
            )
        except psycopg2.Error as exc:
            logger.error("questdb_connection_failed", error=str(exc))
            raise ConnectionError(
                f"Failed to connect to QuestDB at {self._host}:{self._port}: {exc}"
            ) from exc

    def disconnect(self) -> None:
        """Close the connection."""
        if self._conn is not None:
            try:
                self._conn.close()
            except Exception:
                pass
            self._conn = None
            logger.info("questdb_disconnected")

    @property
    def is_connected(self) -> bool:
        """True if the connection is alive."""
        if self._conn is None:
            return False
        try:
            with self._conn.cursor() as cur:
                cur.execute("SELECT 1")
            return True
        except Exception:
            return False

    def _ensure_connected(self) -> None:
        """Reconnect if connection is lost."""
        if not self.is_connected:
            self.connect()

    # ------------------------------------------------------------------
    # Query API
    # ------------------------------------------------------------------

    def query_metric_history(
        self,
        metric_name: str,
        tenant_id: str,
        hours_back: int = 24,
        agent_id: str | None = None,
    ) -> tuple[list[int], list[float]]:
        """Query historical metric data from QuestDB.

        Args:
            metric_name: Name of the metric (e.g. "cpu_usage").
            tenant_id: Tenant scope.
            hours_back: How many hours of history to fetch.
            agent_id: Optional agent filter.

        Returns:
            Tuple of (timestamps, values) where timestamps are Unix seconds.
        """
        self._ensure_connected()

        query = """
            SELECT timestamp, value
            FROM metrics
            WHERE metric_name = %s
              AND tenant_id = %s
              AND timestamp >= dateadd('h', %s, now())
        """
        params: list[Any] = [metric_name, tenant_id, -hours_back]

        if agent_id:
            query += " AND agent_id = %s"
            params.append(agent_id)

        query += " ORDER BY timestamp ASC"

        try:
            with self._conn.cursor() as cur:  # type: ignore[union-attr]
                cur.execute(query, params)
                rows = cur.fetchall()

            timestamps: list[int] = []
            values: list[float] = []
            for row in rows:
                ts = row[0]
                # QuestDB returns datetime objects
                if hasattr(ts, "timestamp"):
                    timestamps.append(int(ts.timestamp()))
                else:
                    timestamps.append(int(ts))
                values.append(float(row[1]))

            logger.debug(
                "questdb_metric_history",
                metric=metric_name,
                tenant=tenant_id,
                points=len(timestamps),
                hours_back=hours_back,
            )
            return timestamps, values

        except psycopg2.Error as exc:
            logger.error(
                "questdb_query_failed",
                metric=metric_name,
                tenant=tenant_id,
                error=str(exc),
            )
            return [], []

    def query_recent(
        self,
        metric_name: str,
        tenant_id: str,
        limit: int = 1000,
        agent_id: str | None = None,
    ) -> tuple[list[int], list[float]]:
        """Query the most recent data points.

        Args:
            metric_name: Name of the metric.
            tenant_id: Tenant scope.
            limit: Maximum number of data points.
            agent_id: Optional agent filter.

        Returns:
            Tuple of (timestamps, values) ordered ascending.
        """
        self._ensure_connected()

        query = """
            SELECT timestamp, value
            FROM metrics
            WHERE metric_name = %s
              AND tenant_id = %s
        """
        params: list[Any] = [metric_name, tenant_id]

        if agent_id:
            query += " AND agent_id = %s"
            params.append(agent_id)

        query += " ORDER BY timestamp DESC LIMIT %s"
        params.append(limit)

        try:
            with self._conn.cursor() as cur:  # type: ignore[union-attr]
                cur.execute(query, params)
                rows = cur.fetchall()

            # Reverse to ascending order
            rows = list(reversed(rows))

            timestamps: list[int] = []
            values: list[float] = []
            for row in rows:
                ts = row[0]
                if hasattr(ts, "timestamp"):
                    timestamps.append(int(ts.timestamp()))
                else:
                    timestamps.append(int(ts))
                values.append(float(row[1]))

            return timestamps, values

        except psycopg2.Error as exc:
            logger.error(
                "questdb_query_recent_failed",
                metric=metric_name,
                tenant=tenant_id,
                error=str(exc),
            )
            return [], []

    def query_available_metrics(
        self,
        tenant_id: str,
        agent_id: str | None = None,
    ) -> list[str]:
        """List available metrics for a tenant.

        Returns:
            List of metric names.
        """
        self._ensure_connected()

        query = "SELECT DISTINCT metric_name FROM metrics WHERE tenant_id = %s"
        params: list[Any] = [tenant_id]

        if agent_id:
            query += " AND agent_id = %s"
            params.append(agent_id)

        try:
            with self._conn.cursor() as cur:  # type: ignore[union-attr]
                cur.execute(query, params)
                rows = cur.fetchall()
            return [row[0] for row in rows]
        except psycopg2.Error as exc:
            logger.error("questdb_list_metrics_failed", error=str(exc))
            return []

    def __enter__(self) -> QuestDBClient:
        self.connect()
        return self

    def __exit__(self, *_: Any) -> None:
        self.disconnect()
