package label

import "strings"

func NormalizeContent(s string) string   { return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) }
func NormalizeStatement(s string) string { return strings.Join(strings.Fields(s), " ") }
func NormalizeCategory(s string) string  { return strings.ToLower(strings.TrimSpace(s)) }
