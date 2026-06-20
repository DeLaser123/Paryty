# Step 4: Connect Agents — Enterprise Specification

**Document Version:** 1.0  
**Date:** June 18, 2026  
**Status:** Specification  

---

## 1. Current State Audit

### 1.1 What Exists

| Component | File | Status |
|-----------|------|--------|
| AgentsPage | `frontend/src/pages/AgentsPage.tsx` | Table, create modal, setup wizard |
| AgentDetailModal | `frontend/src/components/agent/AgentDetailModal.tsx` | Lifecycle views |
| Agent endpoints | `cluster/internal/api/query/rest.go` | Full CRUD + lifecycle |
| API Key Manager | `cluster/internal/controlplane/apikey.go` | Generate, validate, revoke, rotate |
| Agent proto | `proto/paryty/v1/agent.proto` | Registration, config, metrics |

### 1.2 Critical Gaps

| Gap | Severity | Description |
|-----|----------|-------------|
| Download URLs are fake | Critical | `https://get.paryty.io/agent.sh` — placeholders |
| No binary hosting | Critical | No agent binaries available for download |
| No API key in wizard | Critical | Shows `<YOUR_API_KEY>` placeholder |
| Agent creation doesn't auto-generate API key | Critical | User must create API key separately via API key endpoints |
| API key endpoints exist but not in wizard | High | POST /api/v1/api-keys, GET /api/v1/api-keys, PUT /api/v1/api-keys/:key_id/rotate, DELETE /api/v1/api-keys/:key_id |
| No agent-to-twin assignment | High | Setup wizard doesn't offer twin selection |
| No real-time health monitoring | High | No polling/WebSocket for live health |
| No configuration management | Medium | No UI to push config to agents |

---

## 2. Target State — Agent Lifecycle

```
CREATE AGENT → Generate API Key → Show Key Once → Download Binary → Install on Host
    → Agent Connects (gRPC) → Validate Key → Update Status → Poll Pairing Status
    → Show Success → Manage Lifecycle (pair/unpair/retire/blacklist)
```

### State Machine

```
UNCONFIGURED → ACTIVE → LOST → RETIRED
     ↓           ↓        ↓
     └─────────→ ROGUE ←──┘
                  ↓
            BLACKLISTED
```

---

## 3. UI Specification

### 3.1 Agents Page — Table

| Column | Sortable | Data |
|--------|----------|------|
| Name | ✓ | agent.name |
| Agent ID | ✗ | agent.agent_id (monospace, truncated) |
| Status | ✓ | Badge with color |
| OS | ✓ | os/arch |
| Location | ✓ | location or cloud_provider |
| Last Seen | ✓ | Clock icon + locale string |
| Actions | ✗ | Lifecycle buttons |

### 3.2 Create Agent Modal

```tsx
<div className="aef-container-card" style={{ width: 480 }}>
  <div className="aef-container-card__header">
    <h3>Create Edge Agent</h3>
  </div>
  <div className="aef-container-card__body">
    <div className="dp-field">
      <label className="dp-field__label">Agent Name</label>
      <input className="dp-field__input" placeholder="e.g., web-server-01" />
    </div>
    <div className="dp-field">
      <label className="dp-field__label">Description</label>
      <textarea className="dp-field__input" />
    </div>
    {/* OS Selection Cards */}
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 'var(--aef-space-3)' }}>
      <button className="os-card"><Monitor size={32} /> Windows</button>
      <button className="os-card"><Terminal size={32} /> Linux</button>
    </div>
  </div>
  <div className="aef-container-card__body">
    <button className="aef-btn aef-btn-inactive">Cancel</button>
    <button className="aef-btn aef-btn-active">Create Edge Agent</button>
  </div>
</div>
```

### 3.3 API Key Display (One-Time)

```tsx
<div className="aef-container-card" style={{ borderColor: 'var(--aef-status-warning)' }}>
  <div className="aef-container-card__body">
    <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
      <AlertTriangle size={16} style={{ color: 'var(--aef-status-warning)' }} />
      <span>Save Your API Key</span>
    </div>
    <p>Save this key securely. It won't be shown again.</p>
    <div style={{
      background: 'var(--aef-border)',
      borderRadius: 'var(--aef-radius-md)',
      padding: 'var(--aef-space-3)',
      fontFamily: 'monospace',
      fontSize: 12,
      wordBreak: 'break-all',
    }}>
      {apiKey}
    </div>
    <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
      <button className="aef-btn aef-btn-active" onClick={handleCopy}>
        {copied ? 'Copied!' : 'Copy'}
      </button>
      <button className="aef-btn aef-btn-inactive" onClick={() => setMasked(!masked)}>
        {masked ? 'Show' : 'Hide'}
      </button>
    </div>
  </div>
</div>
```

