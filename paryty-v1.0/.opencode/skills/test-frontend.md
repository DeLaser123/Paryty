# Test Frontend — Paryty Frontend Comprehensive Testing

## Purpose
Run the full frontend testing pipeline. Covers unit tests, component tests, integration tests, E2E tests, and coverage reporting.

## Prerequisites
Install test dependencies (run once):
```bash
npm install -D vitest @testing-library/react @testing-library/jest-dom @testing-library/user-event jsdom @playwright/test msw
```

## Execution Steps

### Step 1: Unit Tests (Vitest)
Run: npx vitest run 2>&1
Expected: All unit tests pass.

### Step 2: Unit Tests with Coverage
Run: npx vitest run --coverage 2>&1
Expected: Coverage >= 75% for stores, hooks, and utils.

### Step 3: Component Tests
Run: npx vitest run --reporter=verbose src/components/ 2>&1
Expected: All component tests pass. No console errors.

### Step 4: E2E Tests (Playwright)
Run: npx playwright install chromium 2>&1
Run: npx playwright test 2>&1
Expected: All E2E tests pass.

### Step 5: Type Check
Run: npx tsc --noEmit 2>&1
Expected: Zero type errors.

### Step 6: Lint
Run: npx eslint src/ 2>&1
Expected: Zero errors (warnings OK but report them).

### Step 7: Build
Run: npm run build 2>&1
Expected: Successful production build.

## Test Patterns

### Unit Test Structure (Vitest)
```typescript
import { describe, it, expect } from 'vitest';
import { useTopologyStore } from '../stores/topologyStore';

describe('topologyStore', () => {
  it('should add a node', () => {
    const { addNode, nodes } = useTopologyStore.getState();
    addNode({ id: '1', name: 'service-a', type: 'service' });
    expect(nodes).toHaveLength(1);
    expect(nodes[0].id).toBe('1');
  });
});
```

### Component Test Structure (React Testing Library)
```typescript
import { render, screen, fireEvent } from '@testing-library/react';
import { TopologyView } from '../components/TopologyView';

describe('TopologyView', () => {
  it('should render nodes', () => {
    render(<TopologyView nodes={mockNodes} edges={mockEdges} />);
    expect(screen.getByTestId('topology-canvas')).toBeInTheDocument();
  });

  it('should handle node click', () => {
    const onSelect = vi.fn();
    render(<TopologyView nodes={mockNodes} onNodeSelect={onSelect} />);
    fireEvent.click(screen.getByTestId('node-1'));
    expect(onSelect).toHaveBeenCalledWith('1');
  });
});
```

### E2E Test Structure (Playwright)
```typescript
import { test, expect } from '@playwright/test';

test('topology loads and displays nodes', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('[data-testid="topology-canvas"]')).toBeVisible();
  await expect(page.locator('[data-testid="node-count"]')).toHaveText('5');
});
```

### MSW Mock Pattern
```typescript
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

const server = setupServer(
  http.get('/api/topology', () => {
    return HttpResponse.json({ nodes: [], edges: [] });
  })
);

beforeAll(() => server.listen());
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
```

## Exit Protocol
- ALL gates pass: Report "All testing gates passed"
- Unit test fails: Report test name, assertion, and file:line, STOP
- Component test fails: Report component and interaction details, STOP
- E2E test fails: Report test name, screenshot path, and error, STOP
- Type errors: Report file:line and error message, STOP
- ESLint errors: Report file:line and rule violation, STOP
- Build failure: Report full error output, STOP
- Coverage below 75%: Report which modules are below threshold, STOP

## Coverage Requirements
| Module | Minimum Coverage |
|--------|-----------------|
| stores/ | 80% |
| hooks/ | 75% |
| utils/ | 85% |
| components/ | 70% |
| api/ | 80% |

## Notes
- Must run from frontend/ directory
- E2E tests require Playwright browsers installed
- MSW intercepts API calls for deterministic testing
- WebGPU tests are advisory on non-WebGPU systems
