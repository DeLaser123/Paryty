/**
 * @vitest-environment jsdom
 *
 * Tests for LoginPage — user authentication page.
 *
 * Verifies form fields, submit button, register link, error display,
 * and successful login flow.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

// ─── Mock authStore ───────────────────────────────────────────────────

const mockLogin = vi.fn();

vi.mock('../stores/authStore', () => ({
  useAuthStore: (selector: (state: unknown) => unknown) => {
    const state = {
      login: mockLogin,
    };
    return selector(state);
  },
}));

// ─── Mock toastStore ──────────────────────────────────────────────────

const mockAddToast = vi.fn();

vi.mock('../stores/toastStore', () => ({
  useToastStore: (selector: (state: unknown) => unknown) => {
    const state = {
      addToast: mockAddToast,
    };
    return selector(state);
  },
}));

// ─── Mock ApiClientError ──────────────────────────────────────────────

vi.mock('../api/rest', () => ({
  ApiClientError: class extends Error {
    status: number;
    code?: string;
    constructor(message: string, status: number, code?: string) {
      super(message);
      this.status = status;
      this.code = code;
      this.name = 'ApiClientError';
    }
  },
}));

// ─── Mock useNavigate ─────────────────────────────────────────────────

const mockNavigate = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// ─── Component Under Test ─────────────────────────────────────────────

import { LoginPage } from '../pages/LoginPage';

// ─── Helpers ──────────────────────────────────────────────────────────

function renderLoginPage() {
  return render(
    <MemoryRouter>
      <LoginPage />
    </MemoryRouter>,
  );
}

// ─── Tests ────────────────────────────────────────────────────────────

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockLogin.mockReset();
    mockAddToast.mockReset();
    mockNavigate.mockReset();
  });

  afterEach(() => {
    cleanup();
  });

  // ── Rendering ──────────────────────────────────────────────────

  it('renders email input field', () => {
    renderLoginPage();
    const emailInput = screen.getByTestId('login-email');
    expect(emailInput).toBeDefined();
    expect(emailInput.getAttribute('type')).toBe('email');
  });

  it('renders password input field', () => {
    renderLoginPage();
    const passwordInput = screen.getByTestId('login-password');
    expect(passwordInput).toBeDefined();
    expect(passwordInput.getAttribute('type')).toBe('password');
  });

  it('renders submit button', () => {
    renderLoginPage();
    const submitBtn = screen.getByTestId('login-submit');
    expect(submitBtn).toBeDefined();
    expect(submitBtn.textContent).toContain('Sign in');
  });

  it('renders link to register page', () => {
    renderLoginPage();
    const registerLink = screen.getByTestId('login-register-link');
    expect(registerLink).toBeDefined();
    expect(registerLink.getAttribute('href')).toBe('/register');
    expect(registerLink.textContent).toBe('Create one');
  });

  it('renders the page title', () => {
    renderLoginPage();
    expect(screen.getByText('Sign in to Paryty')).toBeDefined();
  });

  // ── Error Display ──────────────────────────────────────────────

  it('shows error message on failed login (401)', async () => {
    const { ApiClientError } = await import('../api/rest');
    mockLogin.mockRejectedValueOnce(
      new ApiClientError('Invalid credentials', 401),
    );

    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.type(screen.getByTestId('login-password'), 'wrong');
    await user.click(screen.getByTestId('login-submit'));

    const errorEl = await screen.findByTestId('login-error');
    expect(errorEl).toBeDefined();
    expect(errorEl.textContent).toBe('Invalid email or password.');
  });

  it('shows rate-limit error on 429', async () => {
    const { ApiClientError } = await import('../api/rest');
    mockLogin.mockRejectedValueOnce(
      new ApiClientError('Too many requests', 429),
    );

    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.type(screen.getByTestId('login-password'), 'secret123');
    await user.click(screen.getByTestId('login-submit'));

    const errorEl = await screen.findByTestId('login-error');
    expect(errorEl.textContent).toContain('Too many login attempts');
  });

  it('shows default error message for unknown ApiClientError status', async () => {
    const { ApiClientError } = await import('../api/rest');
    mockLogin.mockRejectedValueOnce(
      new ApiClientError('Server error', 500),
    );

    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.type(screen.getByTestId('login-password'), 'secret123');
    await user.click(screen.getByTestId('login-submit'));

    const errorEl = await screen.findByTestId('login-error');
    expect(errorEl.textContent).toBe('Server error');
  });

  it('shows generic error for non-ApiClientError exceptions', async () => {
    mockLogin.mockRejectedValueOnce(new Error('Network failure'));

    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.type(screen.getByTestId('login-password'), 'secret123');
    await user.click(screen.getByTestId('login-submit'));

    const errorEl = await screen.findByTestId('login-error');
    expect(errorEl.textContent).toContain('An unexpected error occurred');
  });

  // ── Validation ─────────────────────────────────────────────────

  it('shows validation error when email is empty', async () => {
    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-password'), 'secret123');
    await user.click(screen.getByTestId('login-submit'));

    const errorEl = await screen.findByTestId('login-error');
    expect(errorEl.textContent).toContain('Email and password are required');
  });

  it('shows validation error when password is empty', async () => {
    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.click(screen.getByTestId('login-submit'));

    const errorEl = await screen.findByTestId('login-error');
    expect(errorEl.textContent).toContain('Email and password are required');
  });

  // ── Successful Login ───────────────────────────────────────────

  it('calls login() and navigates to / on success', async () => {
    mockLogin.mockResolvedValueOnce(undefined);

    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.type(screen.getByTestId('login-password'), 'secret123');
    await user.click(screen.getByTestId('login-submit'));

    // Wait for async login to complete
    await vi.waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith({
        email: 'test@example.com',
        password: 'secret123',
      });
    });

    expect(mockAddToast).toHaveBeenCalledWith({
      type: 'success',
      message: 'Welcome back!',
    });
    expect(mockNavigate).toHaveBeenCalledWith('/');
  });

  it('disables submit button and shows "Signing in…" while submitting', async () => {
    // Make login hang so we can check the submitting state
    mockLogin.mockImplementation(() => new Promise(() => {})); // never resolves

    const user = userEvent.setup();
    renderLoginPage();

    await user.type(screen.getByTestId('login-email'), 'test@example.com');
    await user.type(screen.getByTestId('login-password'), 'secret123');
    await user.click(screen.getByTestId('login-submit'));

    const submitBtn = screen.getByTestId('login-submit');
    expect(submitBtn.textContent).toContain('Signing in…');
    expect(submitBtn.hasAttribute('disabled')).toBe(true);
  });
});
