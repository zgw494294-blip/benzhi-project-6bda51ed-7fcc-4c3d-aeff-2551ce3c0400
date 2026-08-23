package label

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

func Precheck(d Dossier, r Revision, claims []Claim, evidence map[string]Evidence) []Problem {
	var out []Problem
	if strings.TrimSpace(d.ExhibitionName) == "" {
		out = append(out, Problem{"missing_exhibition", "展览名称不能为空", "exhibitionName"})
	}
	if strings.TrimSpace(d.ObjectRef) == "" {
		out = append(out, Problem{"missing_object", "藏品标识不能为空", "objectRef"})
	}
	if strings.TrimSpace(d.Title) == "" {
		out = append(out, Problem{"missing_title", "展签标题不能为空", "title"})
	}
	if strings.TrimSpace(r.Content) == "" {
		out = append(out, Problem{"missing_content", "正文不能为空", "content"})
	}
	seenYears := map[string]bool{}
	seenNames := map[string]bool{}
	if len(claims) == 0 {
		out = append(out, Problem{"missing_claims", "正文尚未拆分事实主张", "claims"})
	}
	for _, c := range claims {
		if strings.TrimSpace(c.Statement) == "" {
			out = append(out, Problem{"empty_claim", "事实主张不能为空", c.ID})
			continue
		}
		if len(c.EvidenceIDs) == 0 {
			out = append(out, Problem{"orphan_claim", "主张缺少来源证据", c.ID})
		}
		for _, id := range c.EvidenceIDs {
			item, ok := evidence[id]
			if !ok || item.DossierID != d.ID || item.CreatedRevision > r.Number || !EvidenceIsUsable(item) || Digest(item.Excerpt, nil, nil) != item.Checksum {
				out = append(out, Problem{"invalid_evidence", "引用的证据不存在", id})
			}
		}
		for _, y := range regexp.MustCompile(`\b(1[0-9]{3}|20[0-9]{2})\b`).FindAllString(c.Statement, -1) {
			seenYears[y] = true
		}
		for _, n := range []string{"故宫", "大英博物馆", "卢浮宫"} {
			if strings.Contains(c.Statement, n) {
				seenNames[n] = true
			}
		}
	}
	if len(seenYears) > 1 {
		out = append(out, Problem{"conflicting_years", "事实主张包含相互矛盾的年代", "claims"})
	}
	if len(seenNames) > 1 {
		out = append(out, Problem{"conflicting_names", "事实主张包含相互矛盾的机构名称", "claims"})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Target == out[j].Target {
			return out[i].Code < out[j].Code
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func DiffProblems(from, to []Problem) (resolved, introduced, remaining []Problem) {
	identity := func(p Problem) string { return p.Target + "\x00" + p.Code }
	left, right := map[string]Problem{}, map[string]Problem{}
	for _, problem := range from {
		left[identity(problem)] = problem
	}
	for _, problem := range to {
		right[identity(problem)] = problem
	}
	for key, problem := range left {
		if current, ok := right[key]; ok {
			remaining = append(remaining, current)
		} else {
			resolved = append(resolved, problem)
		}
	}
	for key, problem := range right {
		if _, ok := left[key]; !ok {
			introduced = append(introduced, problem)
		}
	}
	sortProblems := func(items []Problem) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Target == items[j].Target {
				return items[i].Code < items[j].Code
			}
			return items[i].Target < items[j].Target
		})
	}
	sortProblems(resolved)
	sortProblems(introduced)
	sortProblems(remaining)
	return
}

func CopyProblems(content string) []Problem {
	var out []Problem
	if len([]rune(content)) > 500 {
		out = append(out, Problem{"length_limit", "正文超过500字符限制", "content"})
	}
	if strings.Contains(content, "据说") || strings.Contains(content, "可能是") {
		out = append(out, Problem{"sensitive_wording", "包含未经确认的敏感表述", "content"})
	}
	return out
}

func Digest(content string, claims []Claim, evidence []Evidence) string {
	claims = SortClaims(claims)
	evidence = SortEvidence(evidence)
	b := []byte(content)
	for _, c := range claims {
		b = append(b, []byte("|"+c.ID+":"+c.Statement)...)
		ids := append([]string(nil), c.EvidenceIDs...)
		sort.Strings(ids)
		for _, e := range ids {
			b = append(b, []byte("#"+e)...)
		}
	}
	for _, e := range evidence {
		b = append(b, []byte("|"+e.ID+":"+e.Checksum)...)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func ValidateCopySuggestions(content string, suggestions []CopySuggestion, validClaims map[string]bool) []Problem {
	var out []Problem
	n := len([]rune(content))
	for i, suggestion := range suggestions {
		target := fmt.Sprintf("suggestions[%d]", i)
		if suggestion.Kind != "term" && suggestion.Kind != "sensitive" && suggestion.Kind != "length" {
			out = append(out, Problem{Code: "invalid_suggestion_kind", Message: "建议类型必须为term、sensitive或length", Target: target + ".kind"})
		}
		if suggestion.Start < 0 || suggestion.End <= suggestion.Start || suggestion.End > n {
			out = append(out, Problem{Code: "invalid_suggestion_range", Message: "建议字符区间越界", Target: target + ".range"})
		}
		if strings.TrimSpace(suggestion.Suggestion) == "" {
			out = append(out, Problem{Code: "empty_suggestion", Message: "建议内容不能为空", Target: target + ".suggestion"})
		}
		for _, claimID := range suggestion.AffectedClaimIDs {
			if !validClaims[claimID] {
				out = append(out, Problem{Code: "unknown_affected_claim", Message: "受影响主张不存在", Target: claimID})
			}
		}
	}
	return out
}

func Signature(credentialNo, digest, issuer string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|museum-label-v1", credentialNo, digest, issuer)))
	return hex.EncodeToString(h[:])
}
