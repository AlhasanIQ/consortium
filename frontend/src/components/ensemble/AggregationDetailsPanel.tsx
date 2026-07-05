import type { AgentState } from '../../stores/ensembleStore';
import type { AggregationDetails } from '../../types/workflow';
import { JudgeDetails } from './aggregation/JudgeDetails';
import { PeerMatrixDetails } from './aggregation/PeerMatrixDetails';
import { ScoringDetails } from './aggregation/ScoringDetails';
import { SynthesisDetails } from './aggregation/SynthesisDetails';

interface AggregationDetailsPanelProps {
  details: AggregationDetails;
  agents: AgentState[];
  collapsed?: boolean;
}

export function AggregationDetailsPanel({ details, agents, collapsed = true }: AggregationDetailsPanelProps) {
  if (!details) return null;

  const renderDetails = () => {
    switch (details.method) {
      case 'judge':
        return <JudgeDetails details={details} agents={agents} collapsed={collapsed} />;
      case 'scoring':
        return <ScoringDetails details={details} agents={agents} collapsed={collapsed} />;
      case 'synthesis':
        return <SynthesisDetails details={details} agents={agents} collapsed={collapsed} />;
      case 'peer_matrix':
        return <PeerMatrixDetails details={details} agents={agents} collapsed={collapsed} />;
      default:
        // 'collect' method and unknown methods don't need special display
        return null;
    }
  };

  return <div style={{ marginTop: '12px' }}>{renderDetails()}</div>;
}
