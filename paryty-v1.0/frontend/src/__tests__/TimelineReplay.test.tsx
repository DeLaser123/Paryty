import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { TimelineScrubber } from '../components/timeline/TimelineScrubber';
import { SpeedControls } from '../components/timeline/SpeedControls';
import { SeekInput } from '../components/timeline/SeekInput';
import { DiffViewer } from '../components/timeline/DiffViewer';
import { useTimelineStore } from '../stores/timelineStore';

// Mock the timeline store
vi.mock('../stores/timelineStore', () => ({
  useTimelineStore: vi.fn(),
}));

describe('TimelineReplay', () => {
  const mockPlay = vi.fn();
  const mockPause = vi.fn();
  const mockStop = vi.fn();
  const mockSeekTo = vi.fn();
  const mockSetSpeed = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();

    // Default mock implementation
    (useTimelineStore as any).mockImplementation((selector: any) => {
      const state = {
        position: {
          progress: 0.5,
          currentTime: '2024-01-01T12:00:00Z',
          speed: 1,
          state: 'paused',
        },
        config: {
          startTime: '2024-01-01T00:00:00Z',
          endTime: '2024-01-02T00:00:00Z',
        },
        currentTime: new Date('2024-01-01T12:00:00Z').getTime(),
        startTime: new Date('2024-01-01T00:00:00Z').getTime(),
        endTime: new Date('2024-01-02T00:00:00Z').getTime(),
        snapshots: [
          { id: '1', timestamp: '2024-01-01T06:00:00Z' },
          { id: '2', timestamp: '2024-01-01T12:00:00Z' },
        ],
        play: mockPlay,
        pause: mockPause,
        stop: mockStop,
        seekTo: mockSeekTo,
        setSpeed: mockSetSpeed,
      };
      return selector(state);
    });
  });

  describe('TimelineScrubber', () => {
    it('renders play button when paused', () => {
      render(<TimelineScrubber />);
      const playButton = screen.getByTestId('timeline-play-pause');
      expect(playButton).toBeInTheDocument();
      expect(playButton).toHaveAttribute('aria-label', 'Play');
    });

    it('renders pause button when playing', () => {
      (useTimelineStore as any).mockImplementation((selector: any) => {
        const state = {
          position: {
            progress: 0.5,
            currentTime: '2024-01-01T12:00:00Z',
            speed: 1,
            state: 'playing',
          },
          config: {
            startTime: '2024-01-01T00:00:00Z',
            endTime: '2024-01-02T00:00:00Z',
          },
          snapshots: [],
          play: mockPlay,
          pause: mockPause,
          stop: mockStop,
          seekTo: mockSeekTo,
        };
        return selector(state);
      });

      render(<TimelineScrubber />);
      const pauseButton = screen.getByTestId('timeline-play-pause');
      expect(pauseButton).toHaveAttribute('aria-label', 'Pause');
    });

    it('calls play when play button is clicked', () => {
      render(<TimelineScrubber />);
      fireEvent.click(screen.getByTestId('timeline-play-pause'));
      expect(mockPlay).toHaveBeenCalled();
    });

    it('calls pause when pause button is clicked while playing', () => {
      (useTimelineStore as any).mockImplementation((selector: any) => {
        const state = {
          position: {
            progress: 0.5,
            currentTime: '2024-01-01T12:00:00Z',
            speed: 1,
            state: 'playing',
          },
          config: {
            startTime: '2024-01-01T00:00:00Z',
            endTime: '2024-01-02T00:00:00Z',
          },
          snapshots: [],
          play: mockPlay,
          pause: mockPause,
          stop: mockStop,
          seekTo: mockSeekTo,
        };
        return selector(state);
      });

      render(<TimelineScrubber />);
      fireEvent.click(screen.getByTestId('timeline-play-pause'));
      expect(mockPause).toHaveBeenCalled();
    });

    it('calls stop when stop button is clicked', () => {
      render(<TimelineScrubber />);
      fireEvent.click(screen.getByTestId('timeline-stop'));
      expect(mockStop).toHaveBeenCalled();
    });

    it('calls seekTo when track is clicked', () => {
      render(<TimelineScrubber />);
      const track = screen.getByTestId('timeline-track');
      
      // Mock getBoundingClientRect
      track.getBoundingClientRect = vi.fn(() => ({
        left: 0,
        width: 100,
        top: 0,
        right: 100,
        bottom: 10,
        height: 10,
        x: 0,
        y: 0,
        toJSON: () => {},
      }));

      fireEvent.click(track, { clientX: 50 });
      expect(mockSeekTo).toHaveBeenCalledWith(0.5);
    });

    it('displays current time and speed', () => {
      render(<TimelineScrubber />);
      expect(screen.getByText('1x')).toBeInTheDocument();
    });
  });

  describe('SpeedControls', () => {
    it('renders all speed options', () => {
      render(<SpeedControls />);
      const speeds = [0.25, 0.5, 1, 2, 4, 8, 16];
      speeds.forEach((speed) => {
        expect(screen.getByTestId(`speed-${speed}`)).toBeInTheDocument();
      });
    });

    it('highlights current speed', () => {
      render(<SpeedControls />);
      const speed1Button = screen.getByTestId('speed-1');
      expect(speed1Button).toHaveClass('aef-btn-active');
    });

    it('calls setSpeed when speed button is clicked', () => {
      render(<SpeedControls />);
      fireEvent.click(screen.getByTestId('speed-2'));
      expect(mockSetSpeed).toHaveBeenCalledWith(2);
    });
  });

  describe('SeekInput', () => {
    it('renders input field', () => {
      render(<SeekInput />);
      expect(screen.getByTestId('seek-input-field')).toBeInTheDocument();
    });

    it('calls seekTo when valid ISO timestamp is entered', async () => {
      render(<SeekInput />);
      const input = screen.getByTestId('seek-input-field');
      
      fireEvent.change(input, { target: { value: '2024-01-01T12:00:00Z' } });
      fireEvent.click(screen.getByTestId('seek-input-button'));

      await waitFor(() => {
        expect(mockSeekTo).toHaveBeenCalled();
      });
    });

    it('shows error for invalid timestamp', async () => {
      render(<SeekInput />);
      const input = screen.getByTestId('seek-input-field');
      
      fireEvent.change(input, { target: { value: 'invalid' } });
      fireEvent.click(screen.getByTestId('seek-input-button'));

      await waitFor(() => {
        expect(screen.getByTestId('seek-input-error')).toBeInTheDocument();
      });
    });

    it('handles relative time input', async () => {
      render(<SeekInput />);
      const input = screen.getByTestId('seek-input-field');
      
      fireEvent.change(input, { target: { value: '-5m' } });
      fireEvent.click(screen.getByTestId('seek-input-button'));

      await waitFor(() => {
        expect(mockSeekTo).toHaveBeenCalled();
      });
    });
  });

  describe('DiffViewer', () => {
    const mockFrom = {
      id: '1',
      timestamp: '2024-01-01T00:00:00Z',
      nodes: [
        { id: 'node1', name: 'Node 1', type: 'host' as const, status: 'healthy' as const, labels: {}, metadata: {}, lastSeen: '2024-01-01T00:00:00Z' },
        { id: 'node2', name: 'Node 2', type: 'host' as const, status: 'healthy' as const, labels: {}, metadata: {}, lastSeen: '2024-01-01T00:00:00Z' },
      ],
      edges: [{ id: 'edge1', sourceId: 'node1', targetId: 'node2', type: 'network' as const, metadata: {} }],
      metrics: { cpu: 50, memory: 60 },
      alertCount: 0,
      eventCount: 10,
    };

    const mockTo = {
      id: '2',
      timestamp: '2024-01-01T01:00:00Z',
      nodes: [
        { id: 'node1', name: 'Node 1', type: 'host' as const, status: 'healthy' as const, labels: {}, metadata: {}, lastSeen: '2024-01-01T01:00:00Z' },
        { id: 'node3', name: 'Node 3', type: 'host' as const, status: 'healthy' as const, labels: {}, metadata: {}, lastSeen: '2024-01-01T01:00:00Z' },
      ],
      edges: [{ id: 'edge1', sourceId: 'node1', targetId: 'node3', type: 'network' as const, metadata: {} }],
      metrics: { cpu: 70, memory: 80 },
      alertCount: 1,
      eventCount: 20,
    };

    it('renders diff summary', () => {
      render(<DiffViewer from={mockFrom} to={mockTo} />);
      expect(screen.getByText('+1 added')).toBeInTheDocument();
      expect(screen.getByText('-1 removed')).toBeInTheDocument();
    });

    it('renders from and to panels', () => {
      render(<DiffViewer from={mockFrom} to={mockTo} />);
      expect(screen.getByText('From')).toBeInTheDocument();
      expect(screen.getByText('To')).toBeInTheDocument();
    });

    it('displays node counts', () => {
      render(<DiffViewer from={mockFrom} to={mockTo} />);
      expect(screen.getByText('2')).toBeInTheDocument(); // From nodes
      expect(screen.getByText('2')).toBeInTheDocument(); // To nodes
    });
  });
});
