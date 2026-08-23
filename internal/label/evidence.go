package label

import "strings"

func EvidenceIsActive(e Evidence) bool {
	return e.Status == "" || e.Status == EvidenceActive
}

func EvidenceIsUsable(e Evidence) bool {
	return EvidenceIsActive(e) && e.ReplacementEvidenceID == ""
}

func ValidateEvidence(e Evidence) []Problem {
	var p []Problem
	if strings.TrimSpace(e.Citation) == "" {
		p = append(p, Problem{Code: "missing_citation", Message: "证据引用不能为空", Target: e.ID})
	}
	if strings.TrimSpace(e.Excerpt) == "" {
		p = append(p, Problem{Code: "missing_excerpt", Message: "证据摘录不能为空", Target: e.ID})
	}
	if strings.TrimSpace(e.SourceType) == "" {
		p = append(p, Problem{Code: "missing_source_type", Message: "来源类型不能为空", Target: e.ID})
	}
	if strings.TrimSpace(e.Locator) == "" {
		p = append(p, Problem{Code: "missing_locator", Message: "来源定位不能为空", Target: e.ID})
	}
	if strings.TrimSpace(e.ReliabilityNote) == "" {
		p = append(p, Problem{Code: "missing_reliability", Message: "可靠性说明不能为空", Target: e.ID})
	}
	if len([]rune(e.Excerpt)) > MaxEvidenceExcerptRunes {
		p = append(p, Problem{Code: "excerpt_too_long", Message: "证据摘录过长", Target: e.ID})
	}
	if len([]rune(e.Citation)) > MaxCitationRunes || len([]rune(e.Locator)) > MaxLocatorRunes || len([]rune(e.ReliabilityNote)) > MaxReliabilityRunes {
		p = append(p, Problem{Code: "evidence_field_too_long", Message: "证据字段超过字符限制", Target: e.ID})
	}
	return p
}
