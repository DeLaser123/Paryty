/**
 * StatusBar — Bottom status bar: pure signal/health indicators only.
 *
 * Shows: transport mode, WebSocket signal, FPS, on-screen particle count,
 * node count, and topology version. Alerts and connection text labels
 * have moved to the Topbar.
 *
 * @module components/layout/StatusBar
 */

import { useMemo, useRef, useEffect, useState } from 'react';
import { Wifi, WifiOff, Monitor, Sparkles, Radio, Clock, GitBranch, History, GanttChartSquare } from 'lucide-react';
import { useWebSocket } from '../../hooks/useWebSocket';
import { useTopologyStore } from '../../stores/topologyStore';
import { useParticleStore } from '../../stores/particleStore';
import { useLocation } from 'react-router-dom';
import { Tooltip } from '../common/Tooltip';

/**
 * Bottom status bar — 28px, pure indicators.
 * Exempt from transport-mode saturation degradation.
 */
export function StatusBar() {
  const { state, isConnected } = useWebSocket();
  const version = useTopologyStore((s) => s.version);
  const topology = useTopologyStore((s) => s.topology);
  const timelineDrawerOpen = useTopologyStore((s) => s.timelineDrawerOpen);
  const toggleTimelineDrawer = useTopologyStore((s) => s.toggleTimelineDrawer);
  const visualParticleCount = useParticleStore((s) => s.visualParticleCount);
  const transportMode = useParticleStore((s) => s.transportMode);
  const location = useLocation();
  const isTopology = location.pathname === '/topology';

  // Events-per-second rate derived from totalEventsReceived counter
  const totalEventsReceived = useParticleStore((s) => s.totalEventsReceived);
  const prevEventsRef = useRef(totalEventsReceived);
  const prevTimeRef = useRef(Date.now());
  const [eventsPerSec, setEventsPerSec] = useState(0);

  useEffect(() => {
    const id = setInterval(() => {
      const now = Date.now();
      const dt = (now - prevTimeRef.current) / 1000;
      const delta = totalEventsReceived - prevEventsRef.current;
      setEventsPerSec(dt > 0 ? Math.round(delta / dt) : 0);
      prevEventsRef.current = totalEventsReceived;
      prevTimeRef.current = now;
    }, 1000);
    return () => clearInterval(id);
  }, [totalEventsReceived]);

  const nodeCount = topology?.nodes.length ?? 0;

  const transportConfig = useMemo(() => {
    switch (transportMode) {
      case 'ws':      return { Icon: Wifi,    color: 'var(--aef-transport-ws)',      label: 'WS' };
      case 'rest':    return { Icon: Clock,   color: 'var(--aef-transport-rest)',    label: 'REST' };
      case 'sse':     return { Icon: Radio,   color: 'var(--aef-transport-sse)',     label: 'SSE' };
      case 'offline': return { Icon: WifiOff, color: 'var(--aef-transport-offline)', label: 'Offline' };
      default:        return { Icon: Wifi,    color: 'var(--aef-transport-ws)',      label: 'WS' };
    }
  }, [transportMode]);

  return (
    <footer className="app-statusbar" data-testid="statusbar">

      {/* Transport + signal */}
      <div className="app-statusbar__item" data-testid="statusbar-transport">
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
        {(() => {
          const IconComponent = transportConfig.Icon;
          return <IconComponent size={12} style={{ color: transportConfig.color }} />;
        })()}
        <span style={{ color: transportConfig.color }}>{transportConfig.label}</span>
      </div>

      {/* Events per second */}
      <div className="app-statusbar__item" data-testid="statusbar-evtrate">
        <Radio size={12} />
        <span>{eventsPerSec} evt/s</span>
      </div>

      {/* Visible particles */}
      <div className="app-statusbar__item" data-testid="statusbar-particles">
        <Sparkles size={12} />
        <span>{visualParticleCount} particles</span>
      </div>

      {/* Node count */}
      <div className="app-statusbar__item" data-testid="statusbar-nodes">
        <GitBranch size={12} />
        <span>{nodeCount} nodes</span>
      </div>

      {/* FPS */}
      <div className="app-statusbar__item app-statusbar__fps" data-testid="statusbar-fps">
        <Monitor size={12} />
        <span>— fps</span>
      </div>

      {/* Version — right-aligned */}
      <div className="app-statusbar__item app-statusbar__right" data-testid="statusbar-version">
        <span>{version ? `v${version.slice(0, 8)}` : '—'}</span>
      </div>

      {/* Timeline drawer toggle — centered, state-aware icon */}
      {isTopology && (
        <Tooltip label={timelineDrawerOpen ? 'Collapse timeline' : 'Expand timeline'}>
          <button
            className={'statusbar-timeline-toggle' + (timelineDrawerOpen ? ' statusbar-timeline-toggle--active' : '')}
            onClick={toggleTimelineDrawer}
            aria-label={timelineDrawerOpen ? 'Collapse timeline' : 'Expand timeline'}
            data-testid="statusbar-timeline-toggle"
          >
            {timelineDrawerOpen ? <GanttChartSquare size={14} /> : <History size={14} />}
          </button>
        </Tooltip>
      )}

    </footer>
  );
}
