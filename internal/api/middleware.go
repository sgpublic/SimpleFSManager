package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/sgpublic/simplefsmanager/internal/apperr"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.ResponseWriter.Write(body)
}

func requestErrorLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			response := &statusWriter{ResponseWriter: writer}
			next.ServeHTTP(response, request)
			if response.status >= http.StatusBadRequest {
				logger.Error("request failed", "method", request.Method, "path", request.URL.Path, "status", response.status)
			}
		})
	}
}

func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic while serving request", "method", r.Method, "path", r.URL.Path, "panic", recovered, "stack", string(debug.Stack()))
					writeError(w, http.StatusInternalServerError, apperr.New(http.StatusInternalServerError, apperr.CodeInternalError, "internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
