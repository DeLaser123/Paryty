/**
 * TwinListPage — catalogue of all Digital Paryty twins for the current tenant.
 *
 * Renders a grid of TwinCard components with search, status filtering,
 * and a "Create New Twin" action button. Fetches data from the twins API
 * on mount and supports pagination.
 *
 * @module pages/Twins/TwinListPage
 */

import { useState, useEffect, useCallback, memo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Search, RefreshCw } from 'lucide-react';
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

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Lists all Digital Paryty twins with search and filter controls.
 *
 * Uses the `dp-page` layout pattern and `aef-container-card` for sections.
 * Fetches twins from the API on mount and provides a manual refresh button.
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
                size={14}
                className={isLoading ? 'animate-spin' : undefined}
              />
            </button>
            <button
              type="button"
              className="aef-btn aef-btn-active"
              onClick={handleCreate}
              data-testid="twin-list-create"
            >
              <Plus size={14} /> New Twin
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
              minWidth: 200,
              maxWidth: 360,
              background: 'var(--aef-surface)',
              border: 'var(--aef-border-width) solid var(--aef-border)',
              borderRadius: 'var(--aef-radius-control)',
              padding: '0 var(--aef-space-3)',
            }}
          >
            <Search size={13} style={{ color: 'var(--aef-text-secondary)', flexShrink: 0 }} />
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
                fontSize: 12,
                color: 'var(--aef-text-primary)',
                padding: '8px 0',
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

        {/* Loading state */}
        {isLoading && twins.length === 0 && (
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              fontSize: 12,
              color: 'var(--aef-text-secondary)',
            }}
          >
            Loading twins…
          </div>
        )}

        {/* Error state */}
        {error && !isLoading && twins.length === 0 && (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-6)',
              textAlign: 'center',
            }}
          >
            <p
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 12,
                color: 'var(--aef-error)',
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
        )}

        {/* Empty state */}
        {!isLoading && !error && twins.length === 0 && (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-8)',
              textAlign: 'center',
            }}
          >
            <p
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 13,
                color: 'var(--aef-text-secondary)',
                margin: 0,
                marginBottom: 'var(--aef-space-4)',
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
                <Plus size={14} /> Create First Twin
              </button>
            )}
          </div>
        )}

        {/* Twin cards grid */}
        {twins.length > 0 && (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
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
              style={{ opacity: page <= 1 ? 0.4 : 1 }}
            >
              Previous
            </button>
            <span
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 11,
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
              style={{ opacity: page >= totalPages ? 0.4 : 1 }}
            >
              Next
            </button>
          </div>
        )}
      </div>
    </div>
  );
});
