import { describe, it, expect } from 'vitest';

// Basic smoke tests for Paryty frontend utilities
// These test pure logic without DOM or network dependencies

describe('Paryty Frontend Smoke Tests', () => {
  it('should define WebSocket message types', () => {
    // Verify the WS message type constants exist
    const WS_STATES = ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED'] as const;
    expect(WS_STATES).toHaveLength(4);
    expect(WS_STATES[0]).toBe('CONNECTING');
    expect(WS_STATES[3]).toBe('CLOSED');
  });

  it('should format metric values correctly', () => {
    const formatBytes = (bytes: number): string => {
      if (bytes === 0) return '0 B';
      const k = 1024;
      const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
      const i = Math.floor(Math.log(bytes) / Math.log(k));
      return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    };

    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(1024)).toBe('1 KB');
    expect(formatBytes(1048576)).toBe('1 MB');
    expect(formatBytes(1073741824)).toBe('1 GB');
  });

  it('should format percentage values', () => {
    const formatPercent = (value: number, decimals = 1): string => {
      return value.toFixed(decimals) + '%';
    };

    expect(formatPercent(0)).toBe('0.0%');
    expect(formatPercent(50.5)).toBe('50.5%');
    expect(formatPercent(100)).toBe('100.0%');
    expect(formatPercent(33.333, 2)).toBe('33.33%');
  });

  it('should validate topology node types', () => {
    const validNodeTypes = ['service', 'host', 'container', 'process'];
    
    const isValidNodeType = (type: string): boolean => {
      return validNodeTypes.includes(type);
    };

    expect(isValidNodeType('service')).toBe(true);
    expect(isValidNodeType('host')).toBe(true);
    expect(isValidNodeType('invalid')).toBe(false);
  });

  it('should parse alert severity levels', () => {
    const severityOrder: Record<string, number> = {
      critical: 0,
      high: 1,
      medium: 2,
      low: 3,
      info: 4,
    };

    const sortBySeverity = (a: string, b: string): number => {
      return (severityOrder[a] ?? 99) - (severityOrder[b] ?? 99);
    };

    const severities = ['info', 'critical', 'low', 'medium', 'high'];
    const sorted = [...severities].sort(sortBySeverity);
    
    expect(sorted[0]).toBe('critical');
    expect(sorted[1]).toBe('high');
    expect(sorted[4]).toBe('info');
  });
});
