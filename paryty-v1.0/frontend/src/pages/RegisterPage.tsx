/**
 * RegisterPage — multi-step workspace creation wizard.
 *
 * Centered container-card with brand mark, animated step transitions,
 * icon-rich fields, and staggered entrance animations.
 * Renders WITHOUT AppShell chrome — pure auth surface.
 *
 * Steps:
 * 1. Account — email, password, name, tenant name
 * 2. Plan Selection — radio-style tile buttons for each creatable plan
 * 3. Confirm — review and submit
 *
 * @module pages/RegisterPage
 */

import { useState, useCallback, useEffect, useRef, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  UserPlus,
  ArrowLeft,
  ArrowRight,
  Mail,
  Lock,
  User,
  Building2,
  Eye,
  EyeOff,
  Shield,
  Check,
  Sparkles,
  AlertTriangle,
  RefreshCw,
} from 'lucide-react';
import clsx from 'clsx';
import { useAuthStore } from '../stores/authStore';
import { usePlanStore } from '../stores/planStore';
import { useToastStore } from '../stores/toastStore';
import { StepIndicator } from '../components/common/StepIndicator';
import { ApiClientError } from '../api/rest';
import { AuthHint, type HintField } from '../components/common/AuthHint';
import type { Plan } from '../types/plan';

// ─── Step Labels ──────────────────────────────────────────────────────

const STEPS = ['Account', 'Plan', 'Confirm'];

// ─── Password Validation ──────────────────────────────────────────────

// Mirrors Go auth.ValidatePasswordPolicy:
//   - At least 8 characters
//   - At least one uppercase letter
//   - At least one lowercase letter
//   - At least one digit
//   - At least one special character

interface PasswordChecks {
  minLength: boolean;
  hasUpper: boolean;
  hasLower: boolean;
  hasDigit: boolean;
  hasSpecial: boolean;
}

function checkPassword(password: string): PasswordChecks {
  return {
    minLength: password.length >= 8,
    hasUpper: /[A-Z]/.test(password),
    hasLower: /[a-z]/.test(password),
    hasDigit: /[0-9]/.test(password),
    hasSpecial: /[^A-Za-z0-9]/.test(password),
  };
}

function isPasswordValid(checks: PasswordChecks): boolean {
  return checks.minLength && checks.hasUpper && checks.hasLower && checks.hasDigit && checks.hasSpecial;
}

// ─── Step 1: Account ──────────────────────────────────────────────────

interface StepAccountProps {
  email: string;
  setEmail: (v: string) => void;
  password: string;
  setPassword: (v: string) => void;
  name: string;
  setName: (v: string) => void;
  tenantName: string;
  setTenantName: (v: string) => void;
  focusedField: HintField;
  onFieldFocus: (field: HintField) => void;
  onFieldBlur: () => void;
}

