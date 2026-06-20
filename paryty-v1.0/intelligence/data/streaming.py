"""
Redpanda (Kafka-compatible) streaming client for the Intelligence Layer.

Provides:
- MetricConsumer: consumes raw metrics from the ``metrics.ingested`` topic.
- IntelligenceProducer: publishes intelligence results (forecasts, anomalies)
  to output topics.

Uses confluent-kafka for high-performance Kafka protocol support.
Redpanda is wire-compatible with Kafka, so the standard Kafka client works.
"""

from __future__ import annotations

import json
import os
import threading
from typing import Any, Callable

import structlog

logger = structlog.get_logger(__name__)

try:
    from confluent_kafka import Consumer, Producer, KafkaError, KafkaException
    _HAS_KAFKA = True
except ImportError:
    _HAS_KAFKA = False
    logger.warning("confluent_kafka_not_installed", msg="Streaming disabled")


class MetricConsumer:
    """Consume raw metrics from Redpanda topics.

    Supports both explicit topic lists and regex pattern subscription.
    The default pattern ``paryty\\..*\\.metrics\\.raw`` matches the
    Go cluster's tenant-scoped topic naming scheme.

    Each message is expected to be a JSON object with at least:
    - ``tenant_id``: str
    - ``agent_id``: str
    - ``metric_name``: str
    - ``value``: float
    - ``timestamp``: int (unix seconds)

    The consumer runs in a background thread and dispatches decoded
    messages to the provided callback.
    """

    def __init__(
        self,
        brokers: str | None = None,
        topic: str = "metrics.ingested",
        group_id: str = "intelligence-metrics",
        topic_pattern: str | None = None,
    ) -> None:
        self._brokers = brokers or os.getenv("REDPANDA_BROKERS", "localhost:9092")
        self._topic = topic
        self._topic_pattern = topic_pattern
        self._group_id = group_id
        self._consumer: Any = None
        self._running = False
        self._thread: threading.Thread | None = None
        self._callback: Callable[[dict[str, Any]], None] | None = None

    def connect(self) -> None:
        """Create the Kafka consumer."""
        if not _HAS_KAFKA:
            logger.warning("kafka_consumer_skipped", reason="confluent-kafka not installed")
            return

        self._consumer = Consumer({
            "bootstrap.servers": self._brokers,
            "group.id": self._group_id,
            "auto.offset.reset": "latest",
            "enable.auto.commit": True,
            "session.timeout.ms": 10000,
            "max.poll.interval.ms": 300000,
        })
        if self._topic_pattern is not None:
            # Regex subscription for tenant-scoped topics
            self._consumer.subscribe([], pattern=self._topic_pattern)
            logger.info(
                "kafka_consumer_connected",
                brokers=self._brokers,
                topic_pattern=self._topic_pattern,
                group_id=self._group_id,
            )
        else:
            self._consumer.subscribe([self._topic])
            logger.info(
                "kafka_consumer_connected",
                brokers=self._brokers,
                topic=self._topic,
                group_id=self._group_id,
            )

    def start(self, callback: Callable[[dict[str, Any]], None]) -> None:
        """Start consuming in a background thread.

        Args:
            callback: Function called with each decoded message dict.
        """
        if not _HAS_KAFKA or self._consumer is None:
            logger.warning("kafka_consumer_not_started", reason="not connected")
            return

        self._callback = callback
        self._running = True
        self._thread = threading.Thread(
            target=self._consume_loop,
            name="kafka-metric-consumer",
            daemon=True,
        )
        self._thread.start()
        logger.info("kafka_consumer_started", topic=self._topic)

    def stop(self) -> None:
        """Stop the consumer and close the connection."""
        self._running = False
        if self._thread is not None:
            self._thread.join(timeout=10)
            self._thread = None
        if self._consumer is not None:
            try:
                self._consumer.close()
            except Exception:
                pass
            self._consumer = None
        logger.info("kafka_consumer_stopped")

    def _consume_loop(self) -> None:
        """Background consumer loop."""
        while self._running:
            try:
                msg = self._consumer.poll(timeout=1.0)
                if msg is None:
                    continue
                if msg.error():
                    if msg.error().code() == KafkaError._PARTITION_EOF:
                        continue
                    logger.error("kafka_consumer_error", error=str(msg.error()))
                    continue

                try:
                    data = json.loads(msg.value().decode("utf-8"))
                    if self._callback is not None:
                        self._callback(data)
                except (json.JSONDecodeError, UnicodeDecodeError) as exc:
                    logger.warning("kafka_message_decode_failed", error=str(exc))

            except KafkaException as exc:
                logger.error("kafka_consumer_exception", error=str(exc))
            except Exception as exc:
                logger.error("kafka_consumer_unexpected", error=str(exc))

    def __enter__(self) -> MetricConsumer:
        self.connect()
        return self

    def __exit__(self, *_: Any) -> None:
        self.stop()


