/**
 * UserEditPage — form for editing a sub-user's role and permissions.
 *
 * Fetches the user by ID and renders a form to update their name and role.
 * Admin-only page with role-based access control.
 *
 * @module pages/Users/UserEditPage
 */

import { useState, useEffect, useCallback, memo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, Users, Shield, User as UserIcon } from 'lucide-react';
import { useAuthStore } from '../../stores/authStore';
import { useToastStore } from '../../stores/toastStore';
import { fetchUserById, updateUser } from '../../api/users';
import type { SubUser } from '../../types/auth';
import { ParytySelect } from '../../components/common/ParytySelect';

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Form page for editing a sub-user's role and name.
 *
 * Uses the `dp-page` layout pattern and `aef-container-card` for the form.
 * Fetches the user data on mount and allows updating name and role.
 */
export const UserEditPage = memo(function UserEditPage() {
  const { userId } = useParams<{ userId: string }>();
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const currentUser = useAuthStore((s) => s.user);

  const [user, setUser] = useState<SubUser | null>(null);
  const [name, setName] = useState('');
  const [role, setRole] = useState<'admin' | 'operator' | 'viewer'>('operator');
  const [isLoading, setIsLoading] = useState(true);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isAdmin = currentUser?.role === 'admin';

  // ─── Data Fetching ──────────────────────────────────────────────────

  const loadUser = useCallback(async () => {
    if (!userId) return;
    setIsLoading(true);
    setError(null);
    try {
      const userData = await fetchUserById(userId);
      setUser(userData);
      setName(userData.name);
      setRole(userData.role);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load user.';
      setError(message);
      addToast({ type: 'error', message });
    } finally {
      setIsLoading(false);
    }
  }, [userId, addToast]);

  useEffect(() => {
    if (isAdmin) {
      loadUser();
    }
  }, [isAdmin, loadUser]);

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleBack = useCallback(() => {
    navigate('/users');
  }, [navigate]);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    if (!userId || !user) return;
    setError(null);

    if (!name.trim()) {
      setError('Name is required.');
      return;
    }

    setIsSubmitting(true);
    try {
      await updateUser(userId, {
        name: name.trim(),
        role,
      });
      addToast({ type: 'success', message: 'User updated successfully.' });
      navigate('/users');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to update user.';
      setError(message);
      addToast({ type: 'error', message });
    } finally {
      setIsSubmitting(false);
    }
  }, [userId, user, name, role, addToast, navigate]);

  // ─── Access Control ─────────────────────────────────────────────────

  if (!isAdmin) {
    return (
      <div className="dp-page" data-testid="user-edit-page-unauthorized">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <h1 className="dp-header__title">Edit User</h1>
              <p className="dp-header__sub">Only tenant admins can edit users.</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ─── Loading State ──────────────────────────────────────────────────

  if (isLoading) {
    return (
      <div className="dp-page" data-testid="user-edit-page-loading">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <h1 className="dp-header__title">Edit User</h1>
              <p className="dp-header__sub">Loading user data…</p>
            </div>
          </div>
          <div
            className="aef-container-card"
            style={{
              padding: 'var(--aef-space-12)',
              fontFamily: 'var(--aef-font-body)',
              textAlign: 'center',
              color: 'var(--aef-text-secondary)',
            }}
          >
            <p>Loading…</p>
          </div>
        </div>
      </div>
    );
  }

  // ─── Error State ────────────────────────────────────────────────────

  if (error && !user) {
    return (
      <div className="dp-page" data-testid="user-edit-page-error">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <button
                type="button"
                className="aef-btn aef-btn-inactive"
                onClick={handleBack}
                style={{ marginBottom: 'var(--aef-space-2)' }}
                data-testid="user-edit-back-error"
              >
                <ArrowLeft size={14} /> Back to Users
              </button>
              <h1 className="dp-header__title">Edit User</h1>
              <p className="dp-header__sub">{error}</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="user-edit-page">
      <div className="dp-page__inner">
        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleBack}
              style={{ marginBottom: 'var(--aef-space-2)' }}
              data-testid="user-edit-back"
            >
              <ArrowLeft size={14} /> Back to Users
            </button>
            <h1 className="dp-header__title">Edit User</h1>
            <p className="dp-header__sub">
              Update {user?.name}'s role and information.
            </p>
          </div>
        </div>

        {/* Form */}
        <form
          onSubmit={handleSubmit}
          className="aef-container-card"
          style={{ maxWidth: 600 }}
          data-testid="user-edit-form"
        >
          <div style={{ padding: 'var(--aef-space-5)' }}>
            {/* Error message */}
            {error && (
              <div
                style={{
                  padding: 'var(--aef-space-2) var(--aef-space-3)',
                  background: 'var(--aef-surface-low)',
                  border: 'var(--aef-border-width) solid var(--aef-error)',
                  borderRadius: 'var(--aef-radius-control)',
                  marginBottom: 'var(--aef-space-4)',
                }}
                data-testid="user-edit-error"
              >
                <span
                  style={{
                    fontFamily: 'var(--aef-font-body)',
                    fontSize: 11,
                    color: 'var(--aef-error)',
                  }}
                >
                  {error}
                </span>
              </div>
            )}

            {/* Email (read-only) */}
            <div className="dp-field" style={{ marginBottom: 'var(--aef-space-4)' }}>
              <label className="dp-field__label" htmlFor="user-edit-email">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-1)' }}>
                  <Users size={12} /> Email
                </div>
              </label>
              <input
                id="user-edit-email"
                className="dp-field__input"
                type="email"
                value={user?.email || ''}
                readOnly
                style={{ opacity: 0.7, cursor: 'not-allowed' }}
                data-testid="user-edit-email"
              />
            </div>

            {/* Name field */}
            <div className="dp-field" style={{ marginBottom: 'var(--aef-space-4)' }}>
              <label className="dp-field__label" htmlFor="user-edit-name">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-1)' }}>
                  <UserIcon size={12} /> Name
                </div>
              </label>
              <input
                id="user-edit-name"
                className="dp-field__input"
                type="text"
                placeholder="Full name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                data-testid="user-edit-name"
              />
            </div>

            {/* Role field */}
            <div className="dp-field" style={{ marginBottom: 'var(--aef-space-5)' }}>
              <label className="dp-field__label">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-1)' }}>
                  <Shield size={12} /> Role
                </div>
              </label>
              <ParytySelect
                options={[
                  { label: 'Admin - Full access', value: 'admin' },
                  { label: 'Operator - Can manage resources', value: 'operator' },
                  { label: 'Viewer - Read-only access', value: 'viewer' },
                ]}
                value={role}
                onChange={(v) => setRole(v as 'admin' | 'operator' | 'viewer')}
                testId="user-edit-role"
              />
            </div>

            {/* Actions */}
            <div
              style={{
                display: 'flex',
                justifyContent: 'flex-end',
                gap: 'var(--aef-space-3)',
              }}
            >
              <button
                type="button"
                className="aef-btn aef-btn-inactive"
                onClick={handleBack}
                disabled={isSubmitting}
                data-testid="user-edit-cancel"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="aef-btn aef-btn-active"
                disabled={isSubmitting || !name.trim()}
                data-testid="user-edit-submit"
              >
                {isSubmitting ? (
                  'Saving…'
                ) : (
                  <>
                    <Users size={14} /> Save Changes
                  </>
                )}
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
});
