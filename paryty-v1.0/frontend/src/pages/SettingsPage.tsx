/**
 * SettingsPage — user and tenant settings with tabbed navigation.
 *
 * Tabs:
 * - Account — email, name, change password
 * - Plan & Billing — current plan, features, limits, upgrade CTA
 * - API Keys — list, create, copy key (shown once)
 * - Users — admin only: list sub-users, create, edit role
 *
 * @module pages/SettingsPage
 */

import { useState, useCallback, useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  User, CreditCard, Key, Users, Copy, Plus, Check, Trash2, ArrowUpRight, Shield,
} from 'lucide-react';
import clsx from 'clsx';
import { useAuthStore } from '../stores/authStore';
import { usePlanStore } from '../stores/planStore';
import { useToastStore } from '../stores/toastStore';
import { getRestClient } from '../api/rest';
import type { ApiKey, CreateApiKeyResponse, RotateApiKeyResponse } from '../types/apiKeys';
import type { SubUser, CreateSubUserParams } from '../types/auth';
import { ParytySelect } from '../components/common/ParytySelect';


// ─── Tab Definition ───────────────────────────────────────────────────

interface Tab {
  id: string;
  label: string;
  icon: React.ReactNode;
  /** Only visible when this condition is true. */
  visible?: boolean;
}

const TABS: Tab[] = [
  { id: 'account', label: 'Account', icon: <User size={14} /> },
  { id: 'plan', label: 'Plan & Billing', icon: <CreditCard size={14} /> },
  { id: 'apikeys', label: 'API Keys', icon: <Key size={14} /> },
  { id: 'users', label: 'Users', icon: <Users size={14} />, visible: true }, // visible if admin
];

// ─── Tab: Account ─────────────────────────────────────────────────────

function AccountTab() {
  const user = useAuthStore((s) => s.user);
  const addToast = useToastStore((s) => s.addToast);

  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [changingPassword, setChangingPassword] = useState(false);
  const [pwError, setPwError] = useState<string | null>(null);

  const handleChangePassword = useCallback(async () => {
    setPwError(null);
    if (newPassword.length < 8) {
      setPwError('New password must be at least 8 characters.');
      return;
    }
    if (newPassword !== confirmPassword) {
      setPwError('Passwords do not match.');
      return;
    }

    setChangingPassword(true);
    try {
      await getRestClient().post('/api/v1/auth/change-password', {
        currentPassword,
        newPassword,
      });
      addToast({ type: 'success', message: 'Password changed successfully.' });
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
    } catch {
      setPwError('Failed to change password. Check your current password.');
    } finally {
      setChangingPassword(false);
    }
  }, [currentPassword, newPassword, confirmPassword, addToast]);

  if (!user) return null;

  return (
    <div className="settings-tab">
      <h2 className="settings-tab__title">Account</h2>

      <div className="dp-confirm-row">
        <span className="dp-confirm-row__label">Email</span>
        <span className="dp-confirm-row__value">{user.email}</span>
      </div>
      <div className="dp-confirm-row">
        <span className="dp-confirm-row__label">Name</span>
        <span className="dp-confirm-row__value">{user.name}</span>
      </div>
      <div className="dp-confirm-row">
        <span className="dp-confirm-row__label">Role</span>
        <span className="dp-confirm-row__value" style={{ textTransform: 'capitalize' }}>{user.role}</span>
      </div>

      <hr style={{
        border: 'none', borderTop: '1px solid var(--aef-border)',
        margin: 'var(--aef-space-4) 0',
      }} />

      <h3 style={{
        fontFamily: 'var(--aef-font-body)',
        fontSize: 13,
        fontWeight: 600,
        color: 'var(--aef-text-primary)',
        marginBottom: 'var(--aef-space-3)',
      }}>
        Change Password
      </h3>

      {pwError && (
        <div className="auth-form__error" role="alert" data-testid="pw-error">{pwError}</div>
      )}

      <div className="dp-field">
        <label className="dp-field__label" htmlFor="settings-current-pw">Current password</label>
        <input
          id="settings-current-pw" className="dp-field__input" type="password"
          value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)}
          data-testid="settings-current-pw"
        />
      </div>
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="settings-new-pw">New password</label>
        <input
          id="settings-new-pw" className="dp-field__input" type="password"
          value={newPassword} onChange={(e) => setNewPassword(e.target.value)}
          data-testid="settings-new-pw"
        />
      </div>
      <div className="dp-field">
        <label className="dp-field__label" htmlFor="settings-confirm-pw">Confirm new password</label>
        <input
          id="settings-confirm-pw" className="dp-field__input" type="password"
          value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)}
          data-testid="settings-confirm-pw"
        />
      </div>

      <button
        className="aef-btn aef-btn-active"
        onClick={handleChangePassword}
        disabled={changingPassword || !currentPassword || !newPassword}
        style={{ opacity: changingPassword || !currentPassword || !newPassword ? 0.4 : 1 }}
        data-testid="settings-change-pw"
      >
        {changingPassword ? 'Changing…' : 'Change Password'}
      </button>
    </div>
  );
}

