/**
 * SnapshotList — List of available timeline snapshots.
 *
 * Shows timestamps, click to load, bookmark toggle.
 * Uses the table card pattern from the design system.
 *
 * @module components/timeline/SnapshotList
 */

import { memo, useCallback } from 'react';
import { Bookmark, BookmarkCheck } from 'lucide-react';
import { useTimelineStore } from '../../stores/timelineStore';

/**
 * Scrollable list of available timeline snapshots.
 *
 * Uses the design system table card pattern.
 * Each row shows timestamp and has a bookmark toggle.
 */
export const SnapshotList = memo(function SnapshotList() {
  const availableSnapshots = useTimelineStore((s) => s.availableSnapshots);
  const bookmarks = useTimelineStore((s) => s.bookmarks);
  const loadSnapshot = useTimelineStore((s) => s.loadSnapshot);
  const addBookmark = useTimelineStore((s) => s.addBookmark);
  const removeBookmark = useTimelineStore((s) => s.removeBookmark);
  const selectedSnapshot = useTimelineStore((s) => s.selectedSnapshot);

  const isBookmarked = useCallback(
    (snapshotId: string) => bookmarks.some((b) => b.timestamp === availableSnapshots.find((s) => s.id === snapshotId)?.timestamp),
    [bookmarks, availableSnapshots],
  );

  const handleBookmarkToggle = useCallback(
    (snapshotId: string) => {
      const existing = bookmarks.find(
        (b) => b.timestamp === availableSnapshots.find((s) => s.id === snapshotId)?.timestamp,
      );
      if (existing) {
        removeBookmark(existing.id);
      } else {
        addBookmark(`Snapshot ${snapshotId}`);
      }
    },
    [bookmarks, availableSnapshots, addBookmark, removeBookmark],
  );

  if (availableSnapshots.length === 0) {
    return (
      <div className="aef-viz-well" data-testid="snapshot-list-empty">
        <span className="aef-viz-well__label">No snapshots available</span>
      </div>
    );
  }

  return (
    <div className="aef-table-card" data-testid="snapshot-list">
      <div className="aef-table-card__header">
        <span>Snapshots</span>
      </div>
      <table className="aef-table">
        <thead>
          <tr>
            <th>Timestamp</th>
            <th>Nodes</th>
            <th>Alerts</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {availableSnapshots.map((snap) => {
            const isSelected = selectedSnapshot?.id === snap.id;
            const bookmarked = isBookmarked(snap.id);
            return (
              <tr
                key={snap.id}
                onClick={() => loadSnapshot(snap.id)}
                style={{
                  cursor: 'pointer',
                  background: isSelected ? 'var(--aef-surface-hover)' : undefined,
                }}
                data-testid={`snapshot-row-${snap.id}`}
              >
                <td>{new Date(snap.timestamp).toLocaleString()}</td>
                <td>{snap.nodes.length}</td>
                <td>{snap.alertCount}</td>
                <td>
                  <button
                    className="aef-btn-icon"
                    onClick={(e) => {
                      e.stopPropagation();
                      handleBookmarkToggle(snap.id);
                    }}
                    aria-label={bookmarked ? 'Remove bookmark' : 'Add bookmark'}
                    data-testid={`snapshot-bookmark-${snap.id}`}
                  >
                    {bookmarked ? <BookmarkCheck size={14} /> : <Bookmark size={14} />}
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
});
