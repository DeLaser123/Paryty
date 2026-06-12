/**
 * UserCreatePage — form for creating a new sub-user.
 *
 * Renders a form with email, name, password, and role fields.
 * Admin-only page with role-based access control.
 *
 * @module pages/Users/UserCreatePage
 */

import { useState, useCallback, memo } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowLeft, Users, Shield, Mail, Lock, User as UserIcon } from 'lucide-react';
import { useAuthStore } from '../../stores/authStore';
import { useToastStore } from '../../stores/toastStore';
import { createUser } from '../../api/users';
import type { CreateSubUserParams } from '../../types/auth';
import { ParytySelect } from '../../components/common/ParytySelect';

// ─── Component ──────────────────────────────────────────────────────────────

/**
 * Form page for creating a new sub-user.
 *
 * Uses the `dp-page` layout pattern and `aef-container-card` for the form.
 * Validates input and calls the API to create the user.
 */
export const UserCreatePage = memo(function UserCreatePage() {
  const navigate = useNavigate();
  const addToast = useToastStore((s) => s.addToast);
  const user = useAuthStore((s) => s.user);

  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<'admin' | 'operator' | 'viewer'>('operator');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isAdmin = user?.role === 'admin';

  // ─── Handlers ───────────────────────────────────────────────────────

  const handleBack = useCallback(() => {
    navigate('/users');
  }, [navigate]);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!email.trim() || !name.trim() || !password) {
      setError('All fields are required.');
      return;
    }

    if (password.length < 8) {
      setError('Password must be at least 8 characters.');
      return;
    }

    setIsSubmitting(true);
    try {
      const params: CreateSubUserParams = {
        email: email.trim(),
        name: name.trim(),
        password,
        role,
      };
      await createUser(params);
      addToast({ type: 'success', message: 'User created successfully.' });
      navigate('/users');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to create user.';
      setError(message);
      addToast({ type: 'error', message });
    } finally {
      setIsSubmitting(false);
    }
  }, [email, name, password, role, addToast, navigate]);

  // ─── Access Control ─────────────────────────────────────────────────

  if (!isAdmin) {
    return (
      <div className="dp-page" data-testid="user-create-page-unauthorized">
        <div className="dp-page__inner">
          <div className="dp-header">
            <div className="dp-header__title-block">
              <h1 className="dp-header__title">Create User</h1>
              <p className="dp-header__sub">Only tenant admins can create users.</p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ─── Render ─────────────────────────────────────────────────────────

  return (
    <div className="dp-page" data-testid="user-create-page">
      <div className="dp-page__inner">
        {/* Page header */}
        <div className="dp-header">
          <div className="dp-header__title-block">
            <button
              type="button"
              className="aef-btn aef-btn-inactive"
              onClick={handleBack}
              style={{ marginBottom: 'var(--aef-space-2)' }}
              data-testid="user-create-back"
            >
              <ArrowLeft size={14} /> Back to Users
            </button>
            <h1 className="dp-header__title">Create User</h1>
            <p className="dp-header__sub">Add a new sub-user to your tenant.</p>
          </div>
        </div>

        {/* Form */}
        <form
          onSubmit={handleSubmit}
          className="aef-container-card"
          style={{ maxWidth: 600 }}
          data-testid="user-create-form"
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
                data-testid="user-create-error"
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

            {/* Email field */}
            <div className="dp-field" style={{ marginBottom: 'var(--aef-space-4)' }}>
              <label className="dp-field__label" htmlFor="user-create-email">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-1)' }}>
                  <Mail size={12} /> Email
                </div>
              </label>
              <input
                id="user-create-email"
                className="dp-field__input"
                type="email"
                placeholder="user@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                data-testid="user-create-email"
              />
            </div>

            {/* Name field */}
            <div className="dp-field" style={{ marginBottom: 'var(--aef-space-4)' }}>
              <label className="dp-field__label" htmlFor="user-create-name">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-1)' }}>
                  <UserIcon size={12} /> Name
                </div>
              </label>
              <input
                id="user-create-name"
                className="dp-field__input"
                type="text"
                placeholder="Full name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                data-testid="user-create-name"
              />
            </div>

            {/* Password field */}
            <div className="dp-field" style={{ marginBottom: 'var(--aef-space-4)' }}>
              <label className="dp-field__label" htmlFor="user-create-password">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-1)' }}>
                  <Lock size={12} /> Password
                </div>
              </label>
              <input
                id="user-create-password"
                className="dp-field__input"
                type="password"
                placeholder="Min. 8 characters"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={8}
                data-testid="user-create-password"
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
                testId="user-create-role"
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
                data-testid="user-create-cancel"
              >
                Cancel
              </button>
              <button
                type="submit"
                className="aef-btn aef-btn-active"
                disabled={isSubmitting || !email.trim() || !name.trim() || !password}
                data-testid="user-create-submit"
              >
                {isSubmitting ? (
                  'Creating…'
                ) : (
                  <>
                    <Users size={14} /> Create User
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
