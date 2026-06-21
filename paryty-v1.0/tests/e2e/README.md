# Paryty E2E Tests

## Prerequisites
- Go 1.25+
- Paryty infrastructure running (PostgreSQL, Dragonfly, QuestDB, Redpanda)
- Paryty cluster running

## Running Tests
```bash
cd tests/e2e
go test -v -tags=e2e -timeout=5m ./...
```

## Test Scenarios
1. Auth flow: Register → Login → Token refresh → Logout
2. Twin lifecycle: Create → Read → Update → Delete
3. Agent connection: Register agent → Verify pairing → View in dashboard
4. Alert flow: Trigger alert → Acknowledge → Verify resolution
