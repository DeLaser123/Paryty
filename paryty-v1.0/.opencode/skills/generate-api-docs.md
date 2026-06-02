# Generate API Docs — API Documentation Generation

## Purpose
Generate and validate API documentation for all Paryty interfaces: REST API, gRPC API, and SDK documentation.

## Execution Steps

### Step 1: Generate OpenAPI Spec (REST)
Check: OpenAPI spec exists at `docs/api/openapi.yaml`
Check: All REST endpoints documented
Check: Request/response schemas defined
Check: Error responses documented

### Step 2: Generate gRPC Documentation
Check: Proto files have comments
Check: Service methods documented
Check: Message fields documented
Generate: `protoc --doc_out=docs/api/grpc --doc_opt=html,index.html *.proto`

### Step 3: Generate SDK Documentation
Run: `go doc -all ./agent/go_sdk/paryty/ > docs/api/sdk/go.md`
Check: All public functions documented
Check: Examples included

### Step 4: Generate Changelog
Check: Conventional commits used
Generate: `git log --oneline --no-merges > CHANGELOG.md`
Format: Group by feat/fix/docs/chore

### Step 5: Validate Documentation
Check: All endpoints have examples
Check: All error codes documented
Check: Authentication documented
Check: Rate limiting documented

## Documentation Structure

```
docs/api/
├── openapi.yaml          — REST API specification
├── grpc/
│   └── index.html        — gRPC API documentation
├── sdk/
│   ├── go.md             — Go SDK documentation
│   └── examples/         — SDK usage examples
├── errors.md             — Error code reference
└── changelog.md          — API changelog
```

## OpenAPI Template
```yaml
openapi: 3.0.3
info:
  title: Paryty API
  version: 1.0.0
  description: Paryty Observability Platform API
paths:
  /api/v1/topology:
    get:
      summary: List topology
      parameters:
        - name: tenant_id
          in: query
          required: true
      responses:
        '200':
          description: Topology graph
        '401':
          description: Unauthorized
        '500':
          description: Internal server error
```

## Exit Protocol
- ALL docs generated: Report "Documentation generated successfully"
- Missing endpoint: Report endpoint path, STOP
- Invalid schema: Report schema error, STOP
- Missing examples: Report endpoint, STOP

## Notes
- Run after any API change
- Documentation must be updated with code
- Examples must be tested