class IntelligenceProducer:
    """Publish intelligence results to Redpanda output topics.

    Topics:
    - ``intelligence.anomalies``: detected anomalies
    - ``intelligence.forecasts``: forecast results
    """

    # Default output topics (legacy flat names — prefer tenant-scoped methods)
    TOPIC_ANOMALIES = "intelligence.anomalies"
    TOPIC_FORECASTS = "intelligence.forecasts"
    TOPIC_DRIFT = "intelligence.drift"

    def __init__(
        self,
        brokers: str | None = None,
    ) -> None:
        self._brokers = brokers or os.getenv("REDPANDA_BROKERS", "localhost:9092")
        self._producer: Any = None

    def connect(self) -> None:
        """Create the Kafka producer."""
        if not _HAS_KAFKA:
            logger.warning("kafka_producer_skipped", reason="confluent-kafka not installed")
            return

        self._producer = Producer({
            "bootstrap.servers": self._brokers,
            "acks": "all",
            "retries": 3,
            "linger.ms": 5,
        })
        logger.info("kafka_producer_connected", brokers=self._brokers)

    def publish_anomaly(
        self,
        tenant_id: str,
        agent_id: str,
        metric_name: str,
        anomalies: list[dict[str, Any]],
    ) -> None:
        """Publish detected anomalies to the anomalies topic.

        Args:
            tenant_id: Tenant scope.
            agent_id: Agent that produced the metric.
            metric_name: The metric that was analyzed.
            anomalies: List of anomaly dicts.
        """
        self._publish(self.TOPIC_ANOMALIES, key=f"{tenant_id}:{metric_name}", value={
            "tenant_id": tenant_id,
            "agent_id": agent_id,
            "metric_name": metric_name,
            "anomalies": anomalies,
        })

    def publish_forecast(
        self,
        tenant_id: str,
        agent_id: str,
        metric_name: str,
        forecast: dict[str, Any],
    ) -> None:
        """Publish a forecast result to the forecasts topic.

        Args:
            tenant_id: Tenant scope.
            agent_id: Agent that produced the metric.
            metric_name: The metric that was forecasted.
            forecast: Forecast result dict.
        """
        self._publish(self.TOPIC_FORECASTS, key=f"{tenant_id}:{metric_name}", value={
            "tenant_id": tenant_id,
            "agent_id": agent_id,
            "metric_name": metric_name,
            "forecast": forecast,
        })

    # ── Tenant-scoped publish (matches Go topic scheme) ──────────

    def publish_anomaly_tenant(
        self,
        tenant_id: str,
        agent_id: str,
        metric_name: str,
        anomalies: list[dict[str, Any]],
    ) -> None:
        """Publish anomalies to tenant-scoped topic ``paryty.<tenant>.anomalies``."""
        topic = f"paryty.{tenant_id}.anomalies"
        self._publish(topic, key=f"{tenant_id}:{metric_name}", value={
            "tenant_id": tenant_id,
            "agent_id": agent_id,
            "metric_name": metric_name,
            "anomalies": anomalies,
            "source": "pipeline",
        })

    def publish_forecast_tenant(
        self,
        tenant_id: str,
        agent_id: str,
        metric_name: str,
        forecast: dict[str, Any],
    ) -> None:
        """Publish a forecast to tenant-scoped topic ``paryty.<tenant>.forecasts``."""
        topic = f"paryty.{tenant_id}.forecasts"
        self._publish(topic, key=f"{tenant_id}:{metric_name}", value={
            "tenant_id": tenant_id,
            "agent_id": agent_id,
            "metric_name": metric_name,
            "forecast": forecast,
            "source": "pipeline",
        })

    def publish_drift(
        self,
        tenant_id: str,
        agent_id: str,
        metric_name: str,
        drift_type: str,
        confidence: float,
        timestamp: int,
    ) -> None:
        """Publish a concept drift event to ``paryty.<tenant>.drift``."""
        topic = f"paryty.{tenant_id}.drift"
        self._publish(topic, key=f"{tenant_id}:{metric_name}", value={
            "tenant_id": tenant_id,
            "agent_id": agent_id,
            "metric_name": metric_name,
            "drift_type": drift_type,
            "confidence": confidence,
            "timestamp": timestamp,
            "source": "pipeline",
        })

    # ── Low-level publish ──────────────────────────────────────────

    def _publish(self, topic: str, key: str, value: dict[str, Any]) -> None:
        """Publish a message to a topic."""
        if self._producer is None:
            logger.warning("kafka_publish_skipped", topic=topic, reason="not connected")
            return

        try:
            self._producer.produce(
                topic=topic,
                key=key.encode("utf-8"),
                value=json.dumps(value, default=str).encode("utf-8"),
                callback=self._delivery_callback,
            )
            self._producer.poll(0)  # trigger delivery callbacks
        except KafkaException as exc:
            logger.error("kafka_publish_failed", topic=topic, error=str(exc))

    @staticmethod
    def _delivery_callback(err: Any, msg: Any) -> None:
        """Kafka delivery callback."""
        if err is not None:
            logger.error("kafka_delivery_failed", error=str(err))

    def flush(self, timeout: float = 10.0) -> None:
        """Flush pending messages."""
        if self._producer is not None:
            self._producer.flush(timeout=timeout)

    def close(self) -> None:
        """Flush and close the producer."""
        if self._producer is not None:
            self.flush()
            self._producer = None
            logger.info("kafka_producer_closed")

    def __enter__(self) -> IntelligenceProducer:
        self.connect()
        return self

    def __exit__(self, *_: Any) -> None:
        self.close()
