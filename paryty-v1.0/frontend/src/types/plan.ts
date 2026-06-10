/**
 * Plan types for Paryty multi-tenant SaaS platform.
 *
 * Defines plan metadata, current plan info, feature flags,
 * resource types, and usage tracking.
 *
 * @module types/plan
 */

export interface Plan {
  name: string;
  displayName: string;
  maxTwins: number;
  creatable: boolean;
  features: Record<string, boolean>;
  limits: {
    agentsPerTwin: number;
    dataRetentionDays: number;
    subUsers: number;
    alertRulesPerTenant: number;
    metricsResolution: string;
  };
  quotas: {
    ingestionBytesPerDay: number;
    queryRequestsPerMinute: number;
  };
}

export interface CurrentPlan {
  planName: string;
  features: Record<string, boolean>;
  limits: Record<string, number>;
  quotas: Record<string, number>;
  startedAt: string;
  expiresAt?: string;
}

export type FeatureFlag = string;

export type ResourceType = 'twins' | 'agents' | 'subUsers' | 'alertRules';

export interface UsageInfo {
  resource: ResourceType;
  current: number;
  max: number;
  percentage: number;
}
