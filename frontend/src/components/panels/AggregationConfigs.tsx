import type React from 'react';
import type { Model } from '../../api/modelsClient';
import type { AggregationConfig } from '../../types/workflow';
import {
  type CollectConfig,
  DEFAULT_AGGREGATION_MAX_TOKENS,
  DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS,
  DEFAULT_MODEL,
  type DebateDecideConfig,
  getAggConfig,
  type JudgeConfig,
  type MajorityVoteConfig,
  mergeAggConfig,
  type PeerMatrixConfig,
  type ScoringConfig,
  type SynthesisConfig,
} from '../../types/workflow';
import ModelSelector from '../common/ModelSelector';
import { INPUT_STYLE, SECTION_STYLE, SELECT_STYLE, SUB_LABEL_STYLE, TEXTAREA_STYLE } from './configPanelStyles';

interface AggregationConfigProps {
  method: string;
  aggregationConfig: AggregationConfig | undefined;
  models: Model[];
  loadingModels: boolean;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}

const maxTokensFieldStyle = {
  ...INPUT_STYLE,
  fontSize: '13px',
  marginBottom: '10px',
};

const repairMaxTokensFieldStyle = {
  ...INPUT_STYLE,
  fontSize: '13px',
};

const parseIntegerOrDefault = (raw: string, fallback: number): number => {
  const parsed = Number.parseInt(raw, 10);
  if (Number.isNaN(parsed)) {
    return fallback;
  }
  return parsed;
};

const CollectSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-collect-separator" style={SUB_LABEL_STYLE}>
      Separator
    </label>
    <input
      id="config-collect-separator"
      type="text"
      value={getAggConfig<CollectConfig>(aggregationConfig).separator || ''}
      onChange={(e) => onChange(mergeAggConfig<CollectConfig>(aggregationConfig, { separator: e.target.value }))}
      disabled={disabled}
      style={{
        ...INPUT_STYLE,
        fontSize: '13px',
        fontFamily: 'monospace',
      }}
      placeholder="\n---\n (default)"
    />
  </div>
);

const SynthesisSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  models: Model[];
  loadingModels: boolean;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, models, loadingModels, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-synthesis-model" style={SUB_LABEL_STYLE}>
      Synthesis Model
    </label>
    <ModelSelector
      models={models}
      value={getAggConfig<SynthesisConfig>(aggregationConfig).model || ''}
      onChange={(v) => onChange(mergeAggConfig<SynthesisConfig>(aggregationConfig, { model: v }))}
      disabled={loadingModels || disabled}
      loading={loadingModels}
      placeholder={`Default (${DEFAULT_MODEL})`}
    />

    <label htmlFor="config-synthesis-prompt" style={SUB_LABEL_STYLE}>
      Custom Prompt (optional)
    </label>
    <textarea
      id="config-synthesis-prompt"
      value={getAggConfig<SynthesisConfig>(aggregationConfig).prompt || ''}
      onChange={(e) => onChange(mergeAggConfig<SynthesisConfig>(aggregationConfig, { prompt: e.target.value }))}
      disabled={disabled}
      style={{
        ...TEXTAREA_STYLE,
        fontSize: '12px',
        minHeight: '80px',
      }}
      placeholder="Use {{prompt}} for original question, {{responses}} for all responses"
    />

    <label htmlFor="config-synthesis-max-tokens" style={SUB_LABEL_STYLE}>
      Max Tokens
    </label>
    <input
      id="config-synthesis-max-tokens"
      type="number"
      min={-1}
      step={1}
      value={getAggConfig<SynthesisConfig>(aggregationConfig).max_tokens ?? DEFAULT_AGGREGATION_MAX_TOKENS}
      onChange={(e) =>
        onChange(
          mergeAggConfig<SynthesisConfig>(aggregationConfig, {
            max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={maxTokensFieldStyle}
    />
    <div style={{ fontSize: '11px', color: '#666' }}>Use -1 for unlimited.</div>
  </div>
);

const JudgeSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  models: Model[];
  loadingModels: boolean;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, models, loadingModels, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-judge-model" style={SUB_LABEL_STYLE}>
      Judge Model
    </label>
    <ModelSelector
      models={models}
      value={getAggConfig<JudgeConfig>(aggregationConfig).judge_model || ''}
      onChange={(v) => onChange(mergeAggConfig<JudgeConfig>(aggregationConfig, { judge_model: v }))}
      disabled={loadingModels || disabled}
      loading={loadingModels}
      placeholder={`Default (${DEFAULT_MODEL})`}
    />

    <label htmlFor="config-judge-max-tokens" style={SUB_LABEL_STYLE}>
      Max Tokens
    </label>
    <input
      id="config-judge-max-tokens"
      type="number"
      min={-1}
      step={1}
      value={getAggConfig<JudgeConfig>(aggregationConfig).max_tokens ?? DEFAULT_AGGREGATION_MAX_TOKENS}
      onChange={(e) =>
        onChange(
          mergeAggConfig<JudgeConfig>(aggregationConfig, {
            max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={maxTokensFieldStyle}
    />

    <label htmlFor="config-judge-repair-max-tokens" style={SUB_LABEL_STYLE}>
      Repair Max Tokens
    </label>
    <input
      id="config-judge-repair-max-tokens"
      type="number"
      min={1}
      step={1}
      value={getAggConfig<JudgeConfig>(aggregationConfig).repair_max_tokens ?? DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS}
      onChange={(e) =>
        onChange(
          mergeAggConfig<JudgeConfig>(aggregationConfig, {
            repair_max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={repairMaxTokensFieldStyle}
    />
  </div>
);

const ScoringSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  models: Model[];
  loadingModels: boolean;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, models, loadingModels, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-scoring-model" style={SUB_LABEL_STYLE}>
      Scoring Model
    </label>
    <ModelSelector
      models={models}
      value={getAggConfig<ScoringConfig>(aggregationConfig).scoring_model || ''}
      onChange={(v) => onChange(mergeAggConfig<ScoringConfig>(aggregationConfig, { scoring_model: v }))}
      disabled={loadingModels || disabled}
      loading={loadingModels}
      placeholder={`Default (${DEFAULT_MODEL})`}
    />

    <label htmlFor="config-scoring-rubric-mode" style={SUB_LABEL_STYLE}>
      Rubric Mode
    </label>
    <select
      id="config-scoring-rubric-mode"
      value={getAggConfig<ScoringConfig>(aggregationConfig).rubric_mode || 'static'}
      onChange={(e) =>
        onChange(
          mergeAggConfig<ScoringConfig>(aggregationConfig, {
            rubric_mode: e.target.value as ScoringConfig['rubric_mode'],
          }),
        )
      }
      disabled={disabled}
      style={{ ...SELECT_STYLE, fontSize: '13px', marginBottom: '10px' }}
    >
      <option value="static">Static (fixed rubric)</option>
      <option value="dynamic">Dynamic (LLM-generated per task)</option>
    </select>

    <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
      {getAggConfig<ScoringConfig>(aggregationConfig).rubric_mode === 'dynamic'
        ? 'LLM generates task-specific criteria before scoring. Adds one extra LLM call.'
        : 'Default rubric: Logical Soundness (40%), Evidence Analysis (30%), Completeness (20%), Clarity (10%)'}
    </div>

    <label htmlFor="config-scoring-max-tokens" style={{ ...SUB_LABEL_STYLE, marginTop: '10px' }}>
      Max Tokens
    </label>
    <input
      id="config-scoring-max-tokens"
      type="number"
      min={-1}
      step={1}
      value={getAggConfig<ScoringConfig>(aggregationConfig).max_tokens ?? DEFAULT_AGGREGATION_MAX_TOKENS}
      onChange={(e) =>
        onChange(
          mergeAggConfig<ScoringConfig>(aggregationConfig, {
            max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={repairMaxTokensFieldStyle}
    />
  </div>
);

const PeerMatrixSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  models: Model[];
  loadingModels: boolean;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, models, loadingModels, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-peer-rubric-model" style={SUB_LABEL_STYLE}>
      Rubric Model
    </label>
    <ModelSelector
      models={models}
      value={getAggConfig<PeerMatrixConfig>(aggregationConfig).rubric_model || ''}
      onChange={(v) => onChange(mergeAggConfig<PeerMatrixConfig>(aggregationConfig, { rubric_model: v }))}
      disabled={loadingModels || disabled}
      loading={loadingModels}
      placeholder="Required for dynamic rubric mode"
    />
    <div style={{ fontSize: '11px', color: '#888', marginBottom: '8px' }}>
      Model for dynamic rubric generation. Required when rubric mode is &quot;dynamic&quot;.
    </div>

    <label htmlFor="config-peer-normalization" style={SUB_LABEL_STYLE}>
      Normalization
    </label>
    <select
      id="config-peer-normalization"
      value={getAggConfig<PeerMatrixConfig>(aggregationConfig).normalization || 'none'}
      onChange={(e) =>
        onChange(
          mergeAggConfig<PeerMatrixConfig>(aggregationConfig, {
            normalization: e.target.value as PeerMatrixConfig['normalization'],
          }),
        )
      }
      disabled={disabled}
      style={{
        ...SELECT_STYLE,
        fontSize: '13px',
        marginBottom: '10px',
      }}
    >
      <option value="none">None (Raw scores)</option>
    </select>

    <label htmlFor="config-peer-rubric-mode" style={SUB_LABEL_STYLE}>
      Rubric Mode
    </label>
    <select
      id="config-peer-rubric-mode"
      value={getAggConfig<PeerMatrixConfig>(aggregationConfig).rubric_mode || 'static'}
      onChange={(e) =>
        onChange(
          mergeAggConfig<PeerMatrixConfig>(aggregationConfig, {
            rubric_mode: e.target.value as PeerMatrixConfig['rubric_mode'],
          }),
        )
      }
      disabled={disabled}
      style={{ ...SELECT_STYLE, fontSize: '13px', marginBottom: '10px' }}
    >
      <option value="static">Static (fixed rubric)</option>
      <option value="dynamic">Dynamic (LLM-generated per task)</option>
    </select>

    <label htmlFor="config-peer-rubric" style={{ ...SUB_LABEL_STYLE, marginTop: '10px' }}>
      Scoring Rubric (JSON)
    </label>
    <textarea
      id="config-peer-rubric"
      value={
        getAggConfig<PeerMatrixConfig>(aggregationConfig).rubric
          ? JSON.stringify(getAggConfig<PeerMatrixConfig>(aggregationConfig).rubric, null, 2)
          : ''
      }
      onChange={(e) => {
        try {
          const parsed = JSON.parse(e.target.value);
          onChange(mergeAggConfig<PeerMatrixConfig>(aggregationConfig, { rubric: parsed }));
        } catch {
          // Invalid JSON - keep the text but don't update config
        }
      }}
      disabled={disabled}
      placeholder={`[
  {"name": "Logical Soundness", "weight": 0.4, "description": "..."},
  {"name": "Evidence Analysis", "weight": 0.3, "description": "..."}
]`}
      style={{
        ...TEXTAREA_STYLE,
        fontSize: '11px',
        minHeight: '80px',
      }}
    />

    <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
      Default rubric: Logical Soundness (40%), Evidence Analysis (30%), Completeness (20%), Clarity (10%).
      <br />
      Each agent scores all other agents. N agents = N×(N-1) evaluations.
    </div>

    <label htmlFor="config-peer-max-tokens" style={{ ...SUB_LABEL_STYLE, marginTop: '10px' }}>
      Max Tokens
    </label>
    <input
      id="config-peer-max-tokens"
      type="number"
      min={-1}
      step={1}
      value={getAggConfig<PeerMatrixConfig>(aggregationConfig).max_tokens ?? DEFAULT_AGGREGATION_MAX_TOKENS}
      onChange={(e) =>
        onChange(
          mergeAggConfig<PeerMatrixConfig>(aggregationConfig, {
            max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={repairMaxTokensFieldStyle}
    />
  </div>
);

const MajorityVoteSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-vote-strategy" style={SUB_LABEL_STYLE}>
      Extraction Strategy
    </label>
    <select
      id="config-vote-strategy"
      value={getAggConfig<MajorityVoteConfig>(aggregationConfig).extraction_strategy || 'regex'}
      onChange={(e) =>
        onChange(
          mergeAggConfig<MajorityVoteConfig>(aggregationConfig, {
            extraction_strategy: e.target.value as MajorityVoteConfig['extraction_strategy'],
          }),
        )
      }
      disabled={disabled}
      style={{
        ...SELECT_STYLE,
        fontSize: '13px',
        marginBottom: '10px',
      }}
    >
      <option value="regex">Regex (default MCQA pattern)</option>
      <option value="first_letter">First Uppercase Letter</option>
      <option value="json_field">JSON Field</option>
    </select>

    <label htmlFor="config-vote-tie" style={SUB_LABEL_STYLE}>
      Tie Breaker
    </label>
    <select
      id="config-vote-tie"
      value={
        getAggConfig<MajorityVoteConfig>(aggregationConfig).tie_breaker_method ||
        getAggConfig<MajorityVoteConfig>(aggregationConfig).tie_breaker ||
        'synthesis'
      }
      onChange={(e) =>
        onChange(
          mergeAggConfig<MajorityVoteConfig>(aggregationConfig, {
            tie_breaker_method: e.target.value as MajorityVoteConfig['tie_breaker_method'],
            tie_breaker: undefined,
          }),
        )
      }
      disabled={disabled}
      style={{
        ...SELECT_STYLE,
        fontSize: '13px',
      }}
    >
      <option value="synthesis">Synthesis (LLM fallback)</option>
      <option value="first">First Response</option>
      <option value="error">Error (strict)</option>
    </select>

    <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
      Extracts discrete answers from each agent and picks the majority winner.
      <br />
      Zero LLM cost unless tie-breaking triggers synthesis.
    </div>
  </div>
);

const DebateDecideSection: React.FC<{
  aggregationConfig: AggregationConfig | undefined;
  models: Model[];
  loadingModels: boolean;
  disabled?: boolean;
  onChange: (config: AggregationConfig) => void;
}> = ({ aggregationConfig, models, loadingModels, disabled = false, onChange }) => (
  <div style={SECTION_STYLE}>
    <label htmlFor="config-debate-model" style={SUB_LABEL_STYLE}>
      Judge Model
    </label>
    <ModelSelector
      models={models}
      value={getAggConfig<DebateDecideConfig>(aggregationConfig).judge_model || ''}
      onChange={(v) => onChange(mergeAggConfig<DebateDecideConfig>(aggregationConfig, { judge_model: v }))}
      disabled={loadingModels || disabled}
      loading={loadingModels}
      placeholder={`Default (${DEFAULT_MODEL})`}
    />

    <label htmlFor="config-debate-max-tokens" style={SUB_LABEL_STYLE}>
      Max Tokens
    </label>
    <input
      id="config-debate-max-tokens"
      type="number"
      min={-1}
      step={1}
      value={getAggConfig<DebateDecideConfig>(aggregationConfig).max_tokens ?? DEFAULT_AGGREGATION_MAX_TOKENS}
      onChange={(e) =>
        onChange(
          mergeAggConfig<DebateDecideConfig>(aggregationConfig, {
            max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={maxTokensFieldStyle}
    />

    <label htmlFor="config-debate-repair-max-tokens" style={SUB_LABEL_STYLE}>
      Repair Max Tokens
    </label>
    <input
      id="config-debate-repair-max-tokens"
      type="number"
      min={1}
      step={1}
      value={
        getAggConfig<DebateDecideConfig>(aggregationConfig).repair_max_tokens ?? DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS
      }
      onChange={(e) =>
        onChange(
          mergeAggConfig<DebateDecideConfig>(aggregationConfig, {
            repair_max_tokens: parseIntegerOrDefault(e.target.value, DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS),
          }),
        )
      }
      disabled={disabled}
      style={repairMaxTokensFieldStyle}
    />

    <div style={{ fontSize: '12px', color: '#666', marginTop: '5px' }}>
      Groups agents by extracted answer into camps. Unanimous = zero LLM cost.
      <br />
      Split camps trigger a single judge call to adjudicate.
    </div>
  </div>
);

const SECTIONS: Record<
  string,
  React.FC<{
    aggregationConfig: AggregationConfig | undefined;
    models: Model[];
    loadingModels: boolean;
    disabled?: boolean;
    onChange: (config: AggregationConfig) => void;
  }>
> = {
  collect: CollectSection,
  synthesis: SynthesisSection,
  judge: JudgeSection,
  scoring: ScoringSection,
  peer_matrix: PeerMatrixSection,
  majority_vote: MajorityVoteSection,
  debate_decide: DebateDecideSection,
};

const AggregationConfigSection: React.FC<AggregationConfigProps> = ({
  method,
  aggregationConfig,
  models,
  loadingModels,
  disabled = false,
  onChange,
}) => {
  const Section = SECTIONS[method];
  if (!Section) return null;
  return (
    <Section
      aggregationConfig={aggregationConfig}
      models={models}
      loadingModels={loadingModels}
      disabled={disabled}
      onChange={onChange}
    />
  );
};

export default AggregationConfigSection;
