// Core types for the Paryty platform

export type Timestamp = string; // ISO 8601

export type UUID = string;

export interface Labels {
  [key: string]: string;
}

export interface PaginationRequest {
  page: number;
  pageSize: number;
}

export interface PaginationResponse {
  page: number;
  pageSize: number;
  total: number;
}

export interface ApiError {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface TimeRange {
  start: Timestamp;
  end: Timestamp;
}

export type DataResolution = 'raw' | '1m' | '1h' | '1d';
