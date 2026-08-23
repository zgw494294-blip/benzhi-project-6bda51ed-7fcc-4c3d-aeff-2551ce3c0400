package api

import (
	"fmt"
	"net/http"
	"strconv"
)

func parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 || v > 100 {
		return 0, fmt.Errorf("limit必须在1到100之间")
	}
	return v, nil
}
func idempotencyKey(r *http.Request) string { return r.Header.Get("Idempotency-Key") }