// ─── Tab: Plan & Billing ──────────────────────────────────────────────

function PlanTab() {
  const currentPlan = usePlanStore((s) => s.currentPlan);
  const plans = usePlanStore((s) => s.plans);
  const fetchCurrentPlan = usePlanStore((s) => s.fetchCurrentPlan);
  const fetchPlans = usePlanStore((s) => s.fetchPlans);
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();
  const [upgradingPlan, setUpgradingPlan] = useState<string | null>(null);

  useEffect(() => {
    fetchCurrentPlan();
    fetchPlans();
  }, [fetchCurrentPlan, fetchPlans]);

  if (!currentPlan) {
    return (
      <div className="settings-tab">
        <p style={{ color: 'var(--aef-text-secondary)', fontSize: 12 }}>Loading plan info…</p>
      </div>
    );
  }

  return (
    <div className="settings-tab">
      <h2 className="settings-tab__title">Plan & Billing</h2>

      <div className="dp-confirm-row">
        <span className="dp-confirm-row__label">Current Plan</span>
        <span className="dp-confirm-row__value" style={{ fontWeight: 600 }}>
          {currentPlan.planName}
        </span>
      </div>
      <div className="dp-confirm-row">
        <span className="dp-confirm-row__label">Started</span>
        <span className="dp-confirm-row__value">
          {new Date(currentPlan.startedAt).toLocaleDateString()}
        </span>
      </div>
      {currentPlan.expiresAt && (
        <div className="dp-confirm-row">
          <span className="dp-confirm-row__label">Expires</span>
          <span className="dp-confirm-row__value">
            {new Date(currentPlan.expiresAt).toLocaleDateString()}
          </span>
        </div>
      )}

      <hr style={{
        border: 'none', borderTop: '1px solid var(--aef-border)',
        margin: 'var(--aef-space-4) 0',
      }} />

      <h3 style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 13, fontWeight: 600,
        color: 'var(--aef-text-primary)', marginBottom: 'var(--aef-space-2)',
      }}>
        Features
      </h3>
      <div className="settings-feature-grid">
        {Object.entries(currentPlan.features).map(([key, enabled]) => (
          <div key={key} className="settings-feature-row">
            <span className="settings-feature__name">{key}</span>
            <span className={clsx('settings-feature__status', enabled ? 'settings-feature__status--on' : 'settings-feature__status--off')}>
              {enabled ? '✓' : '—'}
            </span>
          </div>
        ))}
      </div>

      <hr style={{
        border: 'none', borderTop: '1px solid var(--aef-border)',
        margin: 'var(--aef-space-4) 0',
      }} />

      <h3 style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 13, fontWeight: 600,
        color: 'var(--aef-text-primary)', marginBottom: 'var(--aef-space-2)',
      }}>
        Limits
      </h3>
      {Object.entries(currentPlan.limits).map(([key, value]) => (
        <div key={key} className="dp-confirm-row">
          <span className="dp-confirm-row__label">{key}</span>
          <span className="dp-confirm-row__value">{value}</span>
        </div>
      ))}

      {/* Available plans for upgrade */}
      <hr style={{
        border: 'none', borderTop: '1px solid var(--aef-border)',
        margin: 'var(--aef-space-4) 0',
      }} />
      <h3 style={{
        fontFamily: 'var(--aef-font-body)', fontSize: 13, fontWeight: 600,
        color: 'var(--aef-text-primary)', marginBottom: 'var(--aef-space-2)',
      }}>
        Available Plans
      </h3>
      <div className="dp-ability-grid">
        {plans.map((plan) => (
          <div
            key={plan.name}
            className={clsx('dp-ability-tile', plan.name === currentPlan.planName && 'dp-ability-tile--selected')}
            style={{ cursor: 'default' }}
          >
            <span className="dp-ability-tile__name">{plan.displayName}</span>
            <span className="dp-ability-tile__desc">
              {plan.maxTwins > 0 ? `Up to ${plan.maxTwins} Parytys` : 'Unlimited Parytys'}
            </span>
            {plan.name !== currentPlan.planName && plan.creatable && (
              <button
                className="aef-btn aef-btn-active"
                style={{ marginTop: 'var(--aef-space-2)', opacity: upgradingPlan === plan.name ? 0.4 : 1 }}
                disabled={upgradingPlan !== null}
                onClick={async () => {
                  setUpgradingPlan(plan.name);
                  try {
                    await client.post('/api/v1/tenant/plan/change', { planName: plan.name });
                    addToast({ type: 'success', message: `Upgraded to ${plan.displayName}!` });
                    fetchCurrentPlan();
                  } catch {
                    addToast({ type: 'error', message: 'Failed to change plan. Contact support.' });
                  } finally {
                    setUpgradingPlan(null);
                  }
                }}
                data-testid={`upgrade-${plan.name}`}
              >
                <ArrowUpRight size={12} /> {upgradingPlan === plan.name ? 'Upgrading…' : 'Upgrade'}
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Tab: API Keys ────────────────────────────────────────────────────

function ApiKeysTab() {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [newKey, setNewKey] = useState<CreateApiKeyResponse | null>(null);
  const [keyName, setKeyName] = useState('');
  const [isCreating, setIsCreating] = useState(false);
  const [copied, setCopied] = useState(false);
  const [rotatingKeyId, setRotatingKeyId] = useState<string | null>(null);
  const [confirmPassword, setConfirmPassword] = useState('');
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();

  const fetchKeys = useCallback(async () => {
    try {
      const data = await client.get<ApiKey[]>('/api/v1/api-keys');
      setKeys(data);
    } catch {
      // Ignore fetch errors
    }
  }, [client]);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const handleCreate = useCallback(async () => {
    if (!keyName.trim()) return;
    setIsCreating(true);
    try {
      const result = await client.post<CreateApiKeyResponse>('/api/v1/api-keys', {
        name: keyName.trim(),
      });
      setNewKey(result);
      setKeyName('');
      addToast({ type: 'success', message: 'API key created.' });
      fetchKeys();
    } catch {
      addToast({ type: 'error', message: 'Failed to create API key.' });
    } finally {
      setIsCreating(false);
    }
  }, [keyName, client, addToast, fetchKeys]);

  const handleDelete = useCallback(async (id: string) => {
    try {
      await client.delete(`/api/v1/api-keys/${id}`);
      setKeys((prev) => prev.filter((k) => k.id !== id));
      addToast({ type: 'success', message: 'API key deleted.' });
    } catch {
      addToast({ type: 'error', message: 'Failed to delete API key.' });
    }
  }, [client, addToast]);

  const handleRotate = useCallback(async (id: string, name: string) => {
    if (rotatingKeyId !== id) {
      setRotatingKeyId(id);
      setConfirmPassword('');
      return;
    }
    // Require password re-entry for security-sensitive key rotation.
    if (!confirmPassword) {
      addToast({ type: 'error', message: 'Enter your password to confirm key rotation.' });
      return;
    }
    setRotatingKeyId(null);
    setConfirmPassword('');
    try {
      const resp = await client.put<{ data: RotateApiKeyResponse }>(`/api/v1/api-keys/${id}/rotate`, {
        password: confirmPassword,
      });
      const result = resp.data;
      setNewKey({ ...result, name, createdAt: new Date().toISOString() });
      addToast({ type: 'success', message: 'API key rotated. Copy the new key now — it won\'t be shown again.' });
      fetchKeys();
    } catch {
      addToast({ type: 'error', message: 'Failed to rotate API key.' });
    }
  }, [client, addToast, fetchKeys, rotatingKeyId, confirmPassword]);

  const handleCopy = useCallback(async (key: string) => {
    try {
      await navigator.clipboard.writeText(key);
      setCopied(true);
      setTimeout(() => setCopied(false), 3000);
    } catch {
      // Clipboard API not available
    }
  }, []);

  return (
    <div className="settings-tab">
      <h2 className="settings-tab__title">API Keys</h2>

      {/* New key revealed — show once! */}
      {newKey && (
        <div className="aef-container-card" style={{ marginBottom: 'var(--aef-space-4)', borderColor: 'var(--aef-accent)' }}>
          <div className="aef-container-card__body">
            <p style={{ fontFamily: 'var(--aef-font-body)', fontSize: 12, color: 'var(--aef-accent)', fontWeight: 600 }}>
              {newKey.name ? 'Key rotated!' : 'Key created!'} Copy it now — it won&apos;t be shown again.
            </p>
            <div style={{
              display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)',
              background: 'var(--aef-border)', borderRadius: 'var(--aef-radius-md)',
              padding: 'var(--aef-space-2) var(--aef-space-3)',
              marginTop: 'var(--aef-space-2)',
            }}>
              <code style={{
                fontFamily: 'monospace', fontSize: 12, flex: 1,
                overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
              }}>
                {newKey.key}
              </code>
              <button
                className="aef-btn aef-btn-active"
                onClick={() => handleCopy(newKey.key)}
                data-testid="copy-api-key"
              >
                {copied ? <><Check size={12} /> Copied</> : <><Copy size={12} /> Copy</>}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Create form */}
      <div className="aef-container-card" style={{ marginBottom: 'var(--aef-space-4)' }}>
        <div className="aef-container-card__header">
          <Key size={14} />
          <span className="aef-container-card__title">Create API Key</span>
        </div>
        <div className="aef-container-card__body">
          <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
            <input
              className="dp-field__input"
              type="text"
              placeholder="Key name (e.g. CI/CD Pipeline)"
              value={keyName}
              onChange={(e) => setKeyName(e.target.value)}
              style={{ flex: 1 }}
              data-testid="api-key-name"
            />
            <button
              className="aef-btn aef-btn-active"
              onClick={handleCreate}
              disabled={isCreating || !keyName.trim()}
              data-testid="create-api-key"
            >
              <Plus size={14} /> Create
            </button>
          </div>
        </div>
      </div>

      {/* Key list */}
      {keys.length === 0 ? (
        <p style={{ color: 'var(--aef-text-secondary)', fontSize: 12 }} data-testid="no-api-keys">
          No API keys yet. Create one to access the Paryty API.
        </p>
      ) : (
        <div className="aef-table-card">
          <div className="aef-table-card__header">
            <span>API Keys</span>
            <span className="aef-badge">{keys.length}</span>
          </div>
          <div className="aef-table-card__body">
            <table className="aef-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Prefix</th>
                  <th>Created</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody>
                {keys.map((key) => (
                  <tr key={key.id}>
                    <td>
                      <span className="aef-text-primary">{key.name}</span>
                    </td>
                    <td>
                      <code className="aef-text-secondary">{key.prefix}…</code>
                    </td>
                    <td>
                      <span className="aef-text-secondary">
                        {new Date(key.createdAt).toLocaleDateString()}
                      </span>
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: 'var(--aef-space-1)' }}>
                        {rotatingKeyId === key.id ? (
                          <>
                            <input
                              type="password"
                              placeholder="Enter password to confirm"
                              value={confirmPassword}
                              onChange={(e) => setConfirmPassword(e.target.value)}
                              className="aef-input aef-input--compact"
                              style={{ width: 160, padding: '2px 6px', fontSize: 10 }}
                              data-testid={`rotate-password-${key.id}`}
                            />
                            <button
                              className="aef-btn aef-btn-active"
                              onClick={() => handleRotate(key.id, key.name)}
                              aria-label={`Confirm rotate key ${key.name}`}
                              style={{ padding: '2px 6px', fontSize: 11 }}
                              data-testid={`confirm-rotate-${key.id}`}
                            >
                              Confirm
                            </button>
                            <button
                              className="aef-btn aef-btn-inactive"
                              onClick={() => { setRotatingKeyId(null); setConfirmPassword(''); }}
                              aria-label={`Cancel rotate key ${key.name}`}
                              style={{ padding: '2px 6px', fontSize: 11 }}
                              data-testid={`cancel-rotate-${key.id}`}
                            >
                              Cancel
                            </button>
                          </>
                        ) : (
                          <>
                            <button
                              className="aef-btn aef-btn-inactive"
                              onClick={() => handleRotate(key.id, key.name)}
                              aria-label={`Rotate key ${key.name}`}
                              style={{ padding: '2px 6px' }}
                              title="Rotate key"
                            >
                              <ArrowUpRight size={12} />
                            </button>
                            <button
                              className="aef-btn aef-btn-inactive"
                              onClick={() => handleDelete(key.id)}
                              aria-label={`Delete key ${key.name}`}
                              style={{ padding: '2px 6px' }}
                            >
                              <Trash2 size={12} />
                            </button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── Tab: Users ───────────────────────────────────────────────────────

function UsersTab() {
  const user = useAuthStore((s) => s.user);
  const addToast = useToastStore((s) => s.addToast);
  const client = getRestClient();

  const [subUsers, setSubUsers] = useState<SubUser[]>([]);
  const [email, setEmail] = useState('');
  const [subName, setSubName] = useState('');
  const [subPassword, setSubPassword] = useState('');
  const [role, setRole] = useState<'admin' | 'operator' | 'viewer'>('operator');
  const [isCreating, setIsCreating] = useState(false);

  const isAdmin = user?.role === 'admin';

  const fetchSubUsers = useCallback(async () => {
    try {
      // BUGFIX: /api/v1/users returns {data: [...]} (envelope), not a raw array.
      const resp = await client.get<{ data: SubUser[] }>('/api/v1/users');
      setSubUsers(resp.data ?? []);
    } catch {
      // Ignore
    }
  }, [client]);

  useEffect(() => {
    if (isAdmin) fetchSubUsers();
  }, [isAdmin, fetchSubUsers]);

  if (!isAdmin) {
    return (
      <div className="settings-tab">
        <h2 className="settings-tab__title">Users</h2>
        <p style={{ color: 'var(--aef-text-secondary)', fontSize: 12 }}>
          Only tenant admins can manage users.
        </p>
      </div>
    );
  }

  const handleCreate = useCallback(async () => {
    if (!email.trim() || !subPassword || !subName.trim()) return;
    setIsCreating(true);
    try {
      const params: CreateSubUserParams = {
        email: email.trim(),
        password: subPassword,
        name: subName.trim(),
        role,
      };
      await client.post('/api/v1/users', params);
      addToast({ type: 'success', message: 'User created.' });
      setEmail('');
      setSubName('');
      setSubPassword('');
      fetchSubUsers();
    } catch {
      addToast({ type: 'error', message: 'Failed to create user.' });
    } finally {
      setIsCreating(false);
    }
  }, [email, subPassword, subName, role, client, addToast, fetchSubUsers]);

  return (
    <div className="settings-tab">
      <h2 className="settings-tab__title">Users</h2>

      {/* Create form */}
      <div style={{
        display: 'flex', flexDirection: 'column', gap: 'var(--aef-space-2)',
        marginBottom: 'var(--aef-space-4)',
      }}>
        <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
          <div className="dp-field" style={{ flex: 1 }}>
            <label className="dp-field__label">Email</label>
            <input
              className="dp-field__input" type="email" placeholder="user@example.com"
              value={email} onChange={(e) => setEmail(e.target.value)}
              data-testid="sub-user-email"
            />
          </div>
          <div className="dp-field" style={{ flex: 1 }}>
            <label className="dp-field__label">Name</label>
            <input
              className="dp-field__input" type="text" placeholder="Name"
              value={subName} onChange={(e) => setSubName(e.target.value)}
              data-testid="sub-user-name"
            />
          </div>
        </div>
        <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
          <div className="dp-field" style={{ flex: 1 }}>
            <label className="dp-field__label">Password</label>
            <input
              className="dp-field__input" type="password" placeholder="Min. 8 characters"
              value={subPassword} onChange={(e) => setSubPassword(e.target.value)}
              data-testid="sub-user-password"
            />
          </div>
          <div className="dp-field" style={{ flex: 1 }}>
            <label className="dp-field__label">Role</label>
            <ParytySelect
              options={[
                { label: 'Admin', value: 'admin' },
                { label: 'Operator', value: 'operator' },
                { label: 'Viewer', value: 'viewer' },
              ]}
              value={role}
              onChange={(v) => setRole(v as 'admin' | 'operator' | 'viewer')}
              testId="sub-user-role"
            />
          </div>
        </div>
        <button
          className="aef-btn aef-btn-active"
          onClick={handleCreate}
          disabled={isCreating || !email.trim() || !subPassword || !subName.trim()}
          data-testid="create-sub-user"
        >
          <Plus size={14} /> Add User
        </button>
      </div>

      {/* User list */}
      {subUsers.length === 0 ? (
        <p style={{ color: 'var(--aef-text-secondary)', fontSize: 12 }} data-testid="no-sub-users">
          No sub-users yet.
        </p>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--aef-space-2)' }}>
          {subUsers.map((su) => (
            <div key={su.id} className="dp-confirm-row">
              <div>
                <span style={{ fontSize: 12, fontFamily: 'var(--aef-font-body)', color: 'var(--aef-text-primary)' }}>
                  {su.name}
                </span>
                <span style={{ fontSize: 11, color: 'var(--aef-text-secondary)', marginLeft: 'var(--aef-space-2)' }}>
                  {su.email}
                </span>
              </div>
              <span style={{
                fontSize: 11, padding: '2px 8px',
                borderRadius: 'var(--aef-radius-full)',
                background: 'var(--aef-border)',
                color: 'var(--aef-text-secondary)',
                textTransform: 'capitalize',
              }}>
                <Shield size={10} style={{ marginRight: 4 }} /> {su.role}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ─── Page ─────────────────────────────────────────────────────────────

const TAB_QUERY_PARAM = 'tab';

export function SettingsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const user = useAuthStore((s) => s.user);

  const activeTab = searchParams.get(TAB_QUERY_PARAM) ?? 'account';

  // Filter tabs by visibility
  const visibleTabs = useMemo(() => {
    return TABS.filter((tab) => {
      if (tab.id === 'users' && user?.role !== 'admin') return false;
      return true;
    });
  }, [user?.role]);

  const switchTab = useCallback((tabId: string) => {
    setSearchParams({ [TAB_QUERY_PARAM]: tabId });
  }, [setSearchParams]);

  const tabContent = (() => {
    switch (activeTab) {
      case 'account': return <AccountTab />;
      case 'plan': return <PlanTab />;
      case 'apikeys': return <ApiKeysTab />;
      case 'users': return <UsersTab />;
      default: return <AccountTab />;
    }
  })();

  return (
    <div className="dp-page" data-testid="settings-page">
      {/* Page header */}
      <div className="dp-page__inner" style={{ maxWidth: 720, paddingBottom: 0, gap: 0 }}>
        <div className="dp-header">
          <div className="dp-header__title-block">
            <h1 className="dp-header__title">Settings</h1>
          </div>
        </div>
      </div>

      {/* Tab bar — sticky sub-header, direct child of scroll container */}
      <div className="settings-tab-bar settings-tab-bar--sticky" role="tablist" aria-label="Settings tabs">
        {visibleTabs.map((tab) => (
          <button
            key={tab.id}
            role="tab"
            aria-selected={activeTab === tab.id}
            className={clsx('settings-tab-btn', activeTab === tab.id && 'settings-tab-btn--active')}
            onClick={() => switchTab(tab.id)}
            data-testid={`settings-tab-${tab.id}`}
          >
            {tab.icon}
            <span>{tab.label}</span>
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="dp-page__inner" style={{ maxWidth: 720 }}>
        <div role="tabpanel">
          {tabContent}
        </div>
      </div>
    </div>
  );
}
