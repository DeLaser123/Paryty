/**
 * AuthHint — floating contextual help card for auth pages.
 *
 * Renders alongside the login/register card and dynamically updates
 * based on which input field is focused:
 *  - Email: hint text + live format validation (✓/✗)
 *  - Password: 5 animated password requirement checkmarks (register)
 *  - Name / Organization: simple hint text
 *  - null: default welcome message
 *
 * @module components/common/AuthHint
 */

import {
  Mail,
  Lock,
  User,
  Building2,
  Check,
  X,
  Info,
} from 'lucide-react';
import clsx from 'clsx';

// ─── Types ──────────────────────────────────────────────────────────

export type HintField =
  | 'email'
  | 'password'
  | 'name'
  | 'organization'
  | null;

interface AuthHintProps {
  /** Which field is currently focused, or null for default message. */
  focusedField: HintField;
  /** Current email value (for live validation indicator). */
  emailValue?: string;
  /** Current password value (for live requirement checkmarks). */
  passwordValue?: string;
  /** Whether this is the register page (affects wording). */
  isRegister?: boolean;
}

// ─── Email validation ────────────────────────────────────────────────

function isValidEmail(email: string): boolean {
  if (!email) return false;
  return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
}

// ─── Password checks ─────────────────────────────────────────────────

interface PasswordChecks {
  minLength: boolean;
  hasUpper: boolean;
  hasLower: boolean;
  hasDigit: boolean;
  hasSpecial: boolean;
}

function evalPassword(pw: string): PasswordChecks {
  return {
    minLength: pw.length >= 8,
    hasUpper: /[A-Z]/.test(pw),
    hasLower: /[a-z]/.test(pw),
    hasDigit: /[0-9]/.test(pw),
    hasSpecial: /[^A-Za-z0-9]/.test(pw),
  };
}

function allPasswordRulesMet(c: PasswordChecks): boolean {
  return c.minLength && c.hasUpper && c.hasLower && c.hasDigit && c.hasSpecial;
}

const PASSWORD_RULES: { key: keyof PasswordChecks; label: string }[] = [
  { key: 'minLength', label: 'At least 8 characters' },
  { key: 'hasUpper', label: 'One uppercase letter (A–Z)' },
  { key: 'hasLower', label: 'One lowercase letter (a–z)' },
  { key: 'hasDigit', label: 'One digit (0–9)' },
  { key: 'hasSpecial', label: 'One special character (!@#$…)' },
];

// ─── Content renderers ───────────────────────────────────────────────

/** Default welcome message shown when no field is focused. */
function DefaultContent({ isRegister }: { isRegister?: boolean }) {
  return (
    <>
      <p className="auth-hint__text">
        {isRegister
          ? 'Create your Paryty workspace in three easy steps. Fill in your account details to get started.'
          : 'Welcome to Paryty — enter your credentials to access your observability workspace.'}
      </p>
    </>
  );
}

/** Email hint + live validation indicator. */
function EmailContent({ emailValue }: { emailValue?: string }) {
  const val = (emailValue ?? '').trim();
  const showIndicator = val.length > 0;
  const valid = isValidEmail(val);

  return (
    <>
      <p className="auth-hint__text">
        Enter a valid email address (e.g., you@example.com).
      </p>
      {showIndicator && (
        <div
          className={clsx(
            'auth-hint__validation',
            valid ? 'auth-hint__validation--valid' : 'auth-hint__validation--invalid',
          )}
          key={valid ? 'valid' : 'invalid'}
          data-testid={valid ? 'auth-hint-email-valid' : 'auth-hint-email-invalid'}
        >
          {valid ? (
            <Check size={12} style={{ animation: 'aef-scale-settle var(--aef-duration-standard) var(--aef-ease-settle) both' }} />
          ) : (
            <X size={12} style={{ animation: 'aef-scale-settle var(--aef-duration-standard) var(--aef-ease-settle) both' }} />
          )}
          <span>{valid ? 'Valid email format' : 'Invalid email format'}</span>
        </div>
      )}
    </>
  );
}

