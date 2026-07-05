import type React from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

export interface ScrubberItem {
  nodeId: string;
  label: string;
  status: string;
  isSubnode: boolean;
}

interface TimelineScrubberProps {
  items: ScrubberItem[];
  currentIndex: number;
  onIndexChange: (index: number) => void;
}

const isItemFailed = (item: ScrubberItem): boolean => {
  return item.status === 'failed' || item.status === 'cancelled';
};

const TimelineScrubber: React.FC<TimelineScrubberProps> = ({ items, currentIndex, onIndexChange }) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [pauseOnError, setPauseOnError] = useState(false);
  const intervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const maxIndex = items.length - 1;
  const disabled = items.length === 0;

  const stopPlayback = useCallback(() => {
    if (intervalRef.current !== null) {
      clearInterval(intervalRef.current);
      intervalRef.current = null;
    }
    setIsPlaying(false);
  }, []);

  const startPlayback = useCallback(() => {
    if (disabled) return;
    if (currentIndex >= maxIndex) {
      onIndexChange(0);
    }
    setIsPlaying(true);
  }, [disabled, currentIndex, maxIndex, onIndexChange]);

  // Handle the interval for auto-play
  useEffect(() => {
    if (!isPlaying) {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    intervalRef.current = setInterval(() => {
      onIndexChange(currentIndex + 1);
    }, 1000);

    return () => {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [isPlaying, currentIndex, onIndexChange]);

  // Stop when reaching the end
  useEffect(() => {
    if (isPlaying && currentIndex >= maxIndex) {
      stopPlayback();
    }
  }, [isPlaying, currentIndex, maxIndex, stopPlayback]);

  // Pause on error check
  useEffect(() => {
    if (!isPlaying || !pauseOnError) return;
    const nextIndex = currentIndex + 1;
    if (nextIndex <= maxIndex) {
      const nextItem = items[nextIndex];
      if (nextItem && isItemFailed(nextItem)) {
        onIndexChange(nextIndex);
        stopPlayback();
      }
    }
  }, [isPlaying, pauseOnError, currentIndex, maxIndex, items, onIndexChange, stopPlayback]);

  // Clean up on unmount
  useEffect(() => {
    return () => {
      if (intervalRef.current !== null) {
        clearInterval(intervalRef.current);
      }
    };
  }, []);

  const handleTogglePlay = () => {
    if (isPlaying) {
      stopPlayback();
    } else {
      startPlayback();
    }
  };

  const handleSliderChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const value = Number.parseInt(e.target.value, 10);
    onIndexChange(value);
  };

  const handleGoToStart = () => onIndexChange(0);
  const handleGoToEnd = () => onIndexChange(maxIndex);
  const handlePrev = () => {
    if (currentIndex > 0) onIndexChange(currentIndex - 1);
  };
  const handleNext = () => {
    if (currentIndex < maxIndex) onIndexChange(currentIndex + 1);
  };

  // Build error markers
  const errorMarkers: number[] = [];
  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    if (item && isItemFailed(item)) {
      errorMarkers.push(i);
    }
  }

  // Current item info
  const currentItem = disabled ? null : items[currentIndex];
  const currentLabel = currentItem
    ? currentItem.label.length > 30
      ? `${currentItem.label.substring(0, 30)}...`
      : currentItem.label
    : '';

  const navButtonStyle: React.CSSProperties = {
    padding: '2px 6px',
    fontSize: '11px',
    fontWeight: 600,
    backgroundColor: '#fff',
    color: disabled ? '#bbb' : '#333',
    border: '1px solid #ccc',
    borderRadius: '3px',
    cursor: disabled ? 'default' : 'pointer',
    lineHeight: '16px',
    minWidth: '24px',
    textAlign: 'center',
  };

  return (
    <div
      style={{
        backgroundColor: 'white',
        borderRadius: '8px',
        border: '1px solid #e0e0e0',
        padding: '10px 12px',
        marginBottom: '12px',
      }}
    >
      {/* Top row: navigation buttons + slider */}
      <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
        <button
          type="button"
          onClick={handleGoToStart}
          disabled={disabled}
          style={navButtonStyle}
          title="Go to first node"
        >
          |&lt;
        </button>
        <button
          type="button"
          onClick={handlePrev}
          disabled={disabled || currentIndex <= 0}
          style={{
            ...navButtonStyle,
            color: disabled || currentIndex <= 0 ? '#bbb' : '#333',
            cursor: disabled || currentIndex <= 0 ? 'default' : 'pointer',
          }}
          title="Previous node"
        >
          &lt;
        </button>

        {/* Slider area */}
        <div style={{ flex: 1, position: 'relative', minWidth: 0 }}>
          <input
            type="range"
            min={0}
            max={maxIndex >= 0 ? maxIndex : 0}
            value={disabled ? 0 : currentIndex}
            onChange={handleSliderChange}
            disabled={disabled}
            style={{
              width: '100%',
              height: '18px',
              cursor: disabled ? 'default' : 'pointer',
              accentColor: '#1976d2',
              margin: 0,
            }}
          />
          {items.length > 1 && errorMarkers.length > 0 && (
            <div
              style={{ position: 'relative', height: '6px', marginTop: '1px', marginLeft: '6px', marginRight: '6px' }}
            >
              {errorMarkers.map((idx) => {
                const percent = maxIndex > 0 ? (idx / maxIndex) * 100 : 50;
                return (
                  <div
                    key={`err-${idx}`}
                    style={{
                      position: 'absolute',
                      left: `${percent}%`,
                      top: 0,
                      width: '6px',
                      height: '6px',
                      borderRadius: '50%',
                      backgroundColor: '#c62828',
                      transform: 'translateX(-50%)',
                    }}
                    title={`Node ${idx + 1} has errors`}
                  />
                );
              })}
            </div>
          )}
        </div>

        <button
          type="button"
          onClick={handleNext}
          disabled={disabled || currentIndex >= maxIndex}
          style={{
            ...navButtonStyle,
            color: disabled || currentIndex >= maxIndex ? '#bbb' : '#333',
            cursor: disabled || currentIndex >= maxIndex ? 'default' : 'pointer',
          }}
          title="Next node"
        >
          &gt;
        </button>
        <button
          type="button"
          onClick={handleGoToEnd}
          disabled={disabled}
          style={navButtonStyle}
          title="Go to last node"
        >
          &gt;|
        </button>
      </div>

      {/* Bottom row: counter, label, play controls */}
      <div
        style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '6px', gap: '6px' }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '4px', minWidth: 0, flex: 1 }}>
          <span style={{ fontSize: '10px', color: '#555', whiteSpace: 'nowrap' }}>
            {disabled ? 'No nodes' : `${currentIndex + 1}/${items.length}`}
          </span>
          {currentItem && (
            <>
              {currentItem.isSubnode && (
                <span
                  style={{
                    fontSize: '8px',
                    padding: '1px 4px',
                    backgroundColor: '#e8eaf6',
                    color: '#3949ab',
                    borderRadius: '3px',
                    fontWeight: 600,
                    whiteSpace: 'nowrap',
                  }}
                >
                  sub
                </span>
              )}
              <span
                style={{
                  fontSize: '10px',
                  color: '#333',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                  minWidth: 0,
                }}
                title={currentItem.label}
              >
                {currentLabel}
              </span>
            </>
          )}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexShrink: 0 }}>
          <button
            type="button"
            onClick={handleTogglePlay}
            disabled={disabled}
            style={{
              padding: '2px 8px',
              fontSize: '10px',
              fontWeight: 600,
              backgroundColor: disabled ? '#e0e0e0' : isPlaying ? '#e65100' : '#1976d2',
              color: disabled ? '#999' : 'white',
              border: 'none',
              borderRadius: '3px',
              cursor: disabled ? 'default' : 'pointer',
              whiteSpace: 'nowrap',
            }}
            title={isPlaying ? 'Pause playback' : 'Start playback'}
          >
            {isPlaying ? 'Pause' : 'Play'}
          </button>
          <label
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '3px',
              fontSize: '10px',
              color: disabled ? '#bbb' : '#666',
              cursor: disabled ? 'default' : 'pointer',
              whiteSpace: 'nowrap',
            }}
          >
            <input
              type="checkbox"
              checked={pauseOnError}
              onChange={(e) => setPauseOnError(e.target.checked)}
              disabled={disabled}
              style={{ margin: 0, width: '12px', height: '12px' }}
            />
            Pause on error
          </label>
        </div>
      </div>
    </div>
  );
};

export default TimelineScrubber;
