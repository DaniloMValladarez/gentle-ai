package reviewtransaction

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativePrePRGateAllowsOnlyCryptographicallyAttestedCompatibleBaseAdvance(t *testing.T) {
	repo := initSnapshotRepo(t)
	baseCommit := trimGit(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	gitSnapshot(t, repo, "branch", "main", baseCommit)
	remote := configurePublicationRemote(t, repo, "main")
	gitSnapshot(t, repo, "checkout", "-qb", "feature")
	writeSnapshotFile(t, repo, "delivery.txt", "reviewed delivery\n")
	gitSnapshot(t, repo, "add", "delivery.txt")
	gitSnapshot(t, repo, "commit", "-m", "delivery")

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.json")
	ledgerPath := filepath.Join(dir, "ledger.json")
	evidencePath := filepath.Join(dir, "evidence.md")
	policy := map[string]any{"pre_pr_ci_trust": map[string]string{
		"issuer": "trusted-ci", "ed25519_public_key": base64.StdEncoding.EncodeToString(publicKey),
	}}
	policyPayload, _ := json.Marshal(policy)
	if err := os.WriteFile(policyPath, policyPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ledgerPath, []byte("{\"schema\":\"gentle-ai.review-ledger/v1\",\"findings\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, []byte("verified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (SnapshotBuilder{Repo: repo}).Build(context.Background(), Target{Kind: TargetBaseDiff, BaseRef: "main"})
	if err != nil {
		t.Fatal(err)
	}
	policyHash, _ := HashArtifact(policyPath)
	ledgerHash, _ := HashLedgerArtifact(ledgerPath)
	evidenceHash, _ := HashArtifact(evidencePath)
	tx, err := NewTransaction(Start{LineageID: "compatible-base", Mode: ModeOrdinary4R, Generation: 1, Snapshot: snapshot, PolicyHash: policyHash})
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.StartReview()
	_ = tx.FreezeFindings([]Finding{}, ledgerHash)
	_, _ = tx.ClassifyEvidence([]FindingEvidence{})
	_ = tx.BeginFinalVerification()
	_ = tx.CompleteFinalVerification(evidenceHash, true)
	receipt, err := tx.Receipt()
	if err != nil {
		t.Fatal(err)
	}
	store, err := AuthoritativeStore(context.Background(), repo, tx.LineageID)
	if err != nil {
		t.Fatal(err)
	}
	appendApprovedStoreChain(t, store, *tx)

	gitSnapshot(t, repo, "checkout", "main")
	writeSnapshotFile(t, repo, "base-only.txt", "disjoint base advance\n")
	gitSnapshot(t, repo, "add", "base-only.txt")
	gitSnapshot(t, repo, "commit", "-m", "advance base")
	gitSnapshot(t, repo, "push", remote, "main")
	newBaseCommit := trimGit(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	gitSnapshot(t, repo, "checkout", "feature")
	featureCommit := trimGit(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	mergedTree := strings.Fields(gitSnapshot(t, repo, "merge-tree", "--write-tree", newBaseCommit, featureCommit))[0]
	attestation := prePRCIAttestation{Schema: prePRCIAttestationSchema, Issuer: "trusted-ci", MergedTree: mergedTree, Status: "success"}
	attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, prePRCIAttestationPreimage(attestation)))
	attestationPath := filepath.Join(dir, "ci-attestation.json")
	attestationPayload, _ := json.Marshal(attestation)
	if err := os.WriteFile(attestationPath, attestationPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	request := GateRequest{
		Schema: GateRequestSchema, Gate: GatePrePR, Target: Target{Kind: TargetBaseDiff, BaseRef: "main"},
		PolicyArtifact: policyPath, LedgerArtifact: ledgerPath, EvidenceArtifact: evidencePath,
		PrePR: &PrePRRequest{CIAttestationArtifact: attestationPath},
	}
	bindGateRequestToStore(t, &request, store)

	originalPreimageHook := artifactPreimagesReadHook
	artifactPreimagesReadHook = func() {
		artifactPreimagesReadHook = originalPreimageHook
		if err := os.WriteFile(policyPath, []byte(`{"pre_pr_ci_trust":null}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { artifactPreimagesReadHook = originalPreimageHook })
	evaluation := EvaluateNativeGate(context.Background(), repo, receipt, request)
	if evaluation.Result != GateAllow || evaluation.Context.BaseAdvance == nil || !evaluation.Context.BaseAdvance.valid() {
		t.Fatalf("compatible base advance = %#v", evaluation)
	}
	if evaluation.Context.BaseAdvance.Status != "base-advanced-compatible" || evaluation.Context.BaseAdvance.MergedResultTree != mergedTree || evaluation.Context.BaseAdvance.OriginalMergeBaseTree != receipt.BaseTree {
		t.Fatalf("base advance context = %#v", evaluation.Context.BaseAdvance)
	}
	if err := os.WriteFile(policyPath, policyPayload, 0o644); err != nil {
		t.Fatal(err)
	}

	request.PrePR = nil
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result == GateAllow {
		t.Fatalf("missing CI attestation authorized base advance: %#v", got)
	}
	request.PrePR = &PrePRRequest{CIAttestationArtifact: attestationPath}
	attestation.Status = "success"
	attestation.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	attestationPayload, _ = json.Marshal(attestation)
	if err := os.WriteFile(attestationPath, attestationPayload, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result == GateAllow {
		t.Fatalf("untrusted success boolean authorized base advance: %#v", got)
	}
}

func TestBuildNativeGateRequestDerivesAuthorityForEveryGate(t *testing.T) {
	repo := initSnapshotRepo(t)
	gitSnapshot(t, repo, "branch", "main", "HEAD")
	configurePublicationRemote(t, repo, "main")
	tx, _, fixture := nativeGateFixture(t, repo, "native-request-all-gates")
	store, err := AuthoritativeStore(context.Background(), repo, tx.LineageID)
	if err != nil {
		t.Fatal(err)
	}
	appendApprovedStoreChain(t, store, tx)
	bundle, err := store.ExportBundle()
	if err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "bundle.json")
	if err := WriteChainBundleAtomic(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		gate    GateKind
		prepare func(*NativeGateRequestInput)
		assert  func(t *testing.T, request GateRequest)
	}{
		{gate: GatePostApply, assert: func(t *testing.T, request GateRequest) {
			if request.Target.Kind != TargetCurrentChanges || request.Target.IntendedUntracked == nil {
				t.Fatalf("post-apply target = %#v", request.Target)
			}
		}},
		{gate: GatePreCommit, assert: func(t *testing.T, request GateRequest) {
			if request.Target.Kind != TargetCurrentChanges || request.Target.IntendedUntracked == nil {
				t.Fatalf("pre-commit target = %#v", request.Target)
			}
		}},
		{gate: GatePrePush, assert: func(t *testing.T, request GateRequest) {
			if request.Target.Kind != TargetExactRevision || request.Target.Revision == "" {
				t.Fatalf("pre-push target = %#v", request.Target)
			}
		}},
		{gate: GatePrePR, prepare: func(input *NativeGateRequestInput) { input.BaseRef = "main" }, assert: func(t *testing.T, request GateRequest) {
			if request.Target.Kind != TargetBaseDiff || !validGitTree(request.Target.BaseRef) {
				t.Fatalf("pre-PR target = %#v", request.Target)
			}
		}},
		{gate: GateRelease, prepare: func(input *NativeGateRequestInput) {
			dir := t.TempDir()
			input.ReleaseConfiguration = writeGateArtifact(t, dir, "configuration", []byte("configuration\n"))
			input.ReleaseGenerated = writeGateArtifact(t, dir, "generated", []byte("generated\n"))
			input.ReleaseProvenance = writeGateArtifact(t, dir, "provenance", []byte("provenance\n"))
			boundary, _ := json.Marshal(map[string]any{"schema": "gentle-ai.release-publication-boundary/v1", "release_tree": tx.Snapshot.CandidateTree, "state": "sealed"})
			freshness, _ := json.Marshal(map[string]any{"schema": "gentle-ai.release-evidence-freshness/v1", "release_tree": tx.Snapshot.CandidateTree, "evidence_hash": tx.EvidenceHash, "state": "current"})
			input.ReleasePublicationBoundary = writeGateArtifact(t, dir, "boundary", boundary)
			input.ReleaseEvidenceFreshness = writeGateArtifact(t, dir, "freshness", freshness)
		}, assert: func(t *testing.T, request GateRequest) {
			if request.Target.Kind != TargetExactRevision || request.Release == nil || request.Release.Revision != request.Target.Revision {
				t.Fatalf("release target = %#v, evidence = %#v", request.Target, request.Release)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(string(tt.gate), func(t *testing.T) {
			input := NativeGateRequestInput{
				Gate: tt.gate, LineageID: tx.LineageID, BundleArtifact: bundlePath,
				PolicyArtifact: fixture.PolicyArtifact, LedgerArtifact: fixture.LedgerArtifact, EvidenceArtifact: fixture.EvidenceArtifact,
			}
			if tt.prepare != nil {
				tt.prepare(&input)
			}
			request, err := BuildNativeGateRequest(context.Background(), repo, input)
			if err != nil {
				t.Fatalf("BuildNativeGateRequest(%s) error = %v", tt.gate, err)
			}
			if request.StoreRevision != bundle.HeadRevision || request.GenesisRevision != bundle.GenesisRevision || request.ChainIdentity != bundle.ChainIdentity || request.BundleDigest != bundle.BundleDigest {
				t.Fatalf("derived authority = %#v", request)
			}
			tt.assert(t, request)
		})
	}
}

func TestNativeReleaseGateDerivesCompleteImmutableBoundary(t *testing.T) {
	repo := initSnapshotRepo(t)
	writeSnapshotFile(t, repo, "tracked.txt", "release\n")
	gitSnapshot(t, repo, "add", "tracked.txt")
	gitSnapshot(t, repo, "commit", "-m", "release")
	releaseCommit := trimGit(gitSnapshot(t, repo, "rev-parse", "HEAD"))
	snapshot, err := (SnapshotBuilder{Repo: repo}).Build(context.Background(), Target{Kind: TargetExactRevision, Revision: releaseCommit})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	artifacts := map[string]string{
		"policy":        "bounded release policy\n",
		"ledger":        "{\"schema\":\"gentle-ai.review-ledger/v1\",\"findings\":[]}\n",
		"evidence":      "fresh verification evidence\n",
		"configuration": "release configuration\n",
		"generated":     "generated artifact manifest\n",
		"provenance":    "signed provenance\n",
		"boundary":      "publication boundary\n",
		"freshness":     "current evidence marker\n",
	}
	paths := make(map[string]string, len(artifacts))
	hashes := make(map[string]string, len(artifacts))
	for name, content := range artifacts {
		path := filepath.Join(dir, name+".txt")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
		hashes[name], err = HashArtifact(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	boundaryPayload, _ := json.Marshal(map[string]any{"schema": "gentle-ai.release-publication-boundary/v1", "release_tree": snapshot.CandidateTree, "state": "sealed"})
	freshnessPayload, _ := json.Marshal(map[string]any{"schema": "gentle-ai.release-evidence-freshness/v1", "release_tree": snapshot.CandidateTree, "evidence_hash": hashes["evidence"], "state": "current"})
	for name, payload := range map[string][]byte{"boundary": boundaryPayload, "freshness": freshnessPayload} {
		if err := os.WriteFile(paths[name], payload, 0o644); err != nil {
			t.Fatal(err)
		}
		hashes[name], _ = HashArtifact(paths[name])
	}
	release := ReleaseEvidence{
		ReleaseTree: snapshot.CandidateTree, ConfigurationHash: hashes["configuration"],
		GeneratedArtifactHash: hashes["generated"], ProvenanceHash: hashes["provenance"],
		PublicationBoundaryHash: hashes["boundary"], PublicationState: PublicationStateSealed,
		EvidenceFreshnessHash: hashes["freshness"], EvidenceFreshnessState: EvidenceFreshnessCurrent,
	}
	tx, err := NewTransaction(Start{
		LineageID: "release-lineage", Mode: ModeOrdinary4R, Generation: 1,
		Snapshot: snapshot, PolicyHash: hashes["policy"],
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := AuthoritativeStore(context.Background(), repo, "release-lineage")
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.StartReview()
	revision, err := store.Append("", Record{Operation: "review/start", Transaction: *tx})
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.FreezeFindings([]Finding{}, hashes["ledger"])
	revision, err = store.Append(revision, Record{Operation: "review/freeze-findings", Transaction: *tx})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tx.ClassifyEvidence([]FindingEvidence{})
	revision, err = store.Append(revision, Record{Operation: "review/classify", Transaction: *tx})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.BindReleaseEvidence(release); err != nil {
		t.Fatal(err)
	}
	revision, err = store.Append(revision, Record{Operation: "review/bind-release-evidence", Transaction: *tx})
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.BeginFinalVerification()
	revision, err = store.Append(revision, Record{Operation: "review/begin-final-verification", Transaction: *tx})
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.CompleteFinalVerification(hashes["evidence"], true)
	revision, err = store.Append(revision, Record{Operation: "review/complete-final-verification", Transaction: *tx})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := tx.Receipt()
	if err != nil {
		t.Fatal(err)
	}
	request := GateRequest{
		Schema: GateRequestSchema, Gate: GateRelease,
		Target:         Target{Kind: TargetExactRevision, Revision: releaseCommit},
		StoreRevision:  revision,
		PolicyArtifact: paths["policy"], LedgerArtifact: paths["ledger"], EvidenceArtifact: paths["evidence"],
		Release: &ReleaseRequest{
			Revision: releaseCommit, ConfigurationArtifact: paths["configuration"],
			GeneratedArtifact: paths["generated"], ProvenanceArtifact: paths["provenance"],
			PublicationBoundaryArtifact: paths["boundary"], PublicationState: PublicationStateSealed,
			EvidenceFreshnessArtifact: paths["freshness"], EvidenceFreshnessState: EvidenceFreshnessCurrent,
		},
	}
	bindGateRequestToStore(t, &request, store)
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateAllow {
		t.Fatalf("EvaluateNativeGate(exact release) = %#v", got)
	}

	request.Release = nil
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateInvalidated {
		t.Fatalf("EvaluateNativeGate(generic release) = %#v", got)
	}
	request.Release = &ReleaseRequest{
		Revision: releaseCommit, ConfigurationArtifact: paths["configuration"],
		GeneratedArtifact: paths["generated"], ProvenanceArtifact: filepath.Join(dir, "missing-provenance"),
		PublicationBoundaryArtifact: paths["boundary"], PublicationState: PublicationStateSealed,
		EvidenceFreshnessArtifact: paths["freshness"], EvidenceFreshnessState: EvidenceFreshnessCurrent,
	}
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateInvalidated {
		t.Fatalf("EvaluateNativeGate(missing provenance) = %#v", got)
	}
}

func TestNativeGateRejectsHistoricalTargetAfterHeadAdvances(t *testing.T) {
	repo := initSnapshotRepo(t)
	transaction, receipt, request := nativeGateFixture(t, repo, "lifecycle-head")
	store, err := AuthoritativeStore(context.Background(), repo, transaction.LineageID)
	if err != nil {
		t.Fatal(err)
	}
	appendApprovedStoreChain(t, store, transaction)
	bundle, err := store.ExportBundle()
	if err != nil {
		t.Fatal(err)
	}
	request.StoreRevision, request.GenesisRevision, request.ChainIdentity, request.BundleDigest = bundle.HeadRevision, bundle.GenesisRevision, bundle.ChainIdentity, bundle.BundleDigest
	request.Target = Target{Kind: TargetExactRevision, Revision: trimGit(gitSnapshot(t, repo, "rev-parse", "HEAD"))}
	writeSnapshotFile(t, repo, "tracked.txt", "newer candidate\n")
	gitSnapshot(t, repo, "add", "tracked.txt")
	gitSnapshot(t, repo, "commit", "-m", "newer")
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result == GateAllow {
		t.Fatal("historical caller-selected target authorized a newer lifecycle candidate")
	}
}

func TestNativePrePushGateAcceptsCommittedCurrentChangesReceipt(t *testing.T) {
	repo, receipt, request := approvedCurrentChangesGateFixture(t, "pre-push-current-changes")
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateAllow {
		t.Fatalf("EvaluateNativeGate(pre-commit current changes) = %#v", got)
	}

	gitSnapshot(t, repo, "add", "tracked.txt")
	gitSnapshot(t, repo, "commit", "-m", "deliver reviewed current changes")
	request.Gate = GatePrePush
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateAllow {
		t.Fatalf("EvaluateNativeGate(pre-push committed current changes) = %#v", got)
	}
}

func TestNativePrePushGateRejectsAdvancedEmptyCurrentChangesReceipt(t *testing.T) {
	repo, receipt, request := approvedEmptyCurrentChangesGateFixture(t, "pre-push-empty-current-changes")
	gitSnapshot(t, repo, "commit", "--allow-empty", "-m", "first empty delivery")
	gitSnapshot(t, repo, "commit", "--allow-empty", "-m", "advance empty delivery")
	request.Gate = GatePrePush

	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result == GateAllow {
		t.Fatalf("EvaluateNativeGate(advanced empty current changes) = %#v, want rejection", got)
	}
}

func TestNativePrePushGateRejectsChangedOrAdvancedHead(t *testing.T) {
	tests := []struct {
		name    string
		lineage string
		advance func(t *testing.T, repo string)
		want    GateResult
	}{
		{
			name:    "changed head",
			lineage: "pre-push-changed",
			advance: func(t *testing.T, repo string) {
				t.Helper()
				writeSnapshotFile(t, repo, "tracked.txt", "altered delivery\n")
				gitSnapshot(t, repo, "add", "tracked.txt")
				gitSnapshot(t, repo, "commit", "-m", "alter reviewed delivery")
			},
			want: GateScopeChanged,
		},
		{
			name:    "advanced head",
			lineage: "pre-push-advanced",
			advance: func(t *testing.T, repo string) {
				t.Helper()
				gitSnapshot(t, repo, "commit", "--allow-empty", "-m", "advance reviewed delivery")
			},
			want: GateInvalidated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, receipt, request := approvedCurrentChangesGateFixture(t, tt.lineage)
			gitSnapshot(t, repo, "add", "tracked.txt")
			gitSnapshot(t, repo, "commit", "-m", "deliver reviewed current changes")
			tt.advance(t, repo)
			request.Gate = GatePrePush

			if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != tt.want {
				t.Fatalf("EvaluateNativeGate(%s) = %#v, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestNativeGateUsesRetainedArtifactContentAndRejectsMismatch(t *testing.T) {
	repo := initSnapshotRepo(t)
	tx, receipt, request := nativeGateFixture(t, repo, "content-gate")
	store, err := AuthoritativeStore(context.Background(), repo, tx.LineageID)
	if err != nil {
		t.Fatal(err)
	}
	appendApprovedStoreChain(t, store, tx)
	bundle, err := store.ExportBundle()
	if err != nil {
		t.Fatal(err)
	}
	request.StoreRevision, request.GenesisRevision, request.ChainIdentity, request.BundleDigest = bundle.HeadRevision, bundle.GenesisRevision, bundle.ChainIdentity, bundle.BundleDigest
	policy, _ := os.ReadFile(request.PolicyArtifact)
	ledger, _ := os.ReadFile(request.LedgerArtifact)
	evidence, _ := os.ReadFile(request.EvidenceArtifact)
	request.PolicyArtifact, request.LedgerArtifact, request.EvidenceArtifact = "missing", "missing", "missing"
	request.PolicyContent, request.LedgerContent, request.EvidenceContent = string(policy), string(ledger), string(evidence)
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateAllow {
		t.Fatalf("retained content gate = %#v", got)
	}
	request.LedgerContent = `{"schema":"gentle-ai.review-ledger/v1","findings":[{"id":"mismatch"}]}`
	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result == GateAllow {
		t.Fatal("mismatched retained ledger content was accepted")
	}
}

func TestNativeGateFinalRecheckRejectsConcurrentRepositoryMutation(t *testing.T) {
	repo, receipt, request := approvedCurrentChangesGateFixture(t, "final-recheck")
	originalHook := finalGateAuthorizationHook
	finalGateAuthorizationHook = func() { writeSnapshotFile(t, repo, "tracked.txt", "concurrent mutation\n") }
	t.Cleanup(func() { finalGateAuthorizationHook = originalHook })

	if got := EvaluateNativeGate(context.Background(), repo, receipt, request); got.Result != GateInvalidated || !strings.Contains(got.Reason, "changed during final authorization") {
		t.Fatalf("concurrent mutation evaluation = %#v", got)
	}
}

func TestLedgerArtifactParsingIsStrict(t *testing.T) {
	valid := `{"schema":"gentle-ai.review-ledger/v1","findings":[]}`
	for _, payload := range []string{valid + `{"schema":"gentle-ai.review-ledger/v1","findings":[]}`, `{"schema":"gentle-ai.review-ledger/v1","findings":[],"unknown":true}`} {
		if _, _, err := hashLedgerPayload([]byte(payload)); err == nil {
			t.Fatalf("hashLedgerPayload(%q) accepted unsupported JSON", payload)
		}
	}
}

func TestExternalEvidenceDispositionComesFromStrictArtifact(t *testing.T) {
	for _, disposition := range []ExternalEvidenceDisposition{ExternalEvidenceInvalidating, ExternalEvidenceEscalating} {
		payload, _ := json.Marshal(map[string]any{"schema": "gentle-ai.external-review-evidence/v1", "disposition": disposition, "evidence_hash": hash("e")})
		if got, err := deriveExternalEvidenceDisposition(payload); err != nil || got != disposition {
			t.Fatalf("deriveExternalEvidenceDisposition(%s) = %q, %v", disposition, got, err)
		}
	}
	if _, err := deriveExternalEvidenceDisposition([]byte(`{"schema":"gentle-ai.external-review-evidence/v1","disposition":"invalidating","evidence_hash":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","success":true}`)); err == nil {
		t.Fatal("external evidence parser trusted an unsupported success boolean")
	}
}

func TestNativeGateRejectsForgedStandaloneTerminalHead(t *testing.T) {
	repo := initSnapshotRepo(t)
	tx, receipt, request := nativeGateFixture(t, repo, "forged-terminal-lineage")
	forged := Store{Dir: filepath.Join(t.TempDir(), "forged-store")}
	revision := writeStoreEvent(t, forged, Record{
		Operation:        "review/complete-final-verification",
		PreviousRevision: hash("f"),
		Transaction:      tx,
	})
	request.StoreDir = forged.Dir
	request.StoreRevision = revision
	request.GenesisRevision = hash("a")
	request.ChainIdentity = hash("b")
	request.BundleDigest = hash("c")

	evaluation := EvaluateNativeGate(context.Background(), repo, receipt, request)
	if evaluation.Result == GateAllow {
		t.Fatalf("forged standalone terminal HEAD authorized the gate: %#v", evaluation)
	}
}

func TestNativeGateCannotBeInfluencedByAlternateStore(t *testing.T) {
	repo := initSnapshotRepo(t)
	tx, receipt, request := nativeGateFixture(t, repo, "trusted-chain-lineage")
	authoritative := Store{Dir: repositoryLineageStoreDir(t, repo, tx.LineageID)}
	revision := appendApprovedStoreChain(t, authoritative, tx)

	alternateTx := approvedStoreTransaction(t, "alternate-lineage")
	alternate := Store{Dir: filepath.Join(t.TempDir(), "alternate-store")}
	writeStoreEvent(t, alternate, Record{
		Operation:        "review/complete-final-verification",
		PreviousRevision: hash("e"),
		Transaction:      alternateTx,
	})
	request.StoreDir = alternate.Dir
	request.StoreRevision = revision
	bindGateRequestToStore(t, &request, authoritative)

	evaluation := EvaluateNativeGate(context.Background(), repo, receipt, request)
	if evaluation.Result != GateAllow {
		t.Fatalf("alternate store influenced trusted repository validation: %#v", evaluation)
	}
}

func nativeGateFixture(t *testing.T, repo, lineage string) (Transaction, Receipt, GateRequest) {
	t.Helper()
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.md")
	ledgerPath := filepath.Join(dir, "ledger.json")
	evidencePath := filepath.Join(dir, "evidence.md")
	for path, content := range map[string]string{
		policyPath:   "bounded policy\n",
		ledgerPath:   "{\"schema\":\"gentle-ai.review-ledger/v1\",\"findings\":[]}\n",
		evidencePath: "verified\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := (SnapshotBuilder{Repo: repo}).Build(context.Background(), Target{Kind: TargetCurrentChanges, IntendedUntracked: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	policyHash, _ := HashArtifact(policyPath)
	ledgerHash, _ := HashArtifact(ledgerPath)
	evidenceHash, _ := HashArtifact(evidencePath)
	tx, err := NewTransaction(Start{LineageID: lineage, Mode: ModeOrdinary4R, Generation: 1, Snapshot: snapshot, PolicyHash: policyHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.StartReview(); err != nil {
		t.Fatal(err)
	}
	if err := tx.FreezeFindings([]Finding{}, ledgerHash); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ClassifyEvidence([]FindingEvidence{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.BeginFinalVerification(); err != nil {
		t.Fatal(err)
	}
	if err := tx.CompleteFinalVerification(evidenceHash, true); err != nil {
		t.Fatal(err)
	}
	receipt, err := tx.Receipt()
	if err != nil {
		t.Fatal(err)
	}
	return *tx, receipt, GateRequest{
		Schema:           GateRequestSchema,
		Gate:             GatePostApply,
		Target:           Target{Kind: TargetCurrentChanges, IntendedUntracked: []string{}},
		PolicyArtifact:   policyPath,
		LedgerArtifact:   ledgerPath,
		EvidenceArtifact: evidencePath,
	}
}

func approvedCurrentChangesGateFixture(t *testing.T, lineage string) (string, Receipt, GateRequest) {
	t.Helper()
	repo := initSnapshotRepo(t)
	writeSnapshotFile(t, repo, "tracked.txt", "reviewed delivery\n")
	transaction, receipt, request := nativeGateFixture(t, repo, lineage)
	store, err := AuthoritativeStore(context.Background(), repo, transaction.LineageID)
	if err != nil {
		t.Fatal(err)
	}
	appendApprovedStoreChain(t, store, transaction)
	bindGateRequestToStore(t, &request, store)
	request.Gate = GatePreCommit
	return repo, receipt, request
}

func approvedEmptyCurrentChangesGateFixture(t *testing.T, lineage string) (string, Receipt, GateRequest) {
	t.Helper()
	repo := initSnapshotRepo(t)
	transaction, receipt, request := nativeGateFixture(t, repo, lineage)
	store, err := AuthoritativeStore(context.Background(), repo, transaction.LineageID)
	if err != nil {
		t.Fatal(err)
	}
	appendApprovedStoreChain(t, store, transaction)
	bindGateRequestToStore(t, &request, store)
	request.Gate = GatePreCommit
	return repo, receipt, request
}

func appendApprovedStoreChain(t *testing.T, store Store, approved Transaction) string {
	t.Helper()
	reviewing := approved
	reviewing.State = StateReviewing
	reviewing.LedgerHash = ""
	reviewing.EvidenceHash = ""
	reviewing.Release = nil
	reviewing.Counters.FinalVerifications = 0
	revision, err := store.Append("", Record{Operation: "review/start", Transaction: reviewing})
	if err != nil {
		t.Fatal(err)
	}
	frozen := reviewing
	if err := frozen.FreezeFindings([]Finding{}, approved.LedgerHash); err != nil {
		t.Fatal(err)
	}
	revision, err = store.Append(revision, Record{Operation: "review/freeze-findings", Transaction: frozen})
	if err != nil {
		t.Fatal(err)
	}
	classified := frozen
	if _, err := classified.ClassifyEvidence([]FindingEvidence{}); err != nil {
		t.Fatal(err)
	}
	revision, err = store.Append(revision, Record{Operation: "review/classify", Transaction: classified})
	if err != nil {
		t.Fatal(err)
	}
	verifying := classified
	if approved.Release != nil {
		bound := classified
		bound.Release = cloneReleaseEvidence(approved.Release)
		revision, err = store.Append(revision, Record{Operation: "review/bind-release-evidence", Transaction: bound})
		if err != nil {
			t.Fatal(err)
		}
		verifying = bound
	}
	if err := verifying.BeginFinalVerification(); err != nil {
		t.Fatal(err)
	}
	revision, err = store.Append(revision, Record{Operation: "review/begin-final-verification", Transaction: verifying})
	if err != nil {
		t.Fatal(err)
	}
	revision, err = store.Append(revision, Record{Operation: "review/complete-final-verification", Transaction: approved})
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func repositoryLineageStoreDir(t *testing.T, repo, lineage string) string {
	t.Helper()
	commonDir := trimGit(gitSnapshot(t, repo, "rev-parse", "--path-format=absolute", "--git-common-dir"))
	return filepath.Join(commonDir, "gentle-ai", "review-transactions", "v1", lineage)
}

func trimGit(value string) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r') {
		value = value[:len(value)-1]
	}
	return value
}

func configurePublicationRemote(t *testing.T, repo, branch string) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitSnapshot(t, repo, "clone", "--bare", repo, remote)
	gitSnapshot(t, repo, "remote", "add", "origin", remote)
	gitSnapshot(t, repo, "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	return remote
}

func writeGateArtifact(t *testing.T, dir, name string, payload []byte) string {
	t.Helper()
	path := filepath.Join(dir, name+".json")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func bindGateRequestToStore(t *testing.T, request *GateRequest, store Store) {
	t.Helper()
	bundle, err := store.ExportBundle()
	if err != nil {
		t.Fatalf("ExportBundle() error = %v", err)
	}
	request.StoreRevision = bundle.HeadRevision
	request.GenesisRevision = bundle.GenesisRevision
	request.ChainIdentity = bundle.ChainIdentity
	request.BundleDigest = bundle.BundleDigest
}