/** Password hint — simple text on login, rule checklist on register. */
function PasswordContent({ passwordValue, isRegister }: { passwordValue?: string; isRegister?: boolean }) {
  if (!isRegister) {
    return (
      <p className="auth-hint__text">
        Enter your account password.
      </p>
    );
  }

  const pw = passwordValue ?? '';
  const checks = evalPassword(pw);
  const allMet = allPasswordRulesMet(checks);
  const metCount = PASSWORD_RULES.filter((r) => checks[r.key]).length;
  const hasInput = pw.length > 0;

  const strengthLabels = ['Very Weak', 'Weak', 'Fair', 'Good', 'Strong'];
  const strengthColors = [
    'var(--aef-counter-variant-b)',
    'var(--aef-counter-variant-b)',
    '#eab308',
    '#22c55e',
    'var(--aef-text-success)',
  ];
  const strengthLabel = strengthLabels[metCount];
  const strengthColor = strengthColors[metCount];

  return (
    <>
      <p className="auth-hint__text">
        Your password must meet all five requirements below.
      </p>

      {/* Visual strength meter */}
      {hasInput && (
        <div
          className="auth-hint__strength-meter"
          data-testid="auth-hint-strength-meter"
          style={{
            animation: 'aef-scale-in var(--aef-duration-standard) var(--aef-ease-settle) both',
          }}
        >
          <div style={{
            display: 'flex',
            gap: 3,
            marginBottom: 'var(--aef-space-1)',
          }}>
            {[0, 1, 2, 3, 4].map((i) => (
              <div
                key={i}
                style={{
                  flex: 1,
                  height: 3,
                  borderRadius: 2,
                  background: i < metCount ? strengthColor : 'var(--aef-border)',
                  transition: 'background var(--aef-duration-fast) var(--aef-ease-exit)',
                }}
              />
            ))}
          </div>
          <span style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 9,
            color: strengthColor,
            transition: 'color var(--aef-duration-fast) var(--aef-ease-exit)',
          }}>
            {strengthLabel}
          </span>
        </div>
      )}

      <div className="auth-hint__req-list">
        {PASSWORD_RULES.map((rule, i) => {
          const met = checks[rule.key];
          return (
            <div
              key={rule.key}
              className={clsx('auth-hint__req-row', met && 'auth-hint__req-row--met')}
              data-testid={`auth-hint-pw-rule-${i}`}
            >
              <span className="auth-hint__req-icon">
                {met ? (
                  <Check
                    size={10}
                    style={{
                      color: 'var(--aef-text-success)',
                      animation: 'aef-scale-settle var(--aef-duration-standard) var(--aef-ease-settle) both',
                    }}
                  />
                ) : (
                  <span
                    style={{
                      display: 'block',
                      width: 7,
                      height: 7,
                      borderRadius: '50%',
                      border: '1px solid var(--aef-border)',
                    }}
                  />
                )}
              </span>
              <span>{rule.label}</span>
            </div>
          );
        })}
      </div>
      {allMet && (
        <p
          className="auth-hint__strength"
          key="strong"
          style={{
            animation: 'aef-scale-in var(--aef-duration-standard) var(--aef-ease-settle) both',
          }}
        >
          Password strength: Strong
        </p>
      )}
    </>
  );
}

function NameContent() {
  return (
    <p className="auth-hint__text">
      Your display name — how others will see you in the workspace.
    </p>
  );
}

function OrganizationContent() {
  return (
    <p className="auth-hint__text">
      Your company or team name — this will be your tenant identifier.
    </p>
  );
}

// ─── Field metadata ──────────────────────────────────────────────────

interface FieldMeta {
  icon: React.ReactNode;
  label: string;
}

const FIELD_META: Record<NonNullable<HintField>, FieldMeta> = {
  email: { icon: <Mail size={11} />, label: 'Email' },
  password: { icon: <Lock size={11} />, label: 'Password' },
  name: { icon: <User size={11} />, label: 'Name' },
  organization: { icon: <Building2 size={11} />, label: 'Organization' },
};

const DEFAULT_META: FieldMeta = {
  icon: <Info size={11} />,
  label: 'Help',
};

// ─── Component ────────────────────────────────────────────────────────

export function AuthHint({
  focusedField,
  emailValue,
  passwordValue,
  isRegister = false,
}: AuthHintProps) {
  const meta = focusedField ? FIELD_META[focusedField] : DEFAULT_META;

  const content = (() => {
    switch (focusedField) {
      case 'email':
        return <EmailContent emailValue={emailValue} />;
      case 'password':
        return <PasswordContent passwordValue={passwordValue} isRegister={isRegister} />;
      case 'name':
        return <NameContent />;
      case 'organization':
        return <OrganizationContent />;
      default:
        return <DefaultContent isRegister={isRegister} />;
    }
  })();

  return (
    <div
      className="auth-hint aef-container-card"
      data-testid="auth-hint"
      style={{
        animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 100ms both',
      }}
    >
      <div className="aef-container-card__body">
        {/* Header — icon + label */}
        <div className="auth-hint__header">
          <span style={{ display: 'flex', color: 'var(--aef-text-secondary)' }}>
            {meta.icon}
          </span>
          <span>{meta.label}</span>
        </div>

        {/* Body — dynamic content, keyed for transition */}
        <div
          className="auth-hint__body"
          key={focusedField ?? '__default'}
          style={{
            animation: 'aef-scale-in var(--aef-duration-standard) var(--aef-ease-settle) both',
          }}
        >
          {content}
        </div>
      </div>
    </div>
  );
}
