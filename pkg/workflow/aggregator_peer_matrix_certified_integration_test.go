package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

type certifiedPeerMatrixProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *certifiedPeerMatrixProvider) Name() string { return "certified-peer-test" }

func (p *certifiedPeerMatrixProvider) Models() []providers.Model {
	return []providers.Model{{
		ID:         "certified-peer-model",
		Name:       "Certified Peer Test",
		Provider:   p.Name(),
		ContextLen: 4096,
		MaxTokens:  1024,
		Available:  true,
	}}
}

func (p *certifiedPeerMatrixProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()

	candidate := ""
	for _, message := range req.Messages {
		if strings.Contains(message.Content, "response-a") {
			candidate = "a"
			break
		}
		for _, id := range []string{"b", "c", "d", "e"} {
			if strings.Contains(message.Content, "response-"+id) {
				candidate = id
				break
			}
		}
	}

	score := 1
	if candidate == "a" {
		score = 10
	}
	content := `{"quality":{"reasoning":"deterministic test score","score":1}}`
	if score == 10 {
		content = `{"quality":{"reasoning":"deterministic test score","score":10}}`
	}
	return &providers.CompletionResponse{
		ID:      "certified-peer-test-response",
		Model:   req.Model,
		Content: content,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}, nil
}

func (p *certifiedPeerMatrixProvider) EstimateTokens(text string) int { return len(text) / 4 }
func (p *certifiedPeerMatrixProvider) Cost(string, int, int) float64  { return 0 }

func (p *certifiedPeerMatrixProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestCertifiedPeerMatrixSkipsRealProviderCalls(t *testing.T) {
	provider := &certifiedPeerMatrixProvider{}
	registry := providers.NewRegistry()
	registry.Register(provider)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "a", Model: "certified-peer-model", Output: "response-a"},
		{AgentID: "b", Model: "certified-peer-model", Output: "response-b"},
		{AgentID: "c", Model: "certified-peer-model", Output: "response-c"},
		{AgentID: "d", Model: "certified-peer-model", Output: "response-d"},
		{AgentID: "e", Model: "certified-peer-model", Output: "response-e"},
	}
	config := map[string]interface{}{
		"eval_system_prompt":   "Return a strict rubric score.",
		"eval_prompt":          "Candidate: {{response}}\n{{rubric}}",
		"normalization":        "none",
		"max_parallel":         5,
		"temperature":          0.0,
		"max_tokens":           100,
		"certified_early_stop": true,
		"rubric": []RubricCriterion{{
			Name:        "quality",
			Weight:      1,
			Description: "overall quality",
		}},
	}

	result, err := (&PeerMatrixAggregator{}).Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if result.Winner != "a" {
		t.Fatalf("winner = %q, want a", result.Winner)
	}
	if got, want := provider.CallCount(), 15; got != want {
		t.Fatalf("provider calls = %d, want %d (full matrix would be 20)", got, want)
	}
	if !strings.Contains(result.Reasoning, "skipped 5 (25.0%)") {
		t.Fatalf("reasoning does not expose certified savings: %q", result.Reasoning)
	}
	if result.TokensUsed != 15*15 {
		t.Fatalf("aggregation tokens = %d, want %d", result.TokensUsed, 15*15)
	}
	if result.EvalMatrix == nil || result.EvalMatrix.Certificate == nil {
		t.Fatal("certified run did not attach a machine-readable proof certificate")
	}
	certificate := result.EvalMatrix.Certificate
	if !certificate.Certified || certificate.Winner != "a" {
		t.Fatalf("certificate = %+v, want certified winner a", certificate)
	}
	if certificate.ProofVersion != peerMatrixProofVersion {
		t.Fatalf("proof version = %q, want %q", certificate.ProofVersion, peerMatrixProofVersion)
	}
	if certificate.CompletedEvaluations != 15 || certificate.TotalEvaluations != 20 || certificate.SkippedEvaluations != 5 {
		t.Fatalf("certificate counts = completed %d total %d skipped %d, want 15/20/5", certificate.CompletedEvaluations, certificate.TotalEvaluations, certificate.SkippedEvaluations)
	}
	if certificate.SavingsRatio != 0.25 {
		t.Fatalf("certificate savings ratio = %.4f, want 0.25", certificate.SavingsRatio)
	}
	if got := len(certificate.SkippedPairs); got != certificate.SkippedEvaluations {
		t.Fatalf("certificate has %d skipped pairs, want %d", got, certificate.SkippedEvaluations)
	}
	for _, pair := range certificate.SkippedPairs {
		if pair.ReviewerID == pair.CandidateID {
			t.Fatalf("certificate marked self-review as skipped: %+v", pair)
		}
	}
}
