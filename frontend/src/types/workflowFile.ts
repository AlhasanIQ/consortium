/**
 * Complete workflow file format for export/import
 * Preserves ALL data including UI state, node positions, and configurations
 */

import type { Edge, Node } from '@xyflow/react';
import type { NodeData } from './workflow';

export interface WorkflowFileFormat {
  // Metadata
  version: string;
  id?: string; // Optional - backend will assign UUID if undefined
  name: string;
  description?: string;
  created_at: string;
  updated_at: string;

  // Complete React Flow state
  nodes: Node<NodeData>[];
  edges: Edge[];

  // Optional backend-compatible format
  metadata?: {
    author?: string;
    tags?: string[];
    [key: string]: unknown;
  };
}

/**
 * Current file format version
 */
export const WORKFLOW_FILE_VERSION = '1.0.0';
