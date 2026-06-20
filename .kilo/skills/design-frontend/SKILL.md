---
name: design-frontend
description: Design frontend features for Paryty — UI mockups, component structure, state design, accessibility, and responsive layout patterns.
---
# Design Frontend — Paryty Frontend Design

## Purpose
Design frontend features and UI improvements for Paryty with a focus on GPU-accelerated topology visualization, data-dense dashboards, and TradingView-style interactive timelines.

## Design Principles
1. **Data density over whitespace.** Operators need information, not padding.
2. **Motion shows meaning.** Animate transitions between states, not decorative elements.
3. **Dark theme first.** Observability is 24/7. Light theme is secondary.
4. **Keyboard navigable.** Every action has a keyboard shortcut.
5. **Responsive to 1920x1080 minimum.** Target is operator workstations, not mobile.

## Visual Encoding
- Position → topology structure
- Color → health status (green/yellow/red)
- Size → load
- Glow → utilization intensity
- Particles → data flow rate

## Component Library Standards
- All components in TypeScript strict mode
- Zustand stores for global state
- PixiJS for GPU-accelerated rendering
- React for UI chrome (panels, modals, controls)
- Error boundaries around every visualization
