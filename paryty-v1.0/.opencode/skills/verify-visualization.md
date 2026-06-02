# Verify Visualization

Verify the Visualization Features (`frontend/src/components/`) for correctness, accessibility, and security.

## Verification Steps

### 1. Type Check & Build
```bash
cd frontend && npx tsc --noEmit 2>&1
cd frontend && npm run build 2>&1
```

### 2. Component Tests
```bash
cd frontend && npm run test -- --reporter=verbose 2>&1
```
Verify:
- Visual encoding correctness (color, size, glow, particles)
- Interaction: node selection, drill-down, hover tooltip, context menu
- Search and filter functionality
- Legend rendering

### 3. Visual Regression Tests
- Snapshot comparison for each zoom level
- Color encoding: health status maps to correct color
- Animation smoothness: no stuttering
- Edge rendering with different throughput values

### 4. Accessibility Tests
- WCAG 2.1 AA compliance
- Keyboard navigation reaches all interactive elements
- Screen reader announces node selection
- Color contrast >= 4.5:1 for text
- Color-blind safe palette

### 5. Security Audit
- [ ] No XSS via topology labels (sanitized)
- [ ] No dangerouslySetInnerHTML
- [ ] CSP compatible
- [ ] No credentials in client-side state

## Pass Criteria
- Type check passes
- All tests pass
- Visual regression tests pass
- Accessibility tests pass
- Security checklist clean
