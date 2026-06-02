# Verify Intelligence Layer

Verify the Intelligence Layer (Forecasting, Anomaly Detection, Simulation, Timeline) for accuracy, safety, and correctness.

## Verification Steps

### 1. Frontend Type Check & Build
```bash
cd frontend && npx tsc --noEmit 2>&1
cd frontend && npm run build 2>&1
```

### 2. Forecast Accuracy Tests
- Backtesting: compare forecast vs actual for historical data
- MAPE < 10% for CPU/memory forecasts
- 95% confidence interval coverage >= 95%
- Latency: forecast generation <100ms

### 3. Anomaly Detection Tests
- Precision >95% (few false positives)
- Recall >90% (few missed anomalies)
- Latency: detection <50ms
- Test with NAB and Yahoo S5 datasets

### 4. Simulation Safety Tests
- Chaos experiment has automatic rollback
- Blast radius limited to target services
- SLO violation triggers experiment stop
- Reproducibility: same inputs produce same outputs

### 5. Timeline Tests
- Snapshot completeness: full state captured
- Replay accuracy: state matches original
- Diff correctness: all changes identified
- Speed controls: all settings work (0.25x to 16x)

### 6. Security Audit
- [ ] No arbitrary code execution in simulation
- [ ] Python services sandboxed (container isolation)
- [ ] No sensitive data in forecast outputs
- [ ] Chaos experiments require authorization
- [ ] Timeline exports sanitized

## Pass Criteria
- Forecast accuracy meets targets
- Anomaly detection meets targets
- Simulation safety verified
- Timeline correctness verified
- Security checklist clean
