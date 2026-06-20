/**
 * TwinListPage — catalogue of all Digital Paryty twins for the current tenant.
 *
 * Renders a grid of TwinCard components with search, status filtering,
 * and a "Create New Twin" action button. DS typography scale, stagger
 * entrance animations on the card grid, and aef-counter for stats.
 *
 * @module pages/Twins/TwinListPage
 */

import { useState, useEffect, useCallback, memo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Search, RefreshCw, Layers } from 'lucide-react';
import { useToastStore } from '../../stores/toastStore';
import { fetchTwins } from '../../api/twins';
import type { TwinDetails } from '../../types/digitalParyty';
import { TwinCard } from '../../components/Twin/TwinCard';
import { ParytySelect } from '../../components/common/ParytySelect';

// ─── Status Filter Options ──────────────────────────────────────────────────

const STATUS_FILTERS = [
  { label: 'All statuses', value: '' },
  { label: 'Healthy', value: 'healthy' },
  { label: 'Degraded', value: 'degraded' },
  { label: 'Unhealthy', value: 'unhealthy' },
];

// ─── Stagger animation CSS (injected once) ──────────────────────────────────

const STAGGER_STYLE_ID = 'twin-list-stagger';

function ensureStaggerStyle() {
  if (typeof document === 'undefined') return;
  if (document.getElementById(STAGGER_STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STAGGER_STYLE_ID;
  style.textContent = `
    .twin-card-grid > .aef-container-card {
      animation: aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) both;
    }
    .twin-card-grid > .aef-container-card:nth-child(1) { animation-delay: 0ms; }
    .twin-card-grid > .aef-container-card:nth-child(2) { animation-delay: 40ms; }
    .twin-card-grid > .aef-container-card:nth-child(3) { animation-delay: 80ms; }
    .twin-card-grid > .aef-container-card:nth-child(4) { animation-delay: 120ms; }
    .twin-card-grid > .aef-container-card:nth-child(5) { animation-delay: 160ms; }
    .twin-card-grid > .aef-container-card:nth-child(6) { animation-delay: 200ms; }
    .twin-card-grid > .aef-container-card:nth-child(7) { animation-delay: 240ms; }
    .twin-card-grid > .aef-container-card:nth-child(8) { animation-delay: 280ms; }
  `;
  document.head.appendChild(style);
}

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Lists all Digital Paryty twins with search and filter controls.
 *
 * Uses DS primitives for layout, typography, and interaction patterns.
 */
export const TwinListPage = memo(function TwinListPage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);

  const [twins, setTwins] = useState<TwinDetails[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [page, setPage] = useState(1);
  const PAGE_SIZE = 20;

  // Inject stagger styles
  useEffect(() => { ensureStaggerStyle(); }, []);

  // ─── Data Fetching ──────────────────────────────────────────────────

  const loadTwins = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const data = await fetchTwins({
        page,
        limit: PAGE_SIZE,
        status: statusFilter || undefined,
        search: searchQuery.trim() || undefined,
      });
      setTwins(data);
      // NOTE: API returns a flat array without total count. Use array length.
      setTotal(data.length);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load twins.';
      setError(message);
      addToast({ type: 'error', message });
    } finally {
      setIsLoading(false);
    }
  }, [page, statusFilter, searchQuery, addToast]);

  useEffect(() => {
    loadTwins();
  }, [loadTwins]);

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleSearchChange = useCallback((value: string) => {
    setSearchQuery(value);
    setPage(1);
  }, []);

  const handleStatusChange = useCallback((value: string) => {
    setStatusFilter(value);
    setPage(1);
  }, []);

  const handleRefresh = useCallback(() => {
    loadTwins();
  }, [loadTwins]);

  const handleCreate = useCallback(() => {
    navigate('/twins/new');
  }, [navigate]);

  const totalPages = Math.ceil(total / PAGE_SIZE);

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="twin-list-page">
      <div className="dp-page__inner">
        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <h1 className="dp-header__title">Digital Parytys</h1>
            <p className="dp-header__sub">
              {total} twin{total !== 1 ? 's' : ''} total
            </p>
          </div>
          <div className="dp-header__actions">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleRefresh}
              disabled={isLoading}
              aria-label="Refresh twin list"
              data-testid="twin-list-refresh"
            >
              <RefreshCw
                size={12}
                className={isLoading ? 'aef-spin' : undefined}
              />
            </button>
            <button
              type="button"
              className="aef-btn aef-btn-active"
              onClick={handleCreate}
              data-testid="twin-list-create"
            >
              <Plus size={12} /> New Twin
            </button>
          </div>
        </div>

        {/* Filters bar */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--aef-space-3)',
            marginBottom: 'var(--aef-space-4)',
            flexWrap: 'wrap',
          }}
          data-testid="twin-list-filters"
        >
          {/* Search input */}
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-2)',
              flex: 1,
              minWidth: 'var(--aef-space-12)',
              maxWidth: 'var(--aef-content-width-form)',
              background: 'var(--aef-surface-card)',
              border: 'var(--aef-border-width) solid var(--aef-border)',
              borderRadius: 'var(--aef-radius-control)',
              padding: '0 var(--aef-space-3)',
              transition: 'border-color var(--aef-duration-fast) var(--aef-ease-exit)',
            }}
          >
            <Search size={12} style={{ color: 'var(--aef-text-secondary)', flexShrink: 0 }} />
            <input
              type="text"
              placeholder="Search twins…"
              value={searchQuery}
              onChange={(e) => handleSearchChange(e.target.value)}
              style={{
                flex: 1,
                background: 'transparent',
                border: 'none',
                outline: 'none',
                fontFamily: 'var(--aef-font-body)',
                fontSize: 'var(--aef-font-size-xs)',
                color: 'var(--aef-text-primary)',
                padding: 'var(--aef-space-2) 0',
              }}
              data-testid="twin-list-search"
            />
          </div>

          {/* Status filter */}
          <ParytySelect
            options={STATUS_FILTERS}
            value={statusFilter}
            onChange={handleStatusChange}
            placeholder="All statuses"
            testId="twin-list-status"
          />
        </div>

        {/* Counter row: total twins */}
        {twins.length > 0 && (
          <div
            style={{
              display: 'flex',
              gap: 'var(--aef-space-3)',
              marginBottom: 'var(--aef-space-4)',
            }}
          >
            <div className="aef-counter aef-counter-neutral">
              <div className="aef-counter__icon"><Layers size={14} /></div>
              <div className="aef-counter__body">
                <span className="aef-counter__label">Total Twins</span>
                <span className="aef-counter__value">{total}</span>
              </div>
            </div>
          </div>
        )}

        {/* Loading state */}
        {isLoading && twins.length === 0 && (
          <div
            className="aef-viz-well"
            style={{ height: 'auto', padding: 'var(--aef-space-10)' }}
          >
            <RefreshCw size={16} className="aef-viz-well__icon aef-spin" />
            <span className="aef-viz-well__label">Loading twins…</span>
          </div>
        )}

        {/* Error state */}
        {error && !isLoading && twins.length === 0 && (
          <div className="aef-container-card">
            <div className="aef-container-card__body" style={{ alignItems: 'center', padding: 'var(--aef-space-6)' }}>
              <p
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 'var(--aef-font-size-xs)',
                  color: 'var(--aef-status-error)',
                  margin: 0,
                  marginBottom: 'var(--aef-space-3)',
                }}
              >
                {error}
              </p>
              <button
                type="button"
                className="aef-btn aef-btn-inactive"
                onClick={handleRefresh}
                data-testid="twin-list-retry"
              >
                Try again
              </button>
            </div>
          </div>
        )}

        {/* Empty state */}
        {!isLoading && !error && twins.length === 0 && (
          <div className="aef-container-card">
            <div className="aef-container-card__body" style={{ alignItems: 'center', padding: 'var(--aef-space-8)' }}>
              <p
                style={{
                  fontFamily: 'var(--aef-font-body)',
                  fontSize: 'var(--aef-font-size-xs)',
                  color: 'var(--aef-text-secondary)',
                  margin: 0,
                  marginBottom: 'var(--aef-space-4)',
                  textAlign: 'center',
                }}
              >
                {searchQuery || statusFilter
                  ? 'No twins match your filters.'
                  : 'No Digital Parytys yet. Create your first one to get started.'}
              </p>
              {!searchQuery && !statusFilter && (
                <button
                  type="button"
                  className="aef-btn aef-btn-active"
                  onClick={handleCreate}
                  data-testid="twin-list-empty-create"
                >
                  <Plus size={12} /> Create First Twin
                </button>
              )}
            </div>
          </div>
        )}

        {/* Twin cards grid with stagger animation */}
        {twins.length > 0 && (
          <div
            className="twin-card-grid"
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
              gap: 'var(--aef-space-4)',
            }}
            data-testid="twin-list-grid"
          >
            {twins.map((twin) => (
              <TwinCard key={twin.id} twin={twin} />
            ))}
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 'var(--aef-space-3)',
              marginTop: 'var(--aef-space-6)',
            }}
            data-testid="twin-list-pagination"
          >
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page <= 1}
              style={{ opacity: page <= 1 ? 'var(--aef-disabled-opacity)' : 1 }}
            >
              Previous
            </button>
            <span
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 'var(--aef-font-size-xs)',
                color: 'var(--aef-text-secondary)',
              }}
            >
              Page {page} of {totalPages}
            </span>
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              style={{ opacity: page >= totalPages ? 'var(--aef-disabled-opacity)' : 1 }}
            >
              Next
            </button>
          </div>
        )}
      </div>
    </div>
  );
});
