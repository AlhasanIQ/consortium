package workflow

import "fmt"

func peerMatrixCertifiedEarlyStop(config map[string]interface{}) (bool, error) {
	if config == nil {
		return false, nil
	}
	raw, exists := config["certified_early_stop"]
	if !exists {
		return false, nil
	}
	enabled, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("peer_matrix aggregationConfig.certified_early_stop must be boolean: %w", ErrAggregationConfig)
	}
	return enabled, nil
}
