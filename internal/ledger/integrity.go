package ledger

import (
	"fmt"
	"museum-label-governance/internal/label"
)

// integrityFailure returns a descriptive IntegrityError that callers can match
// with errors.Is(ledger.ErrIntegrity). The detail string is meant to make the
// startup failure identifiable in operator logs.
func integrityFailure(kind, target, detail string) IntegrityError {
	return IntegrityError{Kind: kind, Target: target, Detail: detail}
}

// ValidateIntegrity inspects cross-entity references and digest relationships
// across the persisted ledger. It must accept ledgers produced by legitimate
// write paths while rejecting state where a frozen snapshot, credential,
// revision, claim or evidence record points at a dossier that does not exist
// or where a snapshot/credential digest no longer matches the materialised
// content. The function is read-only; it never repairs damage so operators can
// investigate the corruption.
func ValidateIntegrity(st State) error {
	if st.Dossiers == nil {
		st.Dossiers = map[string]label.Dossier{}
	}
	if st.Revisions == nil {
		st.Revisions = map[string][]label.Revision{}
	}
	if st.Claims == nil {
		st.Claims = map[string][]label.Claim{}
	}
	if st.Evidence == nil {
		st.Evidence = map[string][]label.Evidence{}
	}
	if st.Snapshots == nil {
		st.Snapshots = map[string]label.Snapshot{}
	}
	if st.Credentials == nil {
		st.Credentials = map[string]label.Credential{}
	}

	// Build a per-dossier evidence index so claim/evidence references can be
	// checked without assuming the evidence list is keyed consistently.
	evidenceByDossier := make(map[string]map[string]label.Evidence, len(st.Evidence))
	for dossierID, items := range st.Evidence {
		index := make(map[string]label.Evidence, len(items))
		for _, item := range items {
			index[item.ID] = item
		}
		evidenceByDossier[dossierID] = index
	}

	// Revisions must belong to an existing dossier.
	for dossierID, revisions := range st.Revisions {
		if _, ok := st.Dossiers[dossierID]; !ok {
			return integrityFailure("revision", dossierID, "修订指向不存在的案卷")
		}
		for _, revision := range revisions {
			if revision.DossierID != dossierID {
				return integrityFailure("revision", dossierID, fmt.Sprintf("修订 %d 的案卷引用不一致", revision.Number))
			}
		}
	}

	// Claims must belong to an existing dossier and reference evidence owned by
	// the same dossier. This mirrors the association rules enforced by the
	// write path (see workflow.validateEvidenceIDs).
	for dossierID, claims := range st.Claims {
		if _, ok := st.Dossiers[dossierID]; !ok {
			return integrityFailure("claim", dossierID, "主张指向不存在的案卷")
		}
		available := evidenceByDossier[dossierID]
		for _, claim := range claims {
			if claim.DossierID != dossierID {
				return integrityFailure("claim", claim.ID, "主张的案卷引用不一致")
			}
			for _, evidenceID := range claim.EvidenceIDs {
				item, ok := available[evidenceID]
				if !ok || item.DossierID != dossierID {
					return integrityFailure("claim", claim.ID, fmt.Sprintf("主张引用不存在的证据 %s", evidenceID))
				}
			}
		}
	}

	// Evidence must belong to an existing dossier and its checksum digest must
	// match the persisted excerpt. A mismatch indicates the ledger was edited
	// out of band or corrupted on disk.
	for dossierID, items := range st.Evidence {
		if _, ok := st.Dossiers[dossierID]; !ok {
			return integrityFailure("evidence", dossierID, "证据指向不存在的案卷")
		}
		for _, item := range items {
			if item.DossierID != dossierID {
				return integrityFailure("evidence", item.ID, "证据的案卷引用不一致")
			}
			if label.Digest(item.Excerpt, nil, nil) != item.Checksum {
				return integrityFailure("evidence", item.ID, "证据摘要不一致")
			}
		}
	}

	// Snapshots must reference an existing dossier, the referenced revision must
	// exist, the content digest must match the frozen material, and every
	// claim/evidence entry in the snapshot must still resolve within the
	// dossier. This is where "orphan snapshot" corruption surfaces.
	for snapshotID, snapshot := range st.Snapshots {
		dossier, ok := st.Dossiers[snapshot.DossierID]
		if !ok {
			return integrityFailure("snapshot", snapshotID, fmt.Sprintf("快照引用不存在的案卷 %s", snapshot.DossierID))
		}
		revisions := st.Revisions[snapshot.DossierID]
		if snapshot.RevisionNo < 1 || snapshot.RevisionNo > len(revisions) {
			return integrityFailure("snapshot", snapshotID, fmt.Sprintf("快照引用不存在的修订 %d", snapshot.RevisionNo))
		}
		if revisions[snapshot.RevisionNo-1].DossierID != snapshot.DossierID {
			return integrityFailure("snapshot", snapshotID, "快照与修订的案卷引用不一致")
		}
		if snapshot.ContentDigest != label.Digest(snapshot.Content, snapshot.ClaimSnapshot, snapshot.EvidenceManifest) {
			return integrityFailure("snapshot", snapshotID, "快照摘要不一致")
		}
		// The snapshot's own claims and evidence must resolve to the dossier so
		// that a later verify/issue path can re-materialise them.
		evidenceIndex := evidenceByDossier[snapshot.DossierID]
		claimIndex := map[string]bool{}
		for _, claim := range st.Claims[snapshot.DossierID] {
			if claim.RevisionNo == snapshot.RevisionNo {
				claimIndex[claim.ID] = true
			}
		}
		for _, claim := range snapshot.ClaimSnapshot {
			if claim.DossierID != snapshot.DossierID {
				return integrityFailure("snapshot", snapshotID, fmt.Sprintf("快照主张 %s 的案卷引用不一致", claim.ID))
			}
			if !claimIndex[claim.ID] {
				return integrityFailure("snapshot", snapshotID, fmt.Sprintf("快照主张 %s 在案卷中不存在", claim.ID))
			}
		}
		for _, item := range snapshot.EvidenceManifest {
			current, ok := evidenceIndex[item.ID]
			if !ok || current.DossierID != snapshot.DossierID || current.Checksum != item.Checksum {
				return integrityFailure("snapshot", snapshotID, fmt.Sprintf("快照证据清单 %s 与当前证据不一致", item.ID))
			}
			if label.Digest(item.Excerpt, nil, nil) != item.Checksum {
				return integrityFailure("snapshot", snapshotID, fmt.Sprintf("快照证据清单 %s 的摘要不一致", item.ID))
			}
		}
		_ = dossier // retained for future cross-field checks (e.g. dossier.Status)
	}

	// Credentials must point at an existing dossier and snapshot, and the
	// cross-references (revisionNo, contentDigest) must line up so that
	// credential.verify stays consistent after restart.
	for credentialNo, credential := range st.Credentials {
		if _, ok := st.Dossiers[credential.DossierID]; !ok {
			return integrityFailure("credential", credentialNo, "凭据引用不存在的案卷")
		}
		snapshot, ok := st.Snapshots[credential.SnapshotID]
		if !ok {
			return integrityFailure("credential", credentialNo, "凭据引用不存在的快照")
		}
		if snapshot.DossierID != credential.DossierID {
			return integrityFailure("credential", credentialNo, "凭据与快照的案卷引用不一致")
		}
		if credential.RevisionNo != snapshot.RevisionNo {
			return integrityFailure("credential", credentialNo, "凭据与快照的修订号不一致")
		}
		if credential.ContentDigest != snapshot.ContentDigest {
			return integrityFailure("credential", credentialNo, "凭据与快照的内容摘要不一致")
		}
	}

	return nil
}
