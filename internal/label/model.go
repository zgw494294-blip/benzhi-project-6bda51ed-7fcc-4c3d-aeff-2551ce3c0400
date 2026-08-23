package label

import "time"

type Status string

const (
	StatusDraft        Status = "draft"
	StatusPrechecked   Status = "prechecked"
	StatusExpertReview Status = "expert_review"
	StatusCopyReview   Status = "copy_review"
	StatusFrozen       Status = "frozen"
	StatusPublished    Status = "published"
)

type Dossier struct {
	ID               string    `json:"id"`
	ExhibitionName   string    `json:"exhibitionName"`
	ObjectRef        string    `json:"objectRef"`
	Title            string    `json:"title"`
	Owner            string    `json:"owner"`
	Status           Status    `json:"status"`
	CurrentRevision  int       `json:"currentRevision"`
	Version          int       `json:"version"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
	RequiresRevision bool      `json:"requiresRevision,omitempty"`
}

type Revision struct {
	DossierID   string    `json:"dossierId"`
	Number      int       `json:"number"`
	Content     string    `json:"content"`
	Claims      []string  `json:"claims"`
	Status      Status    `json:"status"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	DerivedFrom int       `json:"derivedFrom,omitempty"`
}

type Claim struct {
	ID             string     `json:"id"`
	DossierID      string     `json:"dossierId"`
	RevisionNo     int        `json:"revisionNo"`
	Statement      string     `json:"statement"`
	Category       string     `json:"category"`
	EvidenceIDs    []string   `json:"evidenceIds"`
	ReviewDecision string     `json:"reviewDecision,omitempty"`
	ReviewReason   string     `json:"reviewReason,omitempty"`
	Reviewer       string     `json:"reviewer,omitempty"`
	ReviewedAt     *time.Time `json:"reviewedAt,omitempty"`
	ReviewValid    bool       `json:"reviewValid"`
	InheritedFrom  int        `json:"inheritedFromRevision,omitempty"`
}

type Evidence struct {
	ID                    string     `json:"id"`
	DossierID             string     `json:"dossierId"`
	SourceType            string     `json:"sourceType"`
	Citation              string     `json:"citation"`
	Locator               string     `json:"locator"`
	Excerpt               string     `json:"excerpt"`
	ReliabilityNote       string     `json:"reliabilityNote"`
	Checksum              string     `json:"checksum"`
	CreatedAt             time.Time  `json:"createdAt"`
	CreatedRevision       int        `json:"createdRevision"`
	Status                string     `json:"status"`
	ReplacementEvidenceID string     `json:"replacementEvidenceId,omitempty"`
	SupersedeReason       string     `json:"supersedeReason,omitempty"`
	SupersededBy          string     `json:"supersededBy,omitempty"`
	SupersededAt          *time.Time `json:"supersededAt,omitempty"`
	EffectiveRevision     int        `json:"effectiveRevision,omitempty"`
}

type AuditEvent struct {
	ID        string    `json:"id"`
	DossierID string    `json:"dossierId"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Detail    string    `json:"detail"`
	At        time.Time `json:"at"`
}

type Snapshot struct {
	ID               string     `json:"id"`
	DossierID        string     `json:"dossierId"`
	RevisionNo       int        `json:"revisionNo"`
	Content          string     `json:"content"`
	ClaimSnapshot    []Claim    `json:"claimSnapshot"`
	EvidenceManifest []Evidence `json:"evidenceManifest"`
	ContentDigest    string     `json:"contentDigest"`
	FrozenBy         string     `json:"frozenBy"`
	FrozenAt         time.Time  `json:"frozenAt"`
}

type Credential struct {
	CredentialNo  string    `json:"credentialNo"`
	DossierID     string    `json:"dossierId"`
	SnapshotID    string    `json:"snapshotId"`
	RevisionNo    int       `json:"revisionNo"`
	ContentDigest string    `json:"contentDigest"`
	IssuedBy      string    `json:"issuedBy"`
	IssuedAt      time.Time `json:"issuedAt"`
	Signature     string    `json:"signature"`
	SchemaVersion string    `json:"schemaVersion"`
}

type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
}

type CopySuggestion struct {
	ID               string     `json:"id"`
	DossierID        string     `json:"dossierId"`
	RevisionNo       int        `json:"revisionNo"`
	Kind             string     `json:"kind"`
	Start            int        `json:"start"`
	End              int        `json:"end"`
	Suggestion       string     `json:"suggestion"`
	AffectedClaimIDs []string   `json:"affectedClaimIds"`
	Resolved         bool       `json:"resolved"`
	Status           string     `json:"status"`
	DispositionNote  string     `json:"dispositionNote,omitempty"`
	HandledBy        string     `json:"handledBy,omitempty"`
	HandledAt        *time.Time `json:"handledAt,omitempty"`
	HandledRevision  int        `json:"handledRevision,omitempty"`
	CreatedBy        string     `json:"createdBy"`
	CreatedAt        time.Time  `json:"createdAt"`
}

type PrecheckSnapshot struct {
	DossierID  string    `json:"dossierId"`
	Version    int       `json:"version"`
	RevisionNo int       `json:"revisionNo"`
	Problems   []Problem `json:"problems"`
	Status     Status    `json:"status"`
	Count      int       `json:"problemCount"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ExpertReviewDraft struct {
	DossierID  string    `json:"dossierId"`
	RevisionNo int       `json:"revisionNo"`
	ClaimID    string    `json:"claimId"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason"`
	Reviewer   string    `json:"reviewer"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type RevisionDiff struct {
	DossierID      string   `json:"dossierId"`
	FromRevision   int      `json:"fromRevision"`
	ToRevision     int      `json:"toRevision"`
	AddedLines     []string `json:"addedLines"`
	RemovedLines   []string `json:"removedLines"`
	AddedClaims    []string `json:"addedClaimIds"`
	RemovedClaims  []string `json:"removedClaimIds"`
	ModifiedClaims []string `json:"modifiedClaimIds"`
}

func CanTransition(from, to Status) bool {
	if from == StatusDraft && (to == StatusPrechecked || to == StatusDraft) {
		return true
	}
	if from == StatusPrechecked && (to == StatusExpertReview || to == StatusDraft) {
		return true
	}
	if from == StatusExpertReview && (to == StatusCopyReview || to == StatusDraft) {
		return true
	}
	if from == StatusCopyReview && (to == StatusFrozen || to == StatusDraft) {
		return true
	}
	if from == StatusFrozen && to == StatusPublished {
		return true
	}
	return from == to
}