function StepAccount({
  email, setEmail, password, setPassword, name, setName, tenantName, setTenantName,
  focusedField: _focusedField, onFieldFocus, onFieldBlur,
}: StepAccountProps) {
  const [showPassword, setShowPassword] = useState(false);

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--aef-space-4)',
        animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
      }}
    >
      {/* Email */}
      <div className="dp-field" style={{ position: 'relative' }}>
        <label className="dp-field__label" htmlFor="reg-email">Email</label>
        <div style={{ position: 'relative' }}>
          <input
            id="reg-email" className="dp-field__input" type="email"
            placeholder="you@example.com" value={email}
            onChange={(e) => setEmail(e.target.value)}
            onFocus={() => onFieldFocus('email')}
            onBlur={onFieldBlur}
            autoFocus autoComplete="email"
            style={{ paddingLeft: 'var(--aef-space-8)' }}
          />
          <Mail
            size={14}
            style={{
              position: 'absolute', left: 'var(--aef-space-3)', top: '50%',
              transform: 'translateY(-50%)', color: 'var(--aef-text-secondary)', pointerEvents: 'none',
            }}
          />
        </div>
      </div>

      {/* Password with visibility toggle */}
      <div className="dp-field" style={{ position: 'relative' }}>
        <label className="dp-field__label" htmlFor="reg-password">Password</label>
        <div style={{ position: 'relative', marginTop: 'var(--aef-space-2)' }}>
          <input
            id="reg-password" className="dp-field__input"
            type={showPassword ? 'text' : 'password'}
            placeholder="MyP@ssw0rd" value={password}
            onChange={(e) => setPassword(e.target.value)}
            onFocus={() => onFieldFocus('password')}
            onBlur={onFieldBlur}
            autoComplete="new-password"
            style={{ paddingLeft: 'var(--aef-space-8)', paddingRight: 'var(--aef-space-8)' }}
          />
          <Lock
            size={14}
            style={{
              position: 'absolute', left: 'var(--aef-space-3)', top: '50%',
              transform: 'translateY(-50%)', color: 'var(--aef-text-secondary)', pointerEvents: 'none',
            }}
          />
          <button
            type="button"
            onClick={() => setShowPassword((prev) => !prev)}
            style={{
              position: 'absolute', right: 'var(--aef-space-2)', top: '50%',
              transform: 'translateY(-50%)', display: 'flex', alignItems: 'center', justifyContent: 'center',
              width: 28, height: 28, border: 'none', background: 'transparent',
              color: 'var(--aef-text-secondary)', cursor: 'pointer',
              borderRadius: 'var(--aef-radius-control)',
              transition: 'color var(--aef-duration-fast) var(--aef-ease-exit)',
            }}
            aria-label={showPassword ? 'Hide password' : 'Show password'}
            tabIndex={-1}
          >
            {showPassword ? <EyeOff size={14} /> : <Eye size={14} />}
          </button>
        </div>
      </div>

      {/* Name */}
      <div className="dp-field" style={{ position: 'relative' }}>
        <label className="dp-field__label" htmlFor="reg-name">Your name</label>
        <div style={{ position: 'relative' }}>
          <input
            id="reg-name" className="dp-field__input" type="text"
            placeholder="Jane Smith" value={name}
            onChange={(e) => setName(e.target.value)}
            onFocus={() => onFieldFocus('name')}
            onBlur={onFieldBlur}
            autoComplete="name"
            style={{ paddingLeft: 'var(--aef-space-8)' }}
          />
          <User
            size={14}
            style={{
              position: 'absolute', left: 'var(--aef-space-3)', top: '50%',
              transform: 'translateY(-50%)', color: 'var(--aef-text-secondary)', pointerEvents: 'none',
            }}
          />
        </div>
      </div>

      {/* Tenant name */}
      <div className="dp-field" style={{ position: 'relative' }}>
        <label className="dp-field__label" htmlFor="reg-tenant">Organization name</label>
        <div style={{ position: 'relative' }}>
          <input
            id="reg-tenant" className="dp-field__input" type="text"
            placeholder="Acme Corp" value={tenantName}
            onChange={(e) => setTenantName(e.target.value)}
            onFocus={() => onFieldFocus('organization')}
            onBlur={onFieldBlur}
            style={{ paddingLeft: 'var(--aef-space-8)' }}
          />
          <Building2
            size={14}
            style={{
              position: 'absolute', left: 'var(--aef-space-3)', top: '50%',
              transform: 'translateY(-50%)', color: 'var(--aef-text-secondary)', pointerEvents: 'none',
            }}
          />
        </div>
      </div>
    </div>
  );
}

// ─── Step 2: Plan Selection ───────────────────────────────────────────

interface StepPlanProps {
  plans: Plan[];
  selectedPlan: string;
  setSelectedPlan: (v: string) => void;
  isLoading: boolean;
  error: string | null;
  onRetry: () => void;
}

