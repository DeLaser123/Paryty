// Metric processing Web Worker
// Runs CPU-intensive metric parsing and aggregation off the main thread

interface MetricData {
  timestamp: number;
  value: number;
  labels: Record<string, string>;
}

interface AggregatedResult {
  name: string;
  labels: Record<string, string>;
  count: number;
  sum: number;
  min: number;
  max: number;
  avg: number;
  p50: number;
  p90: number;
  p99: number;
}

// Worker message types
interface WorkerMessage {
  type: 'aggregate' | 'parse' | 'filter';
  id: string;
  data: unknown;
}

interface AggregateMessage extends WorkerMessage {
  type: 'aggregate';
  data: {
    metrics: MetricData[];
    windowMs: number;
  };
}

interface ParseMessage extends WorkerMessage {
  type: 'parse';
  data: {
    raw: string;
    format: 'json' | 'line';
  };
}

self.onmessage = (event: MessageEvent<WorkerMessage>) => {
  const { type, id } = event.data;

  switch (type) {
    case 'aggregate': {
      const { metrics, windowMs } = (event.data as AggregateMessage).data;
      const result = aggregateMetrics(metrics, windowMs);
      self.postMessage({ type: 'result', id, data: result });
      break;
    }
    case 'parse': {
      const { raw, format } = (event.data as ParseMessage).data;
      const result = parseMetrics(raw, format);
      self.postMessage({ type: 'result', id, data: result });
      break;
    }
    default:
      self.postMessage({ type: 'error', id, data: `Unknown message type: ${type}` });
  }
};

function aggregateMetrics(metrics: MetricData[], _windowMs: number): AggregatedResult[] {
  if (metrics.length === 0) return [];

  // Group by name+labels
  const groups = new Map<string, MetricData[]>();
  for (const m of metrics) {
    const key = JSON.stringify(m.labels);
    const group = groups.get(key) ?? [];
    group.push(m);
    groups.set(key, group);
  }

  const results: AggregatedResult[] = [];
  for (const [key, group] of groups) {
    const values = group.map((m) => m.value).sort((a, b) => a - b);
    const sum = values.reduce((a, b) => a + b, 0);
    const count = values.length;

    results.push({
      name: '',
      labels: JSON.parse(key) as Record<string, string>,
      count,
      sum,
      min: values[0],
      max: values[count - 1],
      avg: sum / count,
      p50: percentile(values, 0.5),
      p90: percentile(values, 0.9),
      p99: percentile(values, 0.99),
    });
  }

  return results;
}

function percentile(sorted: number[], p: number): number {
  const idx = Math.ceil(p * sorted.length) - 1;
  return sorted[Math.max(0, Math.min(idx, sorted.length - 1))];
}

function parseMetrics(raw: string, format: 'json' | 'line'): MetricData[] {
  if (format === 'json') {
    try {
      return JSON.parse(raw) as MetricData[];
    } catch {
      return [];
    }
  }

  // Line protocol (InfluxDB line format)
  const lines = raw.split('\n').filter((l) => l.trim() && !l.startsWith('#'));
  const metrics: MetricData[] = [];

  for (const line of lines) {
    try {
      const parts = line.split(' ');
      if (parts.length < 2) continue;
      const [measurement, valueStr, timestampStr] = parts;
      const [name, labelStr] = measurement.split(',');
      const labels: Record<string, string> = {};
      if (labelStr) {
        for (const pair of labelStr.split(',')) {
          const [k, v] = pair.split('=');
          if (k && v) labels[k] = v;
        }
      }
      metrics.push({
        timestamp: parseInt(timestampStr, 10) || Date.now(),
        value: parseFloat(valueStr) || 0,
        labels: { ...labels, __name__: name },
      });
    } catch {
      // Skip malformed lines
    }
  }

  return metrics;
}
