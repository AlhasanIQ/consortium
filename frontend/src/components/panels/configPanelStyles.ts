/**
 * Shared style constants for ConfigPanel to reduce inline style duplication.
 */
import type React from 'react';

/** Standard text input / number input style. */
export const INPUT_STYLE: React.CSSProperties = {
  width: '100%',
  padding: '8px',
  border: '1px solid #ddd',
  borderRadius: '4px',
  fontSize: '14px',
};

/** Textarea base style (monospace, resizable). Spread and override minHeight per use. */
export const TEXTAREA_STYLE: React.CSSProperties = {
  width: '100%',
  padding: '8px',
  border: '1px solid #ddd',
  borderRadius: '4px',
  fontSize: '13px',
  fontFamily: 'monospace',
  resize: 'vertical' as const,
};

/** Primary field label (e.g. "Model", "Temperature"). */
export const LABEL_STYLE: React.CSSProperties = {
  display: 'block' as const,
  marginBottom: '5px',
  fontSize: '14px',
  fontWeight: '500' as const,
};

/** Aggregation / sub-section field label (slightly smaller). */
export const SUB_LABEL_STYLE: React.CSSProperties = {
  display: 'block' as const,
  marginBottom: '5px',
  fontSize: '13px',
  fontWeight: '500' as const,
};

/** Wrapper for aggregation config sections. */
export const SECTION_STYLE: React.CSSProperties = {
  marginBottom: '15px',
  padding: '10px',
  backgroundColor: '#f9f9f9',
  borderRadius: '4px',
};

/** Small helper / description text below a label. */
export const HELP_TEXT_STYLE: React.CSSProperties = {
  fontSize: '11px',
  color: '#666',
  marginBottom: '6px',
};

/** <select> element style. */
export const SELECT_STYLE: React.CSSProperties = {
  width: '100%',
  padding: '8px',
  border: '1px solid #ddd',
  borderRadius: '4px',
  fontSize: '14px',
  backgroundColor: 'white',
};

/** Retry-policy sub-label (smaller, muted). */
export const RETRY_LABEL_STYLE: React.CSSProperties = {
  display: 'block' as const,
  marginBottom: '3px',
  fontSize: '12px',
  color: '#666',
};

/** Retry-policy input (smaller padding). */
export const RETRY_INPUT_STYLE: React.CSSProperties = {
  width: '100%',
  padding: '6px',
  border: '1px solid #ddd',
  borderRadius: '4px',
  fontSize: '13px',
};
