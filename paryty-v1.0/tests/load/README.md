# Paryty Load Tests

## Prerequisites
- k6 installed: https://k6.io/docs/getting-started/installation/
- Paryty cluster running locally

## Running Tests
```bash
# Baseline: 100 agents, 5 minutes
k6 run tests/load/scenarios/baseline.js

# Scale: 10,000 agents, 10 minutes
k6 run tests/load/scenarios/scale.js

# Burst: 1,000 agents, 2 minutes
k6 run tests/load/scenarios/burst.js
```

## Performance Targets
- Ingestion p99: <100ms
- Query p99: <500ms
- Error rate: <0.1%
