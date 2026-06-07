/**
 * SearchOverlay — Command palette-style search overlay.
 *
 * Triggered by Ctrl+K. Provides fuzzy matching over node names,
 * keyboard navigation, and Enter to select.
 * Uses the command palette pattern from the design system.
 *
 * @module components/topology/SearchOverlay
 */

import { useState, useCallback, useRef, useEffect, useMemo } from 'react';
import { Search, Server, Box, Cpu, GitBranch } from 'lucide-react';
import clsx from 'clsx';
import type { TopologyNode } from '../../types/topology';
import { useTopologyStore } from '../../stores/topologyStore';

/** Props for SearchOverlay. */
interface SearchOverlayProps {
  /** Callback to close the search overlay. */
  onClose: () => void;
}

/** Map node type to icon. */
function getNodeIcon(type: TopologyNode['type']): React.ReactNode {
  switch (type) {
    case 'host':      return <Server size={14} />;
    case 'container': return <Box size={14} />;
    case 'service':   return <GitBranch size={14} />;
    case 'process':   return <Cpu size={14} />;
    default:          return <Server size={14} />;
  }
}

/**
 * Full-screen search overlay using the command palette pattern.
 *
 * Features:
 * - Fuzzy matching over node names
 * - Keyboard navigation (up/down arrows, Enter)
 * - Closes on Escape or click outside
 */
export function SearchOverlay({ onClose }: SearchOverlayProps) {
  const [query, setQuery] = useState('');
  const [focusIndex, setFocusIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const topology = useTopologyStore((s) => s.topology);
  const selectNode = useTopologyStore((s) => s.selectNode);
  const zoomToNode = useTopologyStore((s) => s.zoomToNode);

  // Focus input on mount
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Filter nodes by query
  const results = useMemo(() => {
    if (!topology || !query.trim()) return topology?.nodes.slice(0, 20) ?? [];
    const q = query.toLowerCase();
    return topology.nodes
      .filter(
        (n) =>
          n.name.toLowerCase().includes(q) ||
          n.id.toLowerCase().includes(q) ||
          n.type.toLowerCase().includes(q),
      )
      .slice(0, 20);
  }, [topology, query]);

  // Reset focus when results change
  useEffect(() => {
    setFocusIndex(0);
  }, [results.length]);

  const handleSelect = useCallback(
    (node: TopologyNode) => {
      selectNode(node);
      zoomToNode(node.id);
      onClose();
    },
    [selectNode, zoomToNode, onClose],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      switch (e.key) {
        case 'Escape':
          onClose();
          break;
        case 'ArrowDown':
          e.preventDefault();
          setFocusIndex((i) => Math.min(i + 1, results.length - 1));
          break;
        case 'ArrowUp':
          e.preventDefault();
          setFocusIndex((i) => Math.max(i - 1, 0));
          break;
        case 'Enter':
          e.preventDefault();
          if (results[focusIndex]) {
            handleSelect(results[focusIndex]);
          }
          break;
      }
    },
    [results, focusIndex, onClose, handleSelect],
  );

  return (
    <div
      className="aef-cmd-overlay"
      onClick={onClose}
      onKeyDown={handleKeyDown}
      data-testid="search-overlay"
    >
      <div
        className="aef-cmd-palette"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label="Search nodes"
      >
        {/* Search input */}
        <div className="aef-cmd-search">
          <Search size={16} />
          <input
            ref={inputRef}
            className="aef-cmd-input"
            type="text"
            placeholder="Search nodes…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            data-testid="search-input"
          />
        </div>

        {/* Results */}
        <div className="aef-cmd-results aef-scroll" role="listbox">
          {results.length === 0 && (
            <div className="aef-cmd-empty" data-testid="search-empty">
              No nodes found
            </div>
          )}
          {results.map((node, i) => (
            <div
              key={node.id}
              className={clsx(
                'aef-cmd-item',
                i === focusIndex && 'aef-cmd-item--focused',
              )}
              role="option"
              aria-selected={i === focusIndex}
              onClick={() => handleSelect(node)}
              data-testid={`search-result-${node.id}`}
            >
              {getNodeIcon(node.type)}
              <span className="aef-cmd-label">{node.name}</span>
              <span className="aef-cmd-kbd">{node.type}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