### 3.4 OS-Specific Setup Wizard

**Steps:** Select OS → Setup → Verify Connection

**Windows:**
```powershell
Invoke-WebRequest -Uri "${API_BASE}/api/v1/agents/download/windows" -OutFile "paryty-agent.exe"
.\paryty-agent.exe --key "${apiKey}" --cluster-agent-id "${clusterAgentId}"
```

**Linux:**
```bash
curl -fsSL ${API_BASE}/api/v1/agents/download/linux -o paryty-agent
chmod +x paryty-agent
./paryty-agent --key "${apiKey}" --cluster-agent-id "${clusterAgentId}"
```

### 3.5 Pairing Status Polling

Poll every 5 seconds: `GET /api/v1/agents/:id/pairing-status`

**Visual States:**
- Polling: Spinner + "Waiting for agent to connect..."
- Success: Green checkmark + "Connected!" + details (hostname, OS, paired_at)
- Error: Red X + "Connection failed"

### 3.6 Agent Detail Modal

Info grid: Agent ID, Hostname, OS/Arch, Last Seen, CPU Usage, Memory Usage, Uptime, Status, Identity Token.

### 3.7 Lifecycle Actions

| State | Available Actions |
|-------|-------------------|
| Unconfigured | Delete |
| Active | Unpair, Retire, Blacklist, Unregister |
| Lost | Retire, Blacklist |
| Retired | None (read-only) |
| Blacklisted | None (read-only) |

Each action has a confirmation dialog. Blacklist requires a reason.

---

## 4. API Contract

### POST /api/v1/agents
**Request:** `{ "name": "web-server-01", "os": "linux" }`
**Response (201):** `{ "data": { "agent_id": "uuid", "name": "...", "status": "unconfigured", "api_key": "pk_live_...", "api_key_id": "uuid" } }`

### GET /api/v1/agents/download/:platform
**Query:** `?key=<api_key>`
**Response:** Binary file (application/octet-stream)

### POST /api/v1/api-keys
**Request:** `{ "name": "Production Agent Key" }`
**Response (201):** `{ "data": { "key_id": "uuid", "api_key": "pk_live_...", "key_prefix": "pk_live_a1b2c3d4" } }`

### POST /api/v1/api-keys/:key_id/rotate
**Response (200):** `{ "data": { "key_id": "uuid", "api_key": "pk_live_new_..." } }`

---

## 5. Agent Binary Distribution

### Binary Directory Structure
```
bin/agents/
├── paryty-agent-windows-amd64.exe
├── paryty-agent-linux-amd64
├── paryty-agent-linux-arm64
└── checksums.sha256
```

### Download Handler
```go
func (s *QueryService) DownloadAgentBinary(c *gin.Context) {
  platform := c.Param("platform")
  apiKey := c.Query("key")
  
  // Validate API key
  tenantID, err := s.apiKeyManager.ValidateKey(c.Request.Context(), apiKey)
  if err != nil {
    c.JSON(http.StatusUnauthorized, gin.H{"message": "Invalid API key"})
    return
  }
  
  // Serve binary from bin/agents/
  path := filepath.Join(binaryDir, filename)
  c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
  c.File(path)
}
```

---

## 6. Acceptance Criteria

| AC | Criterion |
|----|-----------|
| AC-01 | Agent binary downloads successfully |
| AC-02 | API key generated and shown once |
| AC-03 | Pairing completes when agent connects |
| AC-04 | Status updates in real-time (polling) |
| AC-05 | Lifecycle actions work (retire, blacklist, unpair) |
| AC-06 | Blacklist requires reason |
| AC-07 | Table sorting works |
| AC-08 | Agent detail shows health metrics |

---

## 7. Implementation Tasks

### Backend (Critical)

| Task | File | Description |
|------|------|-------------|
| B1 | rest.go | Add POST /api/v1/agents endpoint |
| B2 | rest.go | Add API key CRUD endpoints |
| B3 | rest.go | Implement DownloadAgentBinary with key validation |
| B4 | CI/CD | Build agent binaries (Windows/Linux) |
| B5 | Infrastructure | Host binaries in bin/agents/ |

### Frontend (Critical)

| Task | File | Description |
|------|------|-------------|
| F1 | New: ApiKeyDisplay.tsx | One-time key display component |
| F2 | AgentsPage.tsx | Update create modal to show API key |
| F3 | AgentsPage.tsx | Add download buttons |
| F4 | AgentsPage.tsx | Implement table sorting |
| F5 | New: ApiKeyManager.tsx | Key management component |

### Agent

| Task | File | Description |
|------|------|-------------|
| A1 | agent/src/auth.rs | Implement API key authentication |
| A2 | agent/src/grpc.rs | gRPC registration with key |
