package powcoins

import (
	"context"
	"testing"
)

func TestSelectClaimCandidateSkipThenAccept(t *testing.T) {
	candidates := []ClaimCandidate{
		{UTXO: UTXO{TxID: "first"}},
		{UTXO: UTXO{TxID: "second"}},
	}
	var calls int
	selected, err := selectClaimCandidate(context.Background(), candidates, func(context.Context, ClaimCandidate) (ClaimDecision, error) {
		calls++
		if calls == 1 {
			return ClaimDecisionSkip, nil
		}
		return ClaimDecisionAccept, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected.UTXO.TxID != "second" {
		t.Fatalf("selected %q, want second", selected.UTXO.TxID)
	}
}

func TestSelectClaimCandidateCancel(t *testing.T) {
	_, err := selectClaimCandidate(context.Background(), []ClaimCandidate{{UTXO: UTXO{TxID: "first"}}},
		func(context.Context, ClaimCandidate) (ClaimDecision, error) {
			return ClaimDecisionCancel, nil
		})
	if err == nil {
		t.Fatal("expected cancel error")
	}
}
