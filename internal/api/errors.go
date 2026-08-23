package api

import "net/http"

type HTTPError struct {
	Status        int
	Code, Message string
}

func (e HTTPError) Error() string { return e.Code + ": " + e.Message }
func methodAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
}
