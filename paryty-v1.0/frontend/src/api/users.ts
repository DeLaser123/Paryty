/**
 * Users API client — CRUD operations for sub-user management.
 *
 * Uses the shared RestClient for authenticated API calls.
 * Follows the same patterns as other API clients in the codebase.
 *
 * @module api/users
 */

import { getRestClient } from './rest';
import type { SubUser, CreateSubUserParams } from '../types/auth';

// ─── Types ──────────────────────────────────────────────────────────────────

/** Payload for updating a user's role and permissions. */
export interface UpdateUserPayload {
  name?: string;
  role?: 'admin' | 'operator' | 'viewer';
  permissions?: Record<string, boolean>;
}

/** Response shape for user list operations. */
export interface UserListResponse {
  users: SubUser[];
  total: number;
}

// ─── API Methods ────────────────────────────────────────────────────────────

/**
 * Fetch all sub-users for the current tenant.
 *
 * @param params - Optional query parameters for filtering/pagination
 * @returns List of sub-users
 */
export async function fetchUsers(params?: {
  page?: number;
  limit?: number;
  search?: string;
}): Promise<UserListResponse> {
  const client = getRestClient();
  const queryParams: Record<string, string> = {};

  if (params?.page !== undefined) queryParams['page'] = String(params.page);
  if (params?.limit !== undefined) queryParams['limit'] = String(params.limit);
  if (params?.search) queryParams['search'] = params.search;

  const query = new URLSearchParams(queryParams).toString();
  const path = `/api/v1/users${query ? `?${query}` : ''}`;

  // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
  const response = await client.get<{ data: SubUser[] }>(path);
  return { users: response.data ?? [], total: response.data?.length ?? 0 };
}

/**
 * Fetch a single user by ID.
 *
 * @param userId - The user identifier
 * @returns User details
 */
export async function fetchUserById(userId: string): Promise<SubUser> {
  const client = getRestClient();
  // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
  const response = await client.get<{ data: SubUser }>(`/api/v1/users/${encodeURIComponent(userId)}`);
  return response.data;
}

/**
 * Create a new sub-user.
 *
 * @param payload - User creation data
 * @returns The created user details
 */
export async function createUser(payload: CreateSubUserParams): Promise<SubUser> {
  const client = getRestClient();
  // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
  const response = await client.post<{ data: SubUser }>('/api/v1/users', payload);
  return response.data;
}

/**
 * Update an existing user's role and permissions.
 *
 * @param userId - The user identifier
 * @param payload - Fields to update
 * @returns The updated user details
 */
export async function updateUser(userId: string, payload: UpdateUserPayload): Promise<SubUser> {
  const client = getRestClient();
  // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
  const response = await client.put<{ data: SubUser }>(
    `/api/v1/users/${encodeURIComponent(userId)}`,
    payload,
  );
  return response.data;
}

/**
 * Delete a user by ID.
 *
 * @param userId - The user identifier to delete
 */
export async function deleteUser(userId: string): Promise<void> {
  const client = getRestClient();
  await client.delete<void>(`/api/v1/users/${encodeURIComponent(userId)}`);
}