function StepPlan({ plans, selectedPlan, setSelectedPlan, isLoading, error, onRetry }: StepPlanProps) {
  // Loading state
  if (isLoading) {
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 'var(--aef-space-3)',
          padding: 'var(--aef-space-8) 0',
          animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
        }}
      >
        <div
          style={{
            width: 20,
            height: 20,
            borderRadius: '50%',
            border: '2px solid var(--aef-border-default)',
            borderTopColor: 'var(--aef-text-primary)',
            animation: 'aef-spin 0.8s linear infinite',
          }}
        />
        <p
          style={{
            fontFamily: 'var(--aef-font-body)', fontSize: 12,
            color: 'var(--aef-text-secondary)', margin: 0,
          }}
        >
          Loading available plans…
        </p>
      </div>
    );
  }

  // Error state — backend unreachable or request failed
  if (error) {
    return (
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 'var(--aef-space-4)',
          padding: 'var(--aef-space-6) var(--aef-space-4)',
          animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
        }}
      >
        <div
          style={{
            width: 36,
            height: 36,
            borderRadius: '50%',
            background: 'var(--aef-surface-error-subtle)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <AlertTriangle size={18} style={{ color: 'var(--aef-text-error)' }} />
        </div>
        <p
          style={{
            fontFamily: 'var(--aef-font-body)',
            fontSize: 12,
            color: 'var(--aef-text-error)',
            textAlign: 'center',
            margin: 0,
            lineHeight: 1.6,
            maxWidth: 320,
          }}
        >
          {error}
        </p>
        <button
          type="button"
          className="aef-btn aef-btn-inactive"
          onClick={onRetry}
        >
          <RefreshCw size={14} /> Retry
        </button>
      </div>
    );
  }

  const creatablePlans = plans.filter((p) => p.creatable);

  if (creatablePlans.length === 0) {
    return (
      <p
        style={{
          fontFamily: 'var(--aef-font-body)', fontSize: 12,
          color: 'var(--aef-text-secondary)', textAlign: 'center',
          padding: 'var(--aef-space-6) 0',
          animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
        }}
      >
        No plans are currently available. Please try again later.
      </p>
    );
  }

  return (
    <div
      className="dp-ability-grid"
      style={{
        animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
      }}
    >
      {creatablePlans.map((plan, i) => {
        const isSelected = selectedPlan === plan.name;
        return (
          <button
            key={plan.name}
            type="button"
            className={clsx('dp-ability-tile', isSelected && 'dp-ability-tile--selected')}
            onClick={() => setSelectedPlan(plan.name)}
            aria-pressed={isSelected}
            style={{
              animation: `aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) ${80 + i * 50}ms both`,
            }}
          >
            {/* Check badge */}
            <span className="dp-ability-tile__check">
              <Check size={14} />
            </span>

            {/* Icon */}
            <span className="dp-ability-tile__icon">
              <Shield size={18} />
            </span>

            <span className="dp-ability-tile__name">{plan.displayName}</span>
            <span className="dp-ability-tile__desc">
              {plan.maxTwins > 0 ? `Up to ${plan.maxTwins} Digital Parytys` : 'Unlimited Parytys'}
            </span>
            <span className="dp-ability-tile__desc">
              {plan.limits.subUsers > 0 ? `${plan.limits.subUsers} team members` : 'Unlimited members'}
            </span>
          </button>
        );
      })}
    </div>
  );
}

// ─── Step 3: Confirm ──────────────────────────────────────────────────

interface StepConfirmProps {
  email: string;
  name: string;
  tenantName: string;
  planName: string;
}

