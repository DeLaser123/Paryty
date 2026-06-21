/**
 * Twins API client — CRUD operations for Digital Paryty management.
 *
 * Uses the shared RestClient for authenticated API calls.
 * All methods follow the same patterns as other API clients in the codebase.
 *
 * @module api/twins
 */

import { getRestClient } from './rest';
import type { TwinDetails, TwinConfig } from '../types/digitalParyty';

// ─── Types ──────────────────────────────────────────────────────────────────

/** Payload for creating a new twin. */
export interface CreateTwinPayload {
  name: string;
  description?: string;
  config?: TwinConfig;
  abilities?: string[];
  agent_ids?: string[];
}

/** Payload for updating an existing twin. */
export interface UpdateTwinPayload {
  name?: string;
  description?: string;
  config?: TwinConfig;
}



// ─── API Methods ────────────────────────────────────────────────────────────

/**
 * Fetch all twins for the current tenant.
 *
 * @param params - Optional query parameters for filtering/pagination
 * @returns List of twin summaries
 */
export async function fetchTwins(params?: {
  page?: number;
  limit?: number;
  status?: string;
  search?: string;
}): Promise<TwinDetails[]> {
  const client = getRestClient();
  const queryParams: Record<string, string> = {};

  if (params?.page !== undefined) queryParams['page'] = String(params.page);
  if (params?.limit !== undefined) queryParams['limit'] = String(params.limit);
  if (params?.status) queryParams['status'] = params.status;
  if (params?.search) queryParams['search'] = params.search;

  const query = new URLSearchParams(queryParams).toString();
  const path = `/api/v1/twins${query ? `?${query}` : ''}`;

  const response = await client.get<{ data: TwinDetails[] }>(path);
  return response.data ?? [];
}

/**
 * Fetch a single twin by ID.
 *
 * @param twinId - The twin identifier
 * @returns Twin details
 */
export async function fetchTwinById(twinId: string): Promise<TwinDetails> {
  const client = getRestClient();
  const response = await client.get<{ data: TwinDetails }>(`/api/v1/twins/${encodeURIComponent(twinId)}`);
  return response.data;
}

/**
 * Create a new Digital Paryty twin.
 *
 * @param payload - Twin creation data
 * @returns The created twin details
 */
export async function createTwin(payload: CreateTwinPayload): Promise<TwinDetails> {
  const client = getRestClient();
  const response = await client.post<{ data: TwinDetails }>('/api/v1/twins', payload);
  return response.data;
}

/**
 * Update an existing twin's configuration.
 *
 * @param twinId - The twin identifier
 * @param payload - Fields to update
 * @returns The updated twin details
 */
export async function updateTwin(twinId: string, payload: UpdateTwinPayload): Promise<TwinDetails> {
  const client = getRestClient();
  const response = await client.put<{ data: TwinDetails }>(
    `/api/v1/twins/${encodeURIComponent(twinId)}`,
    payload,
  );
  return response.data;
}

/**
 * Delete a twin by ID.
 *
 * @param twinId - The twin identifier to delete
 */
export async function deleteTwin(twinId: string): Promise<void> {
  const client = getRestClient();
  await client.delete<void>(`/api/v1/twins/${encodeURIComponent(twinId)}`);
}

/**
 * Fetch twin metrics summary (agent count, health distribution, etc.).
 *
 * @param twinId - The twin identifier
 * @returns Metrics summary object
 */
export async function fetchTwinMetrics(twinId: string): Promise<Record<string, unknown>> {
  const client = getRestClient();
  // BUGFIX: Backend wraps response in {"data": ...} envelope. Extract .data.
  const resp = await client.get<{ data: Record<string, unknown> }>(
    `/api/v1/twins/${encodeURIComponent(twinId)}/metrics`,
  );
  return resp.data ?? {};
}
