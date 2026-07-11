package reviewtransaction

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
)

// NativeGateRequestInput contains artifact locations and lifecycle inputs only.
// Repository, store, bundle, tree, digest, and chain identities are derived.
type NativeGateRequestInput struct {
	Gate                       GateKind
	LineageID                  string
	BundleArtifact             string
	PolicyArtifact             string
	LedgerArtifact             string
	FixDeltaArtifact           string
	EvidenceArtifact           string
	ExternalEvidenceArtifact   string
	IntendedUntracked          []string
	BaseRef                    string
	PrePRCIAttestation         string
	ReleaseConfiguration       string
	ReleaseGenerated           string
	ReleaseProvenance          string
	ReleasePublicationBoundary string
	ReleaseEvidenceFreshness   string
}

func BuildNativeGateRequest(ctx context.Context, repo string, input NativeGateRequestInput) (GateRequest, error) {
	if strings.TrimSpace(input.LineageID) == "" || strings.TrimSpace(input.BundleArtifact) == "" {
		return GateRequest{}, errors.New("native gate request requires lineage and bundle artifact")
	}
	store, err := AuthoritativeStore(ctx, repo, input.LineageID)
	if err != nil {
		return GateRequest{}, fmt.Errorf("derive authoritative review store: %w", err)
	}
	authoritative, err := store.ExportBundle()
	if err != nil {
		return GateRequest{}, fmt.Errorf("export authoritative review bundle: %w", err)
	}
	payload, err := os.ReadFile(input.BundleArtifact)
	if err != nil {
		return GateRequest{}, fmt.Errorf("read review bundle artifact: %w", err)
	}
	named, err := ParseChainBundle(payload)
	if err != nil {
		return GateRequest{}, fmt.Errorf("parse review bundle artifact: %w", err)
	}
	if !reflect.DeepEqual(named, authoritative) {
		return GateRequest{}, errors.New("named review bundle does not match the authoritative repository chain")
	}
	request := GateRequest{
		Schema: GateRequestSchema, Gate: input.Gate,
		StoreRevision: authoritative.HeadRevision, GenesisRevision: authoritative.GenesisRevision,
		ChainIdentity: authoritative.ChainIdentity, BundleDigest: authoritative.BundleDigest,
		PolicyArtifact: input.PolicyArtifact, LedgerArtifact: input.LedgerArtifact,
		FixDeltaArtifact: input.FixDeltaArtifact, EvidenceArtifact: input.EvidenceArtifact,
		ExternalEvidenceArtifact: input.ExternalEvidenceArtifact,
	}
	switch input.Gate {
	case GatePostApply, GatePreCommit:
		intended := input.IntendedUntracked
		if intended == nil {
			intended = []string{}
		}
		request.Target = Target{Kind: TargetCurrentChanges, IntendedUntracked: intended}
	case GatePrePush:
		head, err := resolveCommit(ctx, repo, "HEAD")
		if err != nil {
			return GateRequest{}, err
		}
		request.Target = Target{Kind: TargetExactRevision, Revision: head}
	case GatePrePR:
		_, _, baseCommit, err := resolveAuthoritativePublicationBase(ctx, repo)
		if err != nil {
			return GateRequest{}, err
		}
		if strings.TrimSpace(input.BaseRef) != "" {
			expected, expectedErr := resolveCommit(ctx, repo, input.BaseRef)
			if expectedErr != nil || expected != baseCommit {
				return GateRequest{}, errors.New("native pre-PR base does not match the remote publication boundary")
			}
		}
		request.Target = Target{Kind: TargetBaseDiff, BaseRef: baseCommit}
		if strings.TrimSpace(input.PrePRCIAttestation) != "" {
			request.PrePR = &PrePRRequest{CIAttestationArtifact: input.PrePRCIAttestation}
		}
	case GateRelease:
		head, err := resolveCommit(ctx, repo, "HEAD")
		if err != nil {
			return GateRequest{}, err
		}
		request.Target = Target{Kind: TargetExactRevision, Revision: head}
		request.Release = &ReleaseRequest{
			Revision: head, ConfigurationArtifact: input.ReleaseConfiguration,
			GeneratedArtifact: input.ReleaseGenerated, ProvenanceArtifact: input.ReleaseProvenance,
			PublicationBoundaryArtifact: input.ReleasePublicationBoundary,
			EvidenceFreshnessArtifact:   input.ReleaseEvidenceFreshness,
		}
	default:
		return GateRequest{}, fmt.Errorf("unsupported review gate %q", input.Gate)
	}
	preimages, err := readGateArtifactPreimages(request)
	if err != nil {
		return GateRequest{}, err
	}
	terminal := authoritative.Events[len(authoritative.Events)-1]
	record, err := parseRecordPayload(terminal.Payload)
	if err != nil {
		return GateRequest{}, fmt.Errorf("parse authoritative terminal review event: %w", err)
	}
	if _, err := validateFixDeltaArtifact(preimages.fixDelta, record.Transaction); err != nil {
		return GateRequest{}, err
	}
	request.ExternalEvidence, err = deriveExternalEvidenceDisposition(preimages.externalEvidence)
	if err != nil {
		return GateRequest{}, err
	}
	if request.Release != nil {
		releaseSnapshot, snapshotErr := (SnapshotBuilder{Repo: repo}).Build(ctx, Target{Kind: TargetExactRevision, Revision: request.Release.Revision})
		if snapshotErr != nil {
			return GateRequest{}, snapshotErr
		}
		publicationState, releaseTree, err := parsePublicationBoundary(preimages.publicationBoundary)
		if err != nil || releaseTree != releaseSnapshot.CandidateTree {
			return GateRequest{}, errors.New("release publication boundary is not sealed for current HEAD")
		}
		freshnessState, freshnessTree, evidenceHash, err := parseEvidenceFreshness(preimages.evidenceFreshness)
		if err != nil || freshnessTree != releaseSnapshot.CandidateTree || evidenceHash != hashArtifactPayload(preimages.evidence) {
			return GateRequest{}, errors.New("release evidence freshness is not current for current HEAD and verification evidence")
		}
		request.Release.PublicationState = publicationState
		request.Release.EvidenceFreshnessState = freshnessState
	}
	request.preimages = &preimages
	if err := validateGateRequest(request); err != nil {
		return GateRequest{}, err
	}
	return request, nil
}
