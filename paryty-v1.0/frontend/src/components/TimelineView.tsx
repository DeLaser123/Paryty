import { useTimeline } from '../hooks/useTimeline';
import { ParytySelect } from './common/ParytySelect';

export default function TimelineView() {
  const timeline = useTimeline();

  return (
    <div className="view-container">
      <div className="view-header">
        <h2>Timeline</h2>
        <div className="view-controls">
          <button className="aef-btn aef-btn-inactive" onClick={() => timeline.play()} disabled={timeline.position.state === 'playing'}>
            Play
          </button>
          <button className="aef-btn aef-btn-inactive" onClick={() => timeline.pause()} disabled={timeline.position.state !== 'playing'}>
            Pause
          </button>
          <button className="aef-btn aef-btn-inactive" onClick={() => timeline.stop()}>Stop</button>
          <ParytySelect
            options={[
              { label: '0.5x', value: '0.5' },
              { label: '1x', value: '1' },
              { label: '2x', value: '2' },
              { label: '4x', value: '4' },
              { label: '8x', value: '8' },
              { label: '16x', value: '16' },
            ]}
            value={String(timeline.config.speed)}
            onChange={(val) => timeline.setSpeed(Number(val) as 0.5 | 1 | 2 | 4 | 8 | 16)}
            placeholder="Speed"
          />
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
