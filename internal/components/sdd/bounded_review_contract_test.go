package sdd

import (
	"strings"
	"testing"

	"github.com/gentleman-programming/gentle-ai/internal/catalog"
	"github.com/gentleman-programming/gentle-ai/internal/model"
)

var boundedReviewRequiredClauses = []string{
	"Review is explicit `review/start(target)`",
	"detached, read-only, and terminal after one result",
	"Findings freeze after the initial selected-lens review",
	"neutral structured claims and proof references",
	"Deterministic severe findings become `corroborated` with proof and never invoke a refuter",
	"exactly ONE detached refuter operation for the transaction",
	"Insufficient findings become `inconclusive` and are never auto-fixed",
	"one correction transaction composed of atomic work units",
	"immutable genesis path set",
	"maps exactly to frozen accepted/blocking IDs",
	"exactly one scoped fix-delta validator",
	"original acceptance criteria/tests and correction regression evidence",
	"Later observations are non-blocking follow-ups",
	"can return only `approve` or `escalate`",
	"Final verification is independent requirements/runtime verification",
	"Judgment Day replaces ordinary 4R",
	"Only the parent orchestrator may launch a correction actor or scoped validator",
	"openspec/changes/{change-name}/reviews/transaction.json",
	"sdd/{change-name}/review/transaction",
	"gentle-ai review-validate --cwd <repo> --lineage <id> --gate",
	"never hand-author request JSON",
	"base_advanced_compatible",
	"Ed25519",
	"Model, provider, profile, and effort selection remain optional user choices",
}

func TestBoundedReviewContractRendersForEverySupportedAgent(t *testing.T) {
	agents := catalog.AllAgents()
	if len(agents) != 16 {
		t.Fatalf("catalog.AllAgents() = %d, want 16", len(agents))
	}
	for _, agent := range agents {
		t.Run(string(agent.ID), func(t *testing.T) {
			content := renderSDDOrchestratorAsset(agent.ID)
			assertTextContainsClauses(t, string(agent.ID), content, boundedReviewRequiredClauses)
			for _, forbidden := range []string{
				"exactly THREE refuters total",
				"3 total for full-4R",
				"run at most 2 sweeps per lens",
				"standard review or three lens passes sequentially",
				"verifies fix-touched lines",
				"may append fix-caused defects",
			} {
				if strings.Contains(content, forbidden) {
					t.Errorf("rendered %s retains obsolete review clause %q", agent.ID, forbidden)
				}
			}
		})
	}
	if got := sddOrchestratorAsset(model.AgentPi); got != "generic/sdd-orchestrator.md" {
		t.Fatalf("Pi orchestrator asset = %q, want generic adapter", got)
	}
}

func TestRenderedReviewersAreReadOnlyAndSingleResult(t *testing.T) {
	for _, family := range []string{"claude", "cursor", "kimi", "kiro"} {
		for _, lens := range []string{"risk", "readability", "reliability", "resilience"} {
			path := family + "/agents/review-" + lens + ".md"
			t.Run(family+"/"+lens, func(t *testing.T) {
				content := renderBoundedReviewAsset(path)
				for _, want := range []string{"read-only reviewer", "Return one result and terminate", "neutral structured claims and proof references"} {
					if !strings.Contains(content, want) {
						t.Errorf("%s missing %q", path, want)
					}
				}
			})
		}
	}
}

func TestBoundedReviewContractDoesNotEnforceModelPolicy(t *testing.T) {
	content := boundedReviewContract()
	for _, forbidden := range []string{"MUST use model", "required provider", "enforced effort", "mandatory profile"} {
		if strings.Contains(content, forbidden) {
			t.Errorf("bounded review contract enforces model policy with %q", forbidden)
		}
	}
}
