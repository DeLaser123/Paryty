import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 100,
  duration: '5m',
  thresholds: {
    http_req_duration: ['p(99)<500'],
    http_req_failed: ['rate<0.001'],
  },
};

const BASE_URL = __ENV.PARYTY_URL || 'http://localhost:8080';
const API_KEY = __ENV.PARYTY_API_KEY || 'test-key';

export default function() {
  // Simulate agent metric submission
  const payload = JSON.stringify({
    agent_id: `agent-${__VU}`,
    metrics: {
      cpu_usage: Math.random() * 100,
      memory_usage: Math.random() * 100,
    },
    timestamp: new Date().toISOString(),
  });

  const res = http.post(`${BASE_URL}/api/v1/ingestion/metrics`, payload, {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${API_KEY}`,
    },
  });

  check(res, {
    'status is 200': (r) => r.status === 200,
    'latency < 100ms': (r) => r.timings.duration < 100,
  });

  sleep(1);
}
