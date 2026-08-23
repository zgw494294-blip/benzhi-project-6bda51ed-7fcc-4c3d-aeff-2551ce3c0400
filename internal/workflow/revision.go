package workflow

import (
	"museum-label-governance/internal/label"
	"strings"
	"time"
	"unicode"
)

func NewRevision(d label.Dossier, content, actor string) (label.Revision, error) {
	content = label.NormalizeContent(content)
	if strings.TrimSpace(content) == "" {
		return label.Revision{}, ErrValidation
	}
	return label.Revision{DossierID: d.ID, Number: d.CurrentRevision + 1, Content: content, Status: label.StatusDraft, CreatedBy: actor, CreatedAt: time.Now(), DerivedFrom: d.CurrentRevision}, nil
}

func FactTextChanged(before, after string) bool {
	return normalizeFactText(before) != normalizeFactText(after)
}

func normalizeFactText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
}

// ClaimsAffectedByContent first matches claims to changed text, then falls back
// to line position when a revision uses one fact claim per line.
func ClaimsAffectedByContent(before, after string, claims []label.Claim) []string {
	if len(claims) == 0 || !FactTextChanged(before, after) {
		return nil
	}
	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")
	_, _, oldChanged, newChanged := lineChanges(before, after)
	affected := map[string]bool{}
	for _, claim := range claims {
		statement := normalizeFactText(claim.Statement)
		if statement == "" {
			continue
		}
		for _, index := range oldChanged {
			if factTextOverlaps(statement, normalizeFactText(oldLines[index])) {
				affected[claim.ID] = true
			}
		}
		for _, index := range newChanged {
			if factTextOverlaps(statement, normalizeFactText(newLines[index])) {
				affected[claim.ID] = true
			}
		}
	}
	if len(claims) == len(oldLines) {
		for _, index := range oldChanged {
			if index < len(claims) {
				affected[claims[index].ID] = true
			}
		}
	}
	if len(claims) == len(newLines) {
		for _, index := range newChanged {
			if index < len(claims) {
				affected[claims[index].ID] = true
			}
		}
	}
	if len(affected) == 0 {
		for _, claim := range claims {
			affected[claim.ID] = true
		}
	}
	result := make([]string, 0, len(affected))
	for _, claim := range claims {
		if affected[claim.ID] {
			result = append(result, claim.ID)
		}
	}
	return result
}

func factTextOverlaps(statement, line string) bool {
	return statement != "" && line != "" && (strings.Contains(line, statement) || strings.Contains(statement, line))
}

func lineChanges(before, after string) (removed, added []string, oldChanged, newChanged []int) {
	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")
	lcs := make([][]int, len(oldLines)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(newLines)+1)
	}
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	for i, j := 0, 0; i < len(oldLines) || j < len(newLines); {
		switch {
		case i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j]:
			i++
			j++
		case j < len(newLines) && (i == len(oldLines) || lcs[i][j+1] > lcs[i+1][j]):
			added = append(added, newLines[j])
			newChanged = append(newChanged, j)
			j++
		default:
			removed = append(removed, oldLines[i])
			oldChanged = append(oldChanged, i)
			i++
		}
	}
	return removed, added, oldChanged, newChanged
}
func AffectedClaims(claims []label.Claim, revision int) []label.Claim {
	var out []label.Claim
	for _, c := range claims {
		if c.RevisionNo == revision {
			out = append(out, c)
		}
	}
	return out
}
