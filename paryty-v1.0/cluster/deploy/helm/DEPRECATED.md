# DEPRECATED — Use `deploy/helm/paryty/` instead

The Helm charts under `cluster/deploy/helm/` are deprecated as of 2026-06-21.

**Canonical Helm chart:** `deploy/helm/paryty/` — umbrella chart with subcharts per the locked architectural decision (Phase 8).

**Migration:**
- `paryty-agent` → `deploy/helm/paryty/` (add agent subchart)
- `paryty-cluster` → already covered by `deploy/helm/paryty/charts/{ingestion,pipeline,query}/`
- `paryty-frontend` → `deploy/helm/paryty/charts/frontend/`
- `paryty-storage` → add `deploy/helm/paryty/charts/storage/`
- `paryty-security` → add `deploy/helm/paryty/charts/security/`
- `paryty-monitoring` → handled by OpenTelemetry self-monitoring (dogfooding)

These charts will be removed after the migration is complete.
