# Kilo Code run script for Paryty project (Windows)
# Runs the Paryty development environment when clicking "Run" in Agent Manager

param(
    [string]$WORKTREE_PATH,
    [string]$REPO_PATH
)

Write-Host "=== Paryty Run Script ==="
Write-Host "Starting Paryty development environment..."
Write-Host "Worktree: $WORKTREE_PATH"
Write-Host "Repo:     $REPO_PATH"

# Determine which services to start
# By default, start the frontend dev server and cluster services
Write-Host ""
Write-Host "To start individual services:"
Write-Host "  Frontend:  cd frontend && npm run dev"
Write-Host "  Cluster:   cd cluster && go run ./cmd/pipeline/..."
Write-Host "  Agent:     cd agent && cargo run"
Write-Host ""
Write-Host "For full stack, use: podman-compose up"
Write-Host "=== Ready ==="
