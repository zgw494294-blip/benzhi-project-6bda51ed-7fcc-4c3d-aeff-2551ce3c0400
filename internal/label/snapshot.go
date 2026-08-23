package label

import "sort"

func SortClaims(claims []Claim) []Claim {
	out := append([]Claim(nil), claims...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func SortEvidence(items []Evidence) []Evidence {
	out := append([]Evidence(nil), items...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
