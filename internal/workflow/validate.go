package workflow

import (
	"museum-label-governance/internal/label"
	"strings"
)

func ValidateCreate(exhibition, object, title, content, owner string) error {
	fields := map[string]string{"exhibitionName": exhibition, "objectRef": object, "title": title, "content": content, "owner": owner}
	for k, v := range fields {
		if strings.TrimSpace(v) == "" {
			return FieldError{k, "不能为空"}
		}
	}
	if len([]rune(content)) > label.MaxContentRunes {
		return FieldError{"content", "超过字符限制"}
	}
	for field, value := range map[string]string{"exhibitionName": exhibition, "objectRef": object, "title": title} {
		if len([]rune(strings.TrimSpace(value))) > label.MaxDossierFieldRunes {
			return FieldError{field, "超过字符限制"}
		}
	}
	if len([]rune(strings.TrimSpace(owner))) > label.MaxOwnerRunes {
		return FieldError{"owner", "超过字符限制"}
	}
	return nil
}
func ValidateReview(in ReviewInput) error {
	if !ValidDecision(in.Decision) {
		return FieldError{"decision", "必须为pass、doubt或reject"}
	}
	if strings.TrimSpace(in.Actor) == "" {
		return FieldError{"actor", "不能为空"}
	}
	return nil
}
