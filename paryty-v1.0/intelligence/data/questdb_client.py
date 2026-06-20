"""
QuestDB client for querying historical metrics.

Uses psycopg2 (PostgreSQL wire protocol) to connect to QuestDB on
port 8812.  All queries are tenant-scoped.

QuestDB supports standard PostgreSQL wire protocol for queries, making
psycopg2 compatible.

Connection pooling: uses psycopg2.pool.ThreadedConnectionPool for
efficient connection reuse across concurrent ML workers.
"""

from __future__ import annotations

import os
from typing import Any

import psycopg2
import psycopg2.extras
import psycopg2.pool
import structlog

logger = structlog.get_logger(__name__)


class QuestDBClient:
    """Read historical metrics from QuestDB.

    Connects via PostgreSQL wire protocol (port 8812 by default).
    All queries filter by ``tenant_id`` to enforce multi-tenancy.

    Uses a threaded connection pool (min 2, max 10 connections) for
efficient reuse across concurrent gRPC handler threads.

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
        min_conn: int = 2,
        max_conn: int = 10,
    ) -> None:
        self._host = host or os.getenv("QUESTDB_HOST", "localhost")
        self._port = port or int(os.getenv("QUESTDB_PORT", "8812"))
        self._database = database or os.getenv("QUESTDB_DATABASE", "paryty")
        self._user = user or os.getenv("QUESTDB_USER", "admin")
        self._password = password or os.getenv("QUESTDB_PASSWORD", "quest")
        self._min_conn = min_conn
        self._max_conn = max_conn
        self._pool: psycopg2.pool.ThreadedConnectionPool | None = None

    def connect(self) -> None:
        """Establish connection pool to QuestDB.

        Raises:
            ConnectionError: If the connection pool creation fails.
        """
        try:
            self._pool = psycopg2.pool.ThreadedConnectionPool(
                self._min_conn,
                self._max_conn,
                host=self._host,
                port=self._port,
                dbname=self._database,
                user=self._user,
                password=self._password,
                connect_timeout=10,
            )
            # Set autocommit on pool connections
            logger.info(
                "questdb_pool_connected",
                host=self._host,
                port=self._port,
                database=self._database,
                min_conn=self._min_conn,
                max_conn=self._max_conn,
            )
        except psycopg2.Error as exc:
            logger.error("questdb_connection_failed", error=str(exc))
            raise ConnectionError(
                f"Failed to connect to QuestDB at {self._host}:{self._port}: {exc}"
            ) from exc

    def disconnect(self) -> None:
        """Close all connections in the pool."""
        if self._pool is not None:
            try:
                self._pool.closeall()
            except Exception:
                pass
            self._pool = None
            logger.info("questdb_pool_disconnected")

    @property
    def is_connected(self) -> bool:
        """True if the connection pool is active."""
        return self._pool is not None

    def _ensure_connected(self) -> None:
        """Reconnect if connection pool is not initialised."""
        if self._pool is None:
            self.connect()

    def _get_conn(self) -> Any:
        """Get a connection from the pool."""
        self._ensure_connected()
        return self._pool.getconn()  # type: ignore[union-attr]

    def _put_conn(self, conn: Any) -> None:
        """Return a connection to the pool."""
        if self._pool is not None:
            self._pool.putconn(conn)

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

        conn = self._get_conn()
        try:
            with conn.cursor() as cur:
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
        finally:
            self._put_conn(conn)

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

        conn = self._get_conn()
        try:
            with conn.cursor() as cur:
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
        finally:
            self._put_conn(conn)

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

        conn = self._get_conn()
        try:
            with conn.cursor() as cur:
                cur.execute(query, params)
                rows = cur.fetchall()
            return [row[0] for row in rows]
        except psycopg2.Error as exc:
            logger.error("questdb_list_metrics_failed", error=str(exc))
            return []
        finally:
            self._put_conn(conn)

    def __enter__(self) -> QuestDBClient:
        self.connect()
        return self

    def __exit__(self, *_: Any) -> None:
        self.disconnect()
