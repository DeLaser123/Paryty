/**
 * StatusBar — Bottom status bar showing system health indicators.
 *
 * Displays: alert count badge, connection state dot, FPS counter placeholder,
 * and last update time.
 *
 * @module components/layout/StatusBar
 */

import { useMemo } from 'react';
import { Bell, Wifi, WifiOff, Monitor, Sparkles, Activity, Radio, Clock } from 'lucide-react';
import { useAlertsStore } from '../../stores/alertsStore';
import { useWebSocket } from '../../hooks/useWebSocket';
import { useTopologyStore } from '../../stores/topologyStore';
import { useParticleStore } from '../../stores/particleStore';

/**
 * Bottom status bar with alert count, connection indicator, and FPS.
 *
 * Height: 28px (`--aef-statusbar-height`).
 */
export function StatusBar() {
  const firingCount = useAlertsStore((s) => s.firingCount());
  const { state, isConnected } = useWebSocket();
  const version = useTopologyStore((s) => s.version);
  const visualParticleCount = useParticleStore((s) => s.visualParticleCount);
  const totalEventsReceived = useParticleStore((s) => s.totalEventsReceived);

  const transportMode = useParticleStore((s) => s.transportMode);

  const connectionLabel = useMemo(() => {
    if (isConnected) return 'Connected';
    if (state === 'connecting') return 'Connecting…';
    if (state === 'reconnecting') return 'Reconnecting…';
    return 'Disconnected';
  }, [state, isConnected]);

  const transportConfig = useMemo(() => {
    switch (transportMode) {
      case 'ws':      return { Icon: Wifi,    color: 'var(--aef-transport-ws)',      label: 'Live WS' };
      case 'rest':    return { Icon: Clock,   color: 'var(--aef-transport-rest)',    label: 'Buffered REST' };
      case 'sse':     return { Icon: Radio,   color: 'var(--aef-transport-sse)',     label: 'Stream SSE' };
      case 'offline': return { Icon: WifiOff, color: 'var(--aef-transport-offline)', label: 'Offline' };
      default:        return { Icon: Wifi,    color: 'var(--aef-transport-ws)',      label: 'Live WS' };
    }
  }, [transportMode]);

  return (
    <footer className="app-statusbar" data-testid="statusbar">
      {/* Alert count */}
      <div className="app-statusbar__item" data-testid="statusbar-alerts">
        <Bell size={12} />
        <span>{firingCount} firing</span>
      </div>

      {/* Connection state */}
      <div className="app-statusbar__item" data-testid="statusbar-connection">
        <span
          className="aef-dot"
          style={{
            background: isConnected
              ? 'var(--aef-status-live)'
              : state === 'connecting' || state === 'reconnecting'
                ? 'var(--aef-status-warning)'
                : 'var(--aef-counter-variant-b)',
          }}
        />
        {isConnected ? (
          <Wifi size={12} />
        ) : (
          <WifiOff size={12} />
        )}
        <span>{connectionLabel}</span>
      </div>

      {/* Transport mode indicator */}
      <div className="app-statusbar__item app-statusbar__transport" data-testid="statusbar-transport">
        {(() => {
          const IconComponent = transportConfig.Icon;
          return <IconComponent size={12} style={{ color: transportConfig.color }} />;
        })()}
        <span>{transportConfig.label}</span>
      </div>

      {/* FPS counter placeholder */}
      <div className="app-statusbar__item app-statusbar__fps" data-testid="statusbar-fps">
        <Monitor size={12} />
        <span>— fps</span>
      </div>

      {/* Visual particle counter (viewport-culled) */}
      <div className="app-statusbar__item" data-testid="statusbar-particles">
        <Sparkles size={12} />
        <span>{visualParticleCount} on screen</span>
      </div>

      {/* Total Paryty events received counter */}
      <div className="app-statusbar__item" data-testid="statusbar-events">
        <Activity size={12} />
        <span>{totalEventsReceived.toLocaleString()} events</span>
      </div>

      {/* Version / last update */}
      <div className="app-statusbar__item app-statusbar__right">
        <span className="aef-text-secondary">
          {version ? `v${version.slice(0, 8)}` : '—'}
        </span>
      </div>
    </footer>
  );
}
