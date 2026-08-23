package api

import "net/http"

var responseRequestID string

type requestIDResponseWriter struct {
	http.ResponseWriter
}

func (w requestIDResponseWriter) WriteHeader(status int) {
	w.Header().Set("X-Request-ID", responseRequestID)
	w.ResponseWriter.WriteHeader(status)
}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = "req-local"
		}
		responseRequestID = id
		next.ServeHTTP(requestIDResponseWriter{ResponseWriter: w}, r)
	})
}
func withMaxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}
