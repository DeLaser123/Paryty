type LogLevel = 'debug' | 'info' | 'warn' | 'error';

interface LogEntry {
  timestamp: string;
  level: LogLevel;
  component: string;
  message: string;
  [key: string]: unknown;
}

function formatLog(entry: LogEntry): string {
  return JSON.stringify(entry);
}

export function createLogger(component: string) {
  const isProd = typeof import.meta !== 'undefined' && (import.meta as any).env?.PROD;

  function log(level: LogLevel, message: string, data?: Record<string, unknown>) {
    if (isProd && level === 'debug') return;
    const entry: LogEntry = {
      timestamp: new Date().toISOString(),
      level,
      component,
      message,
      ...data,
    };
    const output = formatLog(entry);
    switch (level) {
      case 'error': console.error(output); break;
      case 'warn': console.warn(output); break;
      case 'info': console.info(output); break;
      case 'debug': console.debug(output); break;
    }
  }

  return {
    debug: (msg: string, data?: Record<string, unknown>) => log('debug', msg, data),
    info: (msg: string, data?: Record<string, unknown>) => log('info', msg, data),
    warn: (msg: string, data?: Record<string, unknown>) => log('warn', msg, data),
    error: (msg: string, data?: Record<string, unknown>) => log('error', msg, data),
  };
}

export const authLogger = createLogger('auth');
export const apiLogger = createLogger('api');
export const storeLogger = createLogger('store');
export const rendererLogger = createLogger('renderer');
export const wsLogger = createLogger('websocket');
