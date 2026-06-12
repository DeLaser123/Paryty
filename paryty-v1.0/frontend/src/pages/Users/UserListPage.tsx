/**
 * UserListPage — list all sub-users for the current tenant.
 *
 * Renders a table of users with their roles, search functionality,
 * and actions for editing and deleting users. Admin-only page with
 * role-based access control.
 *
 * @module pages/Users/UserListPage
 */

import { useState, useEffect, useCallback, memo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, Search, RefreshCw, Users, Shield, Edit, Trash2 } from 'lucide-react';
import { useAuthStore } from '../../stores/authStore';
import { useToastStore } from '../../stores/toastStore';
import { fetchUsers } from '../../api/users';
import type { SubUser } from '../../types/auth';
import { UserDeleteDialog } from './UserDeleteDialog';

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Lists all sub-users for the current tenant with search and actions.
 *
 * Uses the `dp-page` layout pattern and `aef-container-card` for sections.
 * Fetches users from the API on mount and provides a manual refresh button.
 */
export const UserListPage = memo(function UserListPage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const user = useAuthStore((s) => s.user);

  const [users, setUsers] = useState<SubUser[]>([]);
  const [total, setTotal] = useState(0);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [page, setPage] = useState(1);
  const [deleteUser, setDeleteUser] = useState<SubUser | null>(null);
  const PAGE_SIZE = 20;

  const isAdmin = user?.role === 'admin';

  // ─── Data Fetching ──────────────────────────────────────────────────

  const loadUsers = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await fetchUsers({
        page,
        limit: PAGE_SIZE,
        search: searchQuery.trim() || undefined,
      });
      setUsers(response.users);
      setTotal(response.total);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load users.';
      setError(message);
      addToast({ type: 'error', message });
    } finally {
      setIsLoading(false);
    }
  }, [page, searchQuery, addToast]);

  useEffect(() => {
    if (isAdmin) {
      loadUsers();
    }
  }, [isAdmin, loadUsers]);

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleSearchChange = useCallback((value: string) => {
    setSearchQuery(value);
    setPage(1);
  }, []);

  const handleRefresh = useCallback(() => {
    loadUsers();
  }, [loadUsers]);

  const handleCreate = useCallback(() => {
    navigate('/users/new');
  }, [navigate]);

  const handleEdit = useCallback((userId: string) => {
    navigate(`/users/${userId}/edit`);
  }, [navigate]);

  const handleDelete = useCallback((user: SubUser) => {
    setDeleteUser(user);
  }, []);

  const handleDeleteSuccess = useCallback(() => {
    setDeleteUser(null);
    loadUsers();
  }, [loadUsers]);

  const totalPages = Math.ceil(total / PAGE_SIZE);

  // ─── Access Control ─────────────────────────────────────────────────

  if (!isAdmin) {
    return (
      <div className="dp-page" data-testid="user-list-page-unauthorized">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <h1 className="dp-header__title">Users</h1>
              <p className="dp-header__sub">Only tenant admins can manage users.</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="user-list-page">
      <div className="dp-page__inner">
        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <h1 className="dp-header__title">Users</h1>
            <p className="dp-header__sub">
              {total} user{total !== 1 ? 's' : ''} total
            </p>
          </div>
          <div className="dp-header__actions">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleRefresh}
              disabled={isLoading}
              aria-label="Refresh user list"
              data-testid="user-list-refresh"
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
              data-testid="user-list-create"
            >
              <Plus size={14} /> New User
            </button>
          </div>
        </div>

        {/* Search bar */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--aef-space-3)',
            marginBottom: 'var(--aef-space-4)',
            flexWrap: 'wrap',
          }}
          data-testid="user-list-filters"
        >
          <div
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 'var(--aef-space-2)',
              flex: 1,
              minWidth: 200,
              background: 'var(--aef-surface)',
              border: 'var(--aef-border-width) solid var(--aef-border)',
              borderRadius: 'var(--aef-radius-control)',
              padding: '0 var(--aef-space-3)',
            }}
          >
            <Search size={13} style={{ color: 'var(--aef-text-secondary)', flexShrink: 0 }} />
            <input
              type="text"
              className="dp-field__input"
              placeholder="Search users by name or email…"
              value={searchQuery}
              onChange={(e) => handleSearchChange(e.target.value)}
              style={{
                border: 'none',
                background: 'transparent',
                fontFamily: 'var(--aef-font-body)',
                fontSize: 12,
                color: 'var(--aef-text-primary)',
                padding: 'var(--aef-space-2) 0',
              }}
              data-testid="user-list-search"
            />
          </div>
        </div>

        {/* Content */}
        {error ? (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              textAlign: 'center',
            }}
            data-testid="user-list-error"
          >
            <p style={{ color: 'var(--aef-error)', marginBottom: 'var(--aef-space-4)' }}>
              {error}
            </p>
            <button
              type="button"
              className="aef-btn aef-btn-active"
              onClick={handleRefresh}
            >
              <RefreshCw size={14} /> Retry
            </button>
          </div>
        ) : isLoading ? (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              textAlign: 'center',
              color: 'var(--aef-text-secondary)',
            }}
            data-testid="user-list-loading"
          >
            <p>Loading users…</p>
          </div>
        ) : users.length === 0 ? (
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              textAlign: 'center',
              color: 'var(--aef-text-secondary)',
            }}
            data-testid="user-list-empty"
          >
            <Users size={32} style={{ marginBottom: 'var(--aef-space-4)', opacity: 0.5 }} />
            <p>No users found.</p>
            <button
              type="button"
              className="aef-btn aef-btn-active"
              onClick={handleCreate}
              style={{ marginTop: 'var(--aef-space-4)' }}
            >
              <Plus size={14} /> Create User
            </button>
          </div>
        ) : (
          <div className="aef-container-card" data-testid="user-list-table">
            <table style={{ width: '100%', borderCollapse: 'collapse' }}>
              <thead>
                <tr
                  style={{
                    borderBottom: 'var(--aef-border-width) solid var(--aef-border)',
                  }}
                >
                  <th
                    style={{
                      textAlign: 'left',
                      padding: 'var(--aef-space-3) var(--aef-space-4)',
                      fontFamily: 'var(--aef-font-body)',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--aef-text-secondary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                    }}
                  >
                    User
                  </th>
                  <th
                    style={{
                      textAlign: 'left',
                      padding: 'var(--aef-space-3) var(--aef-space-4)',
                      fontFamily: 'var(--aef-font-body)',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--aef-text-secondary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                    }}
                  >
                    Email
                  </th>
                  <th
                    style={{
                      textAlign: 'left',
                      padding: 'var(--aef-space-3) var(--aef-space-4)',
                      fontFamily: 'var(--aef-font-body)',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--aef-text-secondary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                    }}
                  >
                    Role
                  </th>
                  <th
                    style={{
                      textAlign: 'left',
                      padding: 'var(--aef-space-3) var(--aef-space-4)',
                      fontFamily: 'var(--aef-font-body)',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--aef-text-secondary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                    }}
                  >
                    Created
                  </th>
                  <th
                    style={{
                      textAlign: 'right',
                      padding: 'var(--aef-space-3) var(--aef-space-4)',
                      fontFamily: 'var(--aef-font-body)',
                      fontSize: 11,
                      fontWeight: 600,
                      color: 'var(--aef-text-secondary)',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                    }}
                  >
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody>
                {users.map((u) => (
                  <tr
                    key={u.id}
                    style={{
                      borderBottom: 'var(--aef-border-width) solid var(--aef-border)',
                    }}
                  >
                    <td
                      style={{
                        padding: 'var(--aef-space-3) var(--aef-space-4)',
                        fontFamily: 'var(--aef-font-body)',
                        fontSize: 12,
                        color: 'var(--aef-text-primary)',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
                        <Users size={14} style={{ color: 'var(--aef-text-secondary)' }} />
                        {u.name}
                      </div>
                    </td>
                    <td
                      style={{
                        padding: 'var(--aef-space-3) var(--aef-space-4)',
                        fontFamily: 'var(--aef-font-body)',
                        fontSize: 12,
                        color: 'var(--aef-text-secondary)',
                      }}
                    >
                      {u.email}
                    </td>
                    <td
                      style={{
                        padding: 'var(--aef-space-3) var(--aef-space-4)',
                      }}
                    >
                      <span
                        style={{
                          display: 'inline-flex',
                          alignItems: 'center',
                          gap: 'var(--aef-space-1)',
                          padding: '2px 8px',
                          borderRadius: 'var(--aef-radius-full)',
                          background: 'var(--aef-surface-low)',
                          fontFamily: 'var(--aef-font-body)',
                          fontSize: 11,
                          color: 'var(--aef-text-secondary)',
                          textTransform: 'capitalize',
                        }}
                      >
                        <Shield size={10} /> {u.role}
                      </span>
                    </td>
                    <td
                      style={{
                        padding: 'var(--aef-space-3) var(--aef-space-4)',
                        fontFamily: 'var(--aef-font-body)',
                        fontSize: 12,
                        color: 'var(--aef-text-secondary)',
                      }}
                    >
                      {new Date(u.createdAt).toLocaleDateString()}
                    </td>
                    <td
                      style={{
                        padding: 'var(--aef-space-3) var(--aef-space-4)',
                        textAlign: 'right',
                      }}
                    >
                      <div style={{ display: 'flex', gap: 'var(--aef-space-2)', justifyContent: 'flex-end' }}>
                        <button
                          type="button"
                          className="aef-btn aef-btn-inactive"
                          onClick={() => handleEdit(u.id)}
                          aria-label={`Edit user ${u.name}`}
                          style={{ padding: '2px 6px' }}
                          data-testid={`user-edit-${u.id}`}
                        >
                          <Edit size={12} />
                        </button>
                        <button
                          type="button"
                          className="aef-btn aef-btn-inactive"
                          onClick={() => handleDelete(u)}
                          aria-label={`Delete user ${u.name}`}
                          style={{ padding: '2px 6px' }}
                          data-testid={`user-delete-${u.id}`}
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {/* Pagination */}
        {totalPages > 1 && (
          <div
            style={{
              display: 'flex',
              justifyContent: 'center',
              gap: 'var(--aef-space-2)',
              marginTop: 'var(--aef-space-4)',
            }}
            data-testid="user-list-pagination"
          >
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
            >
              Previous
            </button>
            <span
              style={{
                display: 'flex',
                alignItems: 'center',
                padding: '0 var(--aef-space-3)',
                fontFamily: 'var(--aef-font-body)',
                fontSize: 12,
                color: 'var(--aef-text-secondary)',
              }}
            >
              Page {page} of {totalPages}
            </span>
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page === totalPages}
            >
              Next
            </button>
          </div>
        )}

        {/* Delete dialog */}
        {deleteUser && (
          <UserDeleteDialog
            userId={deleteUser.id}
            userName={deleteUser.name}
            onClose={() => setDeleteUser(null)}
            onDeleted={handleDeleteSuccess}
          />
        )}
      </div>
    </div>
  );
});
