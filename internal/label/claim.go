package label

import "strings"

func ValidateClaim(c Claim) []Problem {
	var p []Problem
	if strings.TrimSpace(c.Statement) == "" {
		p = append(p, Problem{Code: "empty_claim", Message: "事实主张不能为空", Target: c.ID})
	}
	if c.RevisionNo < 1 {
		p = append(p, Problem{Code: "invalid_revision", Message: "主张版本无效", Target: c.ID})
	}
	if len([]rune(c.Statement)) > MaxClaimStatementRunes {
		p = append(p, Problem{Code: "claim_too_long", Message: "事实主张超过字符限制", Target: c.ID})
	}
	if strings.TrimSpace(c.Category) == "" {
		p = append(p, Problem{Code: "missing_category", Message: "主张类别不能为空", Target: c.ID})
	}
	return p
}
func ClaimPassed(c Claim) bool {
	return c.ReviewDecision == "pass" && c.ReviewValid && len(ValidateClaim(c)) == 0
}
