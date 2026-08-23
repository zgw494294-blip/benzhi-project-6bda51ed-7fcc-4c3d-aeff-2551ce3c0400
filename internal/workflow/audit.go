package workflow

import (
	"museum-label-governance/internal/label"
	"sort"
)

func SortAudit(events []label.AuditEvent) []label.AuditEvent {
	out := append([]label.AuditEvent(nil), events...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}
func AuditActions(events []label.AuditEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range SortAudit(events) {
		out = append(out, e.Action)
	}
	return out
}
