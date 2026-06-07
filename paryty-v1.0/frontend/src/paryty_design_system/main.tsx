/**
 * Module: main (paryty_design_system entry)
 *
 * Responsibility: Application root. Renders the Dashboard by default.
 *   A ?ds=1 query parameter switches to the design system showcase.
 * Out of scope: state stores, network workers, simulation physics.
 */

import '@fontsource-variable/geist/index.css';

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { Dashboard } from '../dashboard/Dashboard';
import { DesignSystemPage } from './DesignSystemPage';

const showDesignSystem = new URLSearchParams(window.location.search).get('ds') === '1';

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('Root element #root not found');

createRoot(rootEl).render(
  <StrictMode>
    {showDesignSystem ? <DesignSystemPage /> : <Dashboard />}
  </StrictMode>,
);