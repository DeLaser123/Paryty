/**
 * useDashboardPolling — React hook for live Dashboard data.
 *
 * Fetches twins (30s), agents (15s), and alerts (15s) on mount
 * and at their respective refresh intervals.
 *
 * Uses individual Zustand selectors for stable action references
 * to avoid infinite re-render loops.
 *
 * @module hooks/useDashboardPolling
 */

import { useEffect, useRef } from 'react';
import { useDashboardStore } from '../stores/dashboardStore';
import { useAlertsStore } from '../stores/alertsStore';

/** Polling intervals in milliseconds. */
const TWINS_INTERVAL = 30_000;
const AGENTS_INTERVAL = 15_000;
const ALERTS_INTERVAL = 15_000;

/**
 * Hook that manages the dashboard data lifecycle.
 *
 * Immediately fetches all data categories on mount, then
 * continues polling at separate intervals. All intervals
 * are cleaned up on unmount.
 */
export function useDashboardPolling() {
  const fetchTwins = useDashboardStore((s) => s.fetchTwins);
  const fetchAgents = useDashboardStore((s) => s.fetchAgents);
  const fetchAlerts = useAlertsStore((s) => s.fetchAlerts);

  const twinsRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const agentsRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const alertsRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    // Immediate first fetch for all categories
    fetchTwins();
    fetchAgents();
    fetchAlerts();

    // Start polling
    twinsRef.current = setInterval(fetchTwins, TWINS_INTERVAL);
    agentsRef.current = setInterval(fetchAgents, AGENTS_INTERVAL);
    alertsRef.current = setInterval(fetchAlerts, ALERTS_INTERVAL);

    return () => {
      if (twinsRef.current) clearInterval(twinsRef.current);
      if (agentsRef.current) clearInterval(agentsRef.current);
      if (alertsRef.current) clearInterval(alertsRef.current);
    };
  }, [fetchTwins, fetchAgents, fetchAlerts]);
}
