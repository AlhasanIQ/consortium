import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import type { Model } from '../../api/modelsClient';

interface ModelSelectorProps {
  models: Model[];
  value: string;
  onChange: (modelId: string) => void;
  disabled?: boolean;
  placeholder?: string;
  loading?: boolean;
}

// cost is per-token USD from the API; display as per-million-tokens
function formatCost(costPerToken: number | undefined): string {
  if (costPerToken == null) return '—';
  if (costPerToken === 0) return 'Free';
  const perMillion = costPerToken * 1_000_000;
  if (perMillion < 0.01) return `$${perMillion.toFixed(4)}`;
  return `$${perMillion.toFixed(2)}`;
}

function formatContext(contextLen: number): string {
  if (contextLen >= 1_000_000) return `${(contextLen / 1_000_000).toFixed(1)}M ctx`;
  if (contextLen >= 1000) return `${Math.round(contextLen / 1000)}K ctx`;
  return `${contextLen} ctx`;
}

const ModelSelector: React.FC<ModelSelectorProps> = ({
  models,
  value,
  onChange,
  disabled = false,
  placeholder = 'Search models...',
  loading = false,
}) => {
  const [search, setSearch] = useState('');
  const [isOpen, setIsOpen] = useState(false);
  const [highlightIndex, setHighlightIndex] = useState(0);
  const containerRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const selectedModel = models.find((m) => m.id === value);

  const filtered = search
    ? models.filter((m) => {
        const q = search.toLowerCase();
        return m.id.toLowerCase().includes(q) || m.name.toLowerCase().includes(q);
      })
    : models;

  // Sort: available first, then alphabetically by ID
  const sorted = [...filtered].sort((a, b) => {
    if (a.available !== b.available) return a.available ? -1 : 1;
    return a.id.localeCompare(b.id);
  });

  // biome-ignore lint/correctness/useExhaustiveDependencies: reset highlight when filtered list changes
  useEffect(() => {
    setHighlightIndex(0);
  }, [sorted.length]);

  // Close on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  // Scroll highlighted item into view
  useEffect(() => {
    if (isOpen && listRef.current) {
      const item = listRef.current.children[highlightIndex] as HTMLElement;
      if (item) item.scrollIntoView({ block: 'nearest' });
    }
  }, [highlightIndex, isOpen]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (!isOpen) {
      if (e.key === 'ArrowDown' || e.key === 'Enter') {
        setIsOpen(true);
        e.preventDefault();
      }
      return;
    }

    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        setHighlightIndex((i) => Math.min(i + 1, sorted.length - 1));
        break;
      case 'ArrowUp':
        e.preventDefault();
        setHighlightIndex((i) => Math.max(i - 1, 0));
        break;
      case 'Enter':
        e.preventDefault();
        if (sorted[highlightIndex]) {
          onChange(sorted[highlightIndex].id);
          setSearch('');
          setIsOpen(false);
        }
        break;
      case 'Escape':
        setIsOpen(false);
        setSearch('');
        break;
    }
  };

  const handleSelect = (modelId: string) => {
    onChange(modelId);
    setSearch('');
    setIsOpen(false);
  };

  const displayValue = isOpen ? search : selectedModel ? selectedModel.id : '';

  return (
    <div ref={containerRef} style={{ position: 'relative' }}>
      <input
        type="text"
        value={displayValue}
        onChange={(e) => {
          setSearch(e.target.value);
          if (!isOpen) setIsOpen(true);
        }}
        onFocus={() => {
          setIsOpen(true);
          setSearch('');
        }}
        onKeyDown={handleKeyDown}
        disabled={disabled || loading}
        placeholder={loading ? 'Loading models...' : placeholder}
        style={{
          width: '100%',
          padding: '8px',
          border: '1px solid #ddd',
          borderRadius: '4px',
          fontSize: '13px',
          backgroundColor: disabled || loading ? '#f0f0f0' : 'white',
          boxSizing: 'border-box',
        }}
      />
      {value && !isOpen && (
        <button
          type="button"
          onClick={() => {
            onChange('');
            setSearch('');
          }}
          style={{
            position: 'absolute',
            right: '8px',
            top: '50%',
            transform: 'translateY(-50%)',
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            color: '#999',
            fontSize: '14px',
            padding: '0 4px',
          }}
          title="Clear selection"
        >
          x
        </button>
      )}
      {isOpen && sorted.length > 0 && (
        <div
          ref={listRef}
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            maxHeight: '250px',
            overflowY: 'auto',
            border: '1px solid #ddd',
            borderTop: 'none',
            borderRadius: '0 0 4px 4px',
            backgroundColor: 'white',
            zIndex: 100,
            boxShadow: '0 4px 8px rgba(0,0,0,0.1)',
          }}
        >
          {sorted.map((model, i) => (
            <div
              key={model.id}
              role="option"
              aria-selected={value === model.id}
              tabIndex={-1}
              onClick={() => handleSelect(model.id)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') handleSelect(model.id);
              }}
              onMouseEnter={() => setHighlightIndex(i)}
              style={{
                padding: '6px 8px',
                cursor: 'pointer',
                backgroundColor: i === highlightIndex ? '#e3f2fd' : value === model.id ? '#f5f5f5' : 'white',
                borderBottom: '1px solid #f0f0f0',
                opacity: model.available ? 1 : 0.5,
              }}
            >
              <div style={{ fontSize: '13px', fontWeight: 500 }}>{model.id}</div>
              <div style={{ fontSize: '11px', color: '#666', display: 'flex', gap: '8px' }}>
                <span>{formatCost(model.input_cost_per_token)}/M in</span>
                <span>{formatCost(model.output_cost_per_token)}/M out</span>
                {model.context_length > 0 && <span>{formatContext(model.context_length)}</span>}
              </div>
            </div>
          ))}
        </div>
      )}
      {isOpen && sorted.length === 0 && search && (
        <div
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            padding: '8px',
            border: '1px solid #ddd',
            borderTop: 'none',
            borderRadius: '0 0 4px 4px',
            backgroundColor: 'white',
            zIndex: 100,
            color: '#999',
            fontSize: '13px',
          }}
        >
          No models matching "{search}"
        </div>
      )}
    </div>
  );
};

export default ModelSelector;
