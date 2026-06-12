/**
 * API key types for Paryty multi-tenant SaaS platform.
 *
 * @module types/apiKeys
 */

export interface ApiKey {
  id: string;
  name: string;
  prefix: string;
  createdAt: string;
  lastUsedAt?: string;
}

export interface CreateApiKeyResponse {
  id: string;
  name: string;
  key: string; // Full key — shown once!
  prefix: string;
  createdAt: string;
}

export interface RotateApiKeyResponse {
  id: string;
  key: string; // New full key — shown once!
  prefix: string;
}
