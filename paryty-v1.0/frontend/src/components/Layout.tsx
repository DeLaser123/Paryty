// Layout component wrapping the main application

import { useWebSocket } from '../hooks/useWebSocket';
import type { WsState } from '../api/websocket';

interface LayoutProps {
  children: React.ReactNode;
}

export default function Layout({ children }: LayoutProps) {
  const { state, isConnected } = useWebSocket();

  return (
    <div className="app-layout">
      <header className="app-header">
        <div className="header-brand">
          <h1>Paryty</h1>
        </div>
        <nav className="header-nav">
          <a href="/">Topology</a>
          <a href="/metrics">Metrics</a>
          <a href="/timeline">Timeline</a>
          <a href="/alerts">Alerts</a>
        </nav>
        <div className="header-status">
          <ConnectionIndicator state={state} isConnected={isConnected} />
        </div>
      </header>
      <main className="app-main">
        {children}
      </main>
    </div>
  );
}

interface ConnectionIndicatorProps {
  state: WsState;
  isConnected: boolean;
}

function ConnectionIndicator({ state, isConnected }: ConnectionIndicatorProps) {
  const statusClass = isConnected ? 'connected' : state === 'connecting' ? 'connecting' : 'disconnected';
  const label = isConnected ? 'Connected' : state === 'connecting' ? 'Connecting...' : state === 'reconnecting' ? 'Reconnecting...' : 'Disconnected';

  return (
    <div className="connection-indicator">
      <span className={`status-dot ${statusClass}`} />
      <span>{label}</span>
    </div>
  );
}
