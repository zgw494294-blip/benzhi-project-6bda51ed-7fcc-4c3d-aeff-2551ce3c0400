package workflow

import (
	"museum-label-governance/internal/label"
	"time"
)

type AuditView struct {
	Dossier   label.Dossier      `json:"dossier"`
	Revisions []label.Revision   `json:"revisions"`
	Claims    []label.Claim      `json:"claims"`
	Evidence  []label.Evidence   `json:"evidence"`
	Audits    []label.AuditEvent `json:"audits"`
}
type ReviewInput struct {
	ClaimID  string
	Decision string
	Reason   string
	Actor    string
}

type DossierQuery struct {
	Status         label.Status
	ExhibitionName string
	Owner          string
	Limit          int
	Cursor         string
	Actor          string
}

type DossierPage struct {
	Items      []label.Dossier `json:"items"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

type PrecheckResult struct {
	Dossier      label.Dossier   `json:"dossier"`
	Problems     []label.Problem `json:"problems"`
	ProblemCount int             `json:"problemCount"`
}

type ExpertReviewResult struct {
	Dossier label.Dossier `json:"dossier"`
	Claims  []label.Claim `json:"claims"`
}

type CopyReviewInput struct {
	Decision    string
	Reason      string
	Actor       string
	Suggestions []label.CopySuggestion
}

type CopyReviewResult struct {
	Dossier     label.Dossier          `json:"dossier"`
	Problems    []label.Problem        `json:"problems"`
	Suggestions []label.CopySuggestion `json:"suggestions"`
}

type CredentialView struct {
	Credential label.Credential   `json:"credential"`
	Dossier    label.Dossier      `json:"dossier"`
	Snapshot   label.Snapshot     `json:"snapshot"`
	Audits     []label.AuditEvent `json:"audits"`
}

func ValidDecision(v string) bool { return v == "pass" || v == "doubt" || v == "reject" }

type DossierPatch struct {
	ExhibitionName *string
	ObjectRef      *string
	Title          *string
	Owner          *string
}

type EvidenceUsageRevision struct {
	RevisionNo int      `json:"revisionNo"`
	ClaimIDs   []string `json:"claimIds"`
	Valid      bool     `json:"valid"`
}

type EvidenceUsage struct {
	Evidence label.Evidence          `json:"evidence"`
	Usage    []EvidenceUsageRevision `json:"usage"`
}

type PrecheckHistory struct {
	Items []label.PrecheckSnapshot `json:"items"`
}

type PrecheckDiff struct {
	DossierID  string                 `json:"dossierId"`
	From       label.PrecheckSnapshot `json:"from"`
	To         label.PrecheckSnapshot `json:"to"`
	Resolved   []label.Problem        `json:"resolved"`
	Introduced []label.Problem        `json:"introduced"`
	Remaining  []label.Problem        `json:"remaining"`
}

type DecisionCounts struct {
	Pass   int `json:"pass"`
	Doubt  int `json:"doubt"`
	Reject int `json:"reject"`
}

type ExpertReviewProgress struct {
	Dossier         label.Dossier             `json:"dossier"`
	RevisionNo      int                       `json:"revisionNo"`
	TotalCount      int                       `json:"totalCount"`
	CompletedCount  int                       `json:"completedCount"`
	MissingClaimIDs []string                  `json:"missingClaimIds"`
	DecisionCounts  DecisionCounts            `json:"decisionCounts"`
	Drafts          []label.ExpertReviewDraft `json:"drafts"`
}

type CopySuggestionList struct {
	Items        []label.CopySuggestion `json:"items"`
	PendingCount int                    `json:"pendingCount"`
}

type VerificationCheck struct {
	Name        string `json:"name"`
	Valid       bool   `json:"valid"`
	ProblemCode string `json:"problemCode,omitempty"`
}

type CredentialVerification struct {
	CredentialNo   string              `json:"credentialNo"`
	DossierID      string              `json:"dossierId"`
	SnapshotID     string              `json:"snapshotId"`
	RevisionNo     int                 `json:"revisionNo"`
	ContentDigest  string              `json:"contentDigest"`
	IssuedBy       string              `json:"issuedBy"`
	IssuedAt       time.Time           `json:"issuedAt"`
	Valid          bool                `json:"valid"`
	SignatureValid bool                `json:"signatureValid"`
	DigestValid    bool                `json:"digestValid"`
	ProblemCodes   []string            `json:"problemCodes"`
	Checks         []VerificationCheck `json:"checks"`
}
