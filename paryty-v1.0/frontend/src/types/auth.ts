/**
 * Auth types for Paryti multi-tenant SaaS platform.
 *
 * Covers users, tenants, registration, login, token refresh,
 * and sub-user management.
 *
 * @module types/auth
 */

import type { CurrentPlan } from './plan';

export interface User {
  id: string;
  email: string;
  name: string;
  role: 'admin' | 'operator' | 'viewer';
  tenantId: string;
  permissions: Record<string, boolean>;
  createdAt: string;
}

export interface Tenant {
  id: string;
  name: string;
  planName: string;
  createdAt: string;
}

export interface RegisterParams {
  email: string;
  password: string;
  name: string;
  tenantName: string;
  planName: string;
}

export interface LoginParams {
  email: string;
  password: string;
}

export interface LoginResponse {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  user: User;
  tenant: Tenant;
  /** Full plan definition (BFF pattern — injected by backend to avoid extra API call). */
  plan?: CurrentPlan;
}

export interface RefreshResponse {
  accessToken: string;
  refreshToken: string;
  expiresAt: string;
  user: User;
  tenant: Tenant;
}

export interface SubUser {
  id: string;
  email: string;
  name: string;
  role: 'admin' | 'operator' | 'viewer';
  permissions: Record<string, boolean>;
  createdAt: string;
}

export interface CreateSubUserParams {
  email: string;
  password: string;
  name: string;
  role: 'admin' | 'operator' | 'viewer';
  permissions?: Record<string, boolean>;
}