function StepConfirm({ email, name, tenantName, planName }: StepConfirmProps) {
  const rows: { label: string; value: string; icon: React.ReactNode }[] = [
    { label: 'Email', value: email || '—', icon: <Mail size={12} /> },
    { label: 'Name', value: name || '—', icon: <User size={12} /> },
    { label: 'Organization', value: tenantName || '—', icon: <Building2 size={12} /> },
    { label: 'Plan', value: planName || '—', icon: <Sparkles size={12} /> },
  ];

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 'var(--aef-space-3)',
        animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
      }}
    >
      <p
        style={{
          fontFamily: 'var(--aef-font-body)', fontSize: 11,
          color: 'var(--aef-text-secondary)', lineHeight: 1.6,
        }}
      >
        Review your information before creating your workspace.
      </p>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 0 }}>
        {rows.map((row, i) => (
          <div
            key={row.label}
            className="dp-confirm-row"
            style={{
              animation: `aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) ${80 + i * 40}ms both`,
            }}
          >
            <span className="dp-confirm-row__label" style={{ display: 'flex', alignItems: 'center', gap: 'var(--aef-space-2)' }}>
              <span style={{ color: 'var(--aef-text-secondary)', display: 'flex' }}>{row.icon}</span>
              {row.label}
            </span>
            <span className="dp-confirm-row__value">{row.value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ─── Page ─────────────────────────────────────────────────────────────

/**
 * Multi-step registration wizard.
 *
 * Fetches available plans on mount and walks the user through:
 * account creation, plan selection, and confirmation.
 */
export function RegisterPage() {
  const navigate = useNavigate();
  const register = useAuthStore((s) => s.register);
  const addToast = useToastStore((s) => s.addToast);
  const plans = usePlanStore((s) => s.plans);
  const plansLoading = usePlanStore((s) => s.isLoading);
  const plansError = usePlanStore((s) => s.error);
  const fetchPlans = usePlanStore((s) => s.fetchPlans);
  const clearPlanError = usePlanStore((s) => s.clearPlanError);

  const [step, setStep] = useState(0);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [tenantName, setTenantName] = useState('');
  const [selectedPlan, setSelectedPlan] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [focusedField, setFocusedField] = useState<HintField>(null);

  // 150ms debounce on blur to avoid flicker when tabbing between fields
  const blurTimer = useRef<ReturnType<typeof setTimeout>>();
  const handleBlur = useCallback(() => {
    blurTimer.current = setTimeout(() => setFocusedField(null), 150);
  }, []);
  const handleFocus = useCallback((field: HintField) => {
    if (blurTimer.current) clearTimeout(blurTimer.current);
    setFocusedField(field);
  }, []);

  // Auth pages are full-viewport surfaces — override body overflow:hidden
  // so vertical scrolling works when content exceeds the viewport.
  useEffect(() => {
    document.body.style.overflow = 'auto';
    return () => { document.body.style.overflow = 'hidden'; };
  }, []);

  // Fetch plans on mount
  useEffect(() => {
    fetchPlans();
  }, [fetchPlans]);

  const canAdvance = (): boolean => {
    switch (step) {
      case 0:
        return email.trim().length > 0 && isPasswordValid(checkPassword(password)) && name.trim().length > 0 && tenantName.trim().length > 0;
      case 1:
        return selectedPlan.length > 0;
      default:
        return true;
    }
  };

  const handleNext = useCallback(() => {
    if (!canAdvance()) return;
    if (step === STEPS.length - 1) {
      handleSubmit();
      return;
    }
    setError(null);
    setStep((s) => s + 1);
  }, [step, email, password, name, tenantName, selectedPlan]);

  const handlePrev = useCallback(() => {
    setError(null);
    setStep((s) => Math.max(0, s - 1));
  }, []);

  const handleSubmit = useCallback(async () => {
    setIsSubmitting(true);
    setError(null);
    try {
      await register({
        email: email.trim(),
        password,
        name: name.trim(),
        tenantName: tenantName.trim(),
        planName: selectedPlan,
      });
      addToast({ type: 'success', message: 'Workspace created! Welcome to Paryty.' });
      navigate('/');
    } catch (err) {
      if (err instanceof ApiClientError) {
        switch (err.status) {
          case 409:
            setError('An account with this email already exists.');
            break;
          case 429:
            setError('Too many registration attempts. Please try again later.');
            break;
          default:
            setError(err.message || 'Registration failed. Please try again.');
        }
      } else {
        setError('An unexpected error occurred. Please try again.');
      }
    } finally {
      setIsSubmitting(false);
    }
  }, [email, password, name, tenantName, selectedPlan, register, navigate, addToast]);

  const isLastStep = step === STEPS.length - 1;

  // Auto-select first plan
  useEffect(() => {
    if (step === 1 && !selectedPlan && plans.length > 0) {
      const first = plans.find((p) => p.creatable);
      if (first) setSelectedPlan(first.name);
    }
  }, [step, selectedPlan, plans]);

  return (
    <div className="auth-page" data-testid="register-page">
      <div
        className="auth-card auth-card--register aef-container-card"
        style={{
          animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) both',
        }}
      >
        {/* Header — Paryty brand */}
        <div className="aef-container-card__header">
          <span className="aef-container-card__icon">
            <Shield size={14} />
          </span>
          <span className="aef-container-card__title">Paryty</span>
        </div>

        <div className="aef-container-card__body">
          {/* Header */}
          <div className="auth-card__header">
            <h1
              className="auth-card__title"
              style={{
                animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
              }}
            >
              Create your workspace
            </h1>
            <p
              className="auth-card__subtitle"
              style={{
                animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 80ms both',
              }}
            >
              Step {step + 1} of {STEPS.length}: {STEPS[step]}
            </p>
          </div>

          {/* Form */}
          <form
            className="auth-form"
            onSubmit={(e: FormEvent) => { e.preventDefault(); handleNext(); }}
            noValidate
            style={{
              animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 100ms both',
            }}
          >
            {/* Error banner */}
            {error && (
              <div
                className="auth-form__error"
                role="alert"
                data-testid="register-error"
                style={{
                  animation: 'aef-scale-in var(--aef-duration-fast) var(--aef-ease-settle) both',
                }}
              >
                {error}
              </div>
            )}

            {/* Step content — keyed for re-animation on step change */}
            <div key={step}>
              {step === 0 && (
                <StepAccount
                  email={email} setEmail={setEmail}
                  password={password} setPassword={setPassword}
                  name={name} setName={setName}
                  tenantName={tenantName} setTenantName={setTenantName}
                  focusedField={focusedField}
                  onFieldFocus={handleFocus}
                  onFieldBlur={handleBlur}
                />
              )}
              {step === 1 && (
                <StepPlan
                  plans={plans}
                  selectedPlan={selectedPlan}
                  setSelectedPlan={setSelectedPlan}
                  isLoading={plansLoading}
                  error={plansError}
                  onRetry={() => { clearPlanError(); fetchPlans(); }}
                />
              )}
              {step === 2 && (
                <StepConfirm
                  email={email} name={name} tenantName={tenantName}
                  planName={plans.find((p) => p.name === selectedPlan)?.displayName ?? selectedPlan}
                />
              )}
            </div>

            {/* Steps + navigation */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                paddingTop: 'var(--aef-space-2)',
              }}
            >
              <StepIndicator total={STEPS.length} current={step} />
              <div style={{ display: 'flex', gap: 'var(--aef-space-2)' }}>
                {step > 0 && (
                  <button type="button" className="aef-btn aef-btn-inactive" onClick={handlePrev}>
                    <ArrowLeft size={14} /> Back
                  </button>
                )}
                <button
                  type="submit"
                  className="aef-btn aef-btn-active"
                  disabled={!canAdvance() || isSubmitting}
                  style={{ opacity: canAdvance() && !isSubmitting ? 1 : 0.4 }}
                  data-testid="register-submit"
                >
                  {isSubmitting ? (
                    'Creating…'
                  ) : isLastStep ? (
                    <><UserPlus size={14} /> Create Workspace</>
                  ) : (
                    <>Continue <ArrowRight size={14} /></>
                  )}
                </button>
              </div>
            </div>
          </form>

          {/* Footer link */}
          <div
            className="auth-card__footer"
            style={{
              animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 150ms both',
            }}
          >
            <span
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 11,
                color: 'var(--aef-text-secondary)',
              }}
            >
              Already have an account?{' '}
            </span>
            <Link
              to="/login"
              className="auth-link"
              data-testid="register-login-link"
            >
              Sign in
            </Link>
          </div>
        </div>
      </div>

      {/* AuthHint — floating contextual help, only during Account step */}
      {step === 0 && (
        <AuthHint
          focusedField={focusedField}
          emailValue={email}
          passwordValue={password}
          isRegister
        />
      )}
    </div>
  );
}
