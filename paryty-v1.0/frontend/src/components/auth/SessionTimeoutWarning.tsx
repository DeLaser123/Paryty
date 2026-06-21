/**
 * SessionTimeoutWarning — modal that appears 2 minutes before the access token expires.
 *
 * Shows a countdown timer and gives the user the option to stay logged in
 * (triggers a silent token refresh) or log out.
 *
 * @module components/auth/SessionTimeoutWarning
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import { Clock, LogOut, RefreshCw } from 'lucide-react';
import { useAuthStore } from '../../stores/authStore';

const WARNING_BEFORE_EXPIRY_MS = 120_000; // 2 minutes
const COUNTDOWN_INTERVAL_MS = 1_000;

export function SessionTimeoutWarning() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const expiresAt = useAuthStore((s) => s.expiresAt);
  const refreshAuth = useAuthStore((s) => s.refreshAuth);
  const logout = useAuthStore((s) => s.logout);

  const [isVisible, setIsVisible] = useState(false);
  const [countdown, setCountdown] = useState(0);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const checkTimer = useRef<ReturnType<typeof setInterval>>();
  const countdownTimer = useRef<ReturnType<typeof setInterval>>();

  const clearTimers = useCallback(() => {
    if (checkTimer.current) clearInterval(checkTimer.current);
    if (countdownTimer.current) clearInterval(countdownTimer.current);
  }, []);

  // Monitor token expiry and show warning when appropriate.
  useEffect(() => {
    if (!isAuthenticated || !expiresAt) {
      setIsVisible(false);
      clearTimers();
      return;
    }

    const checkExpiry = () => {
      const expiryMs = new Date(expiresAt).getTime();
      const remaining = expiryMs - Date.now();

      if (remaining <= 0) {
        // Token already expired — logout.
        setIsVisible(false);
        clearTimers();
        logout();
        return;
      }

      if (remaining <= WARNING_BEFORE_EXPIRY_MS) {
        setIsVisible(true);
        setCountdown(Math.ceil(remaining / 1000));
      } else {
        setIsVisible(false);
      }
    };

    // Check every second.
    checkTimer.current = setInterval(checkExpiry, COUNTDOWN_INTERVAL_MS);
    checkExpiry(); // Initial check.

    return () => clearTimers();
  }, [isAuthenticated, expiresAt, logout, clearTimers]);

  // Countdown timer when modal is visible.
  useEffect(() => {
    if (!isVisible) {
      if (countdownTimer.current) clearInterval(countdownTimer.current);
      return;
    }

    countdownTimer.current = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          // Countdown reached zero — auto-logout.
          clearInterval(countdownTimer.current);
          logout();
          return 0;
        }
        return prev - 1;
      });
    }, COUNTDOWN_INTERVAL_MS);

    return () => {
      if (countdownTimer.current) clearInterval(countdownTimer.current);
    };
  }, [isVisible, logout]);

  const handleStayLoggedIn = useCallback(async () => {
    setIsRefreshing(true);
    const success = await refreshAuth();
    setIsRefreshing(false);
    if (success) {
      setIsVisible(false);
    } else {
      logout();
    }
  }, [refreshAuth, logout]);

  const handleLogout = useCallback(() => {
    clearTimers();
    setIsVisible(false);
    logout();
  }, [logout, clearTimers]);

  if (!isVisible) return null;

  const minutes = Math.floor(countdown / 60);
  const seconds = countdown % 60;
  const timeDisplay = minutes > 0
    ? `${minutes}m ${seconds.toString().padStart(2, '0')}s`
    : `${seconds}s`;

  return (
    <div className="aef-modal-overlay" data-testid="session-timeout-warning">
      <div className="aef-modal" style={{ maxWidth: 420 }}>
        <div className="aef-modal-header">
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
            <Clock size={14} style={{ color: 'var(--aef-text-secondary)' }} />
            <span className="aef-modal-title">Session Expiring</span>
          </div>
        </div>
        <div className="aef-modal-body">
          <p className="aef-modal-para">
            Your session will expire in{' '}
            <strong style={{ color: 'var(--aef-text-primary)' }}>{timeDisplay}</strong>.
          </p>
          <p className="aef-modal-para">
            Would you like to stay logged in?
          </p>
        </div>
        <div className="aef-modal-footer">
          <button
            type="button"
            className="aef-btn aef-btn-inactive"
            onClick={handleLogout}
            data-testid="session-timeout-logout"
          >
            <LogOut size={14} /> Log Out
          </button>
          <button
            type="button"
            className="aef-btn aef-btn-active"
            onClick={handleStayLoggedIn}
            disabled={isRefreshing}
            data-testid="session-timeout-stay"
          >
            <RefreshCw size={14} style={isRefreshing ? { animation: 'aef-spin 0.8s linear infinite' } : undefined} />
            {isRefreshing ? 'Refreshing…' : 'Stay Logged In'}
          </button>
        </div>
      </div>
    </div>
  );
}
