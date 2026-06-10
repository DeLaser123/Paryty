/**
 * LoginPage — Paryty authentication page.
 *
 * Centered container-card with brand mark, icon-rich form fields,
 * password visibility toggle, and staggered entrance animations.
 * Renders WITHOUT AppShell chrome — pure auth surface.
 *
 * @module pages/LoginPage
 */

import { useState, useCallback, useEffect, useRef, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  LogIn,
  Mail,
  Lock,
  Eye,
  EyeOff,
  Shield,
} from 'lucide-react';
import { useAuthStore } from '../stores/authStore';
import { useToastStore } from '../stores/toastStore';
import { AuthHint } from '../components/common/AuthHint';
import { ApiClientError } from '../api/rest';

export function LoginPage() {
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const addToast = useToastStore((s) => s.addToast);

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [focusedField, setFocusedField] = useState<'email' | 'password' | null>(null);

  // 150ms debounce on blur to avoid flicker when tabbing between fields
  const blurTimer = useRef<ReturnType<typeof setTimeout>>();
  const handleBlur = useCallback(() => {
    blurTimer.current = setTimeout(() => setFocusedField(null), 150);
  }, []);
  const handleFocus = useCallback((field: 'email' | 'password') => {
    if (blurTimer.current) clearTimeout(blurTimer.current);
    setFocusedField(field);
  }, []);

  // Auth pages are full-viewport surfaces — override body overflow:hidden
  // so vertical scrolling works when content exceeds the viewport.
  useEffect(() => {
    document.body.style.overflow = 'auto';
    return () => { document.body.style.overflow = 'hidden'; };
  }, []);

  const handleSubmit = useCallback(async (e: FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!email.trim() || !password.trim()) {
      setError('Email and password are required.');
      return;
    }

    setIsSubmitting(true);
    try {
      await login({ email: email.trim(), password });
      addToast({ type: 'success', message: 'Welcome back!' });
      navigate('/');
    } catch (err) {
      if (err instanceof ApiClientError) {
        switch (err.status) {
          case 401:
            setError('Invalid email or password.');
            break;
          case 429:
            setError('Too many login attempts. Please try again later.');
            break;
          default:
            setError(err.message || 'Login failed. Please try again.');
        }
      } else {
        setError('An unexpected error occurred. Please try again.');
      }
    } finally {
      setIsSubmitting(false);
    }
  }, [email, password, login, navigate, addToast]);

  return (
    <div className="auth-page" data-testid="login-page">
      <div
        className="auth-card aef-container-card"
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
          {/* Title / subtitle */}
          <div className="auth-card__header">
            <h1
              className="auth-card__title"
              style={{
                animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 60ms both',
              }}
            >
              Sign in to Paryty
            </h1>
            <p
              className="auth-card__subtitle"
              style={{
                animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 90ms both',
              }}
            >
              Enter your credentials to access your observability workspace.
            </p>
          </div>

          {/* Form */}
          <form
            className="auth-form"
            onSubmit={handleSubmit}
            noValidate
            style={{
              animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 120ms both',
            }}
          >
            {/* Error banner */}
            {error && (
              <div
                className="auth-form__error"
                role="alert"
                data-testid="login-error"
                style={{
                  animation: 'aef-scale-in var(--aef-duration-fast) var(--aef-ease-settle) both',
                }}
              >
                {error}
              </div>
            )}

            {/* Email field */}
            <div className="dp-field" style={{ position: 'relative' }}>
              <label className="dp-field__label" htmlFor="login-email">
                Email
              </label>
              <div style={{ position: 'relative' }}>
                <input
                  id="login-email"
                  className="dp-field__input"
                  type="email"
                  placeholder="you@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  onFocus={() => handleFocus('email')}
                  onBlur={handleBlur}
                  autoFocus
                  autoComplete="email"
                  data-testid="login-email"
                  style={{ paddingLeft: 'var(--aef-space-8)' }}
                />
                <Mail
                  size={14}
                  style={{
                    position: 'absolute',
                    left: 'var(--aef-space-3)',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    color: 'var(--aef-text-secondary)',
                    pointerEvents: 'none',
                  }}
                />
              </div>
            </div>

            {/* Password field with visibility toggle */}
            <div className="dp-field" style={{ position: 'relative' }}>
              <label className="dp-field__label" htmlFor="login-password">
                Password
              </label>
              <div style={{ position: 'relative' }}>
                <input
                  id="login-password"
                  className="dp-field__input"
                  type={showPassword ? 'text' : 'password'}
                  placeholder="Enter your password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  onFocus={() => handleFocus('password')}
                  onBlur={handleBlur}
                  autoComplete="current-password"
                  data-testid="login-password"
                  style={{
                    paddingLeft: 'var(--aef-space-8)',
                    paddingRight: 'var(--aef-space-8)',
                  }}
                />
                <Lock
                  size={14}
                  style={{
                    position: 'absolute',
                    left: 'var(--aef-space-3)',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    color: 'var(--aef-text-secondary)',
                    pointerEvents: 'none',
                  }}
                />
                <button
                  type="button"
                  onClick={() => setShowPassword((prev) => !prev)}
                  style={{
                    position: 'absolute',
                    right: 'var(--aef-space-2)',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    width: 28,
                    height: 28,
                    border: 'none',
                    background: 'transparent',
                    color: 'var(--aef-text-secondary)',
                    cursor: 'pointer',
                    borderRadius: 'var(--aef-radius-control)',
                    transition: 'color var(--aef-duration-fast) var(--aef-ease-exit)',
                  }}
                  aria-label={showPassword ? 'Hide password' : 'Show password'}
                  tabIndex={-1}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color = 'var(--aef-text-primary)';
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color = 'var(--aef-text-secondary)';
                  }}
                >
                  {showPassword ? <EyeOff size={14} /> : <Eye size={14} />}
                </button>
              </div>
            </div>

            {/* Submit button */}
            <button
              type="submit"
              className="aef-btn aef-btn-active"
              disabled={isSubmitting}
              style={{
                width: '100%',
                padding: 'var(--aef-space-3) var(--aef-space-4)',
                opacity: isSubmitting ? 0.6 : 1,
                marginTop: 'var(--aef-space-2)',
              }}
              data-testid="login-submit"
            >
              <LogIn size={14} />
              {isSubmitting ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          {/* Footer link */}
          <div
            className="auth-card__footer"
            style={{
              animation: 'aef-panel-enter var(--aef-duration-standard) var(--aef-ease-settle) 180ms both',
            }}
          >
            <span
              style={{
                fontFamily: 'var(--aef-font-body)',
                fontSize: 11,
                color: 'var(--aef-text-secondary)',
              }}
            >
              Don&apos;t have an account?{' '}
            </span>
            <Link
              to="/register"
              className="auth-link"
              data-testid="login-register-link"
            >
              Create one
            </Link>
          </div>
        </div>
      </div>

      {/* AuthHint — floating contextual help */}
      <AuthHint focusedField={focusedField} emailValue={email} />
    </div>
  );
}
