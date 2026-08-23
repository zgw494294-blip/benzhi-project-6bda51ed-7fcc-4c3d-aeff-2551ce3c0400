package label

const (
	MaxContentRunes         = 500
	MaxEvidenceExcerptRunes = 2000
	MaxListItems            = 100
	MaxClaimStatementRunes  = 1000
	MaxCitationRunes        = 1000
	MaxLocatorRunes         = 500
	MaxReliabilityRunes     = 1000
	MaxReviewBatchItems     = 100
	MaxDossierFieldRunes    = 200
	MaxOwnerRunes           = 100
	MaxPrecheckHistoryItems = 100
)

const (
	EvidenceActive      = "active"
	EvidenceSuperseded  = "superseded"
	SuggestionPending   = "pending"
	SuggestionApplied   = "applied"
	SuggestionDismissed = "dismissed"
)

func ValidStatus(s Status) bool {
	switch s {
	case StatusDraft, StatusPrechecked, StatusExpertReview, StatusCopyReview, StatusFrozen, StatusPublished:
		return true
	default:
		return false
	}
}

func IsTerminal(s Status) bool     { return s == StatusPublished }
func IsReviewStatus(s Status) bool { return s == StatusExpertReview || s == StatusCopyReview }
