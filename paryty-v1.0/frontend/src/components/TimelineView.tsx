import { useTimeline } from '../hooks/useTimeline';

export default function TimelineView() {
  const timeline = useTimeline();

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Timeline</h2>
        <div className="view-controls">
          <button onClick={() => timeline.play()} disabled={timeline.position.state === 'playing'}>
            Play
          </button>
          <button onClick={() => timeline.pause()} disabled={timeline.position.state !== 'playing'}>
            Pause
          </button>
          <button onClick={() => timeline.stop()}>Stop</button>
          <select
            value={timeline.config.speed}
            onChange={(e) => timeline.setSpeed(Number(e.target.value) as 0.5 | 1 | 2 | 4 | 8 | 16)}
          >
            <option value="0.5">0.5x</option>
            <option value="1">1x</option>
            <option value="2">2x</option>
            <option value="4">4x</option>
            <option value="8">8x</option>
            <option value="16">16x</option>
          </select>
          <input
            type="range"
            min={0}
            max={1}
            step={0.001}
            value={timeline.position.progress}
            onChange={(e) => timeline.seekTo(Number(e.target.value))}
          />
        </div>
      </div>
      <div className="timeline-content">
        <div className="timeline-info">
          <span>State: {timeline.position.state}</span>
          <span>Time: {timeline.position.currentTime}</span>
          <span>Snapshots: {timeline.snapshots.length}</span>
        </div>
        <div className="timeline-replay-placeholder">
          {timeline.snapshots.length === 0 && !timeline.isStreaming && (
            <p>Press Play to start timeline replay</p>
          )}
          {timeline.isStreaming && <p>Streaming timeline data...</p>}
          {timeline.snapshots.length > 0 && (
            <div>
              <p>Latest snapshot: {timeline.snapshots[timeline.snapshots.length - 1]?.timestamp}</p>
              <p>Nodes: {timeline.snapshots[timeline.snapshots.length - 1]?.nodes.length ?? 0}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
