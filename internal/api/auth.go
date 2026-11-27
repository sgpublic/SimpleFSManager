package api

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sgpublic/simplefsmanager/internal/apperr"
	"github.com/sgpublic/simplefsmanager/internal/auth"
)

const (
	authBodyLimit       = 64 << 10 // 64 KiB
	authRateLimitMax    = 10
	authRateLimitWindow = 5 * time.Minute
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *server) registerAuthRoutes(router chi.Router) {
	router.Get("/api/auth/status", func(writer http.ResponseWriter, request *http.Request) {
		status, err := s.auth.Status(request.Context(), request)
		if err != nil {
			logOperationError(s.logger, "get authentication status", err)
			writeError(writer, http.StatusInternalServerError, err)
			return
		}
		writeJSON(writer, http.StatusOK, status)
	})

	router.Post("/api/auth/login", func(writer http.ResponseWriter, request *http.Request) {
		clientKey := clientIP(request)
		if !s.limiter.Allow(clientKey) {
			writeError(writer, http.StatusTooManyRequests, apperr.New(http.StatusTooManyRequests, apperr.CodeAuthRateLimited, "too many authentication attempts; try again later"))
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, authBodyLimit)
		var input loginRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, apperr.New(http.StatusBadRequest, "request_failed", "invalid request body"))
			return
		}
		token, err := s.auth.Login(request.Context(), input.Username, input.Password)
		if err != nil {
			status := apperr.Status(err)
			if status >= http.StatusInternalServerError {
				logOperationError(s.logger, "log in", err)
			}
			writeError(writer, status, err)
			return
		}
		s.limiter.Reset(clientKey)
		http.SetCookie(writer, auth.SessionCookieForRequest(request, token))
		logOperationSuccess(s.logger, "log in")
		writeJSON(writer, http.StatusOK, auth.Status{Authenticated: true, Username: input.Username})
	})

	router.Post("/api/auth/password", func(writer http.ResponseWriter, request *http.Request) {
		user, err := s.auth.User(request.Context(), request)
		if err != nil {
			writeError(writer, apperr.Status(err), err)
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, authBodyLimit)
		var input changePasswordRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			writeError(writer, http.StatusBadRequest, apperr.New(http.StatusBadRequest, "request_failed", "invalid request body"))
			return
		}
		token, err := s.auth.ChangePassword(request.Context(), user, input.CurrentPassword, input.NewPassword)
		if err != nil {
			status := apperr.Status(err)
			if status >= http.StatusInternalServerError {
				logOperationError(s.logger, "change password", err)
			}
			writeError(writer, status, err)
			return
		}
		http.SetCookie(writer, auth.SessionCookieForRequest(request, token))
		logOperationSuccess(s.logger, "change password", "username", user.Username)
		writer.WriteHeader(http.StatusNoContent)
	})

	router.Post("/api/auth/logout", func(writer http.ResponseWriter, request *http.Request) {
		s.auth.Logout(request.Context(), request)
		http.SetCookie(writer, auth.ExpiredSessionCookieForRequest(request))
		logOperationSuccess(s.logger, "log out")
		writer.WriteHeader(http.StatusNoContent)
	})
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || host == "" {
		return request.RemoteAddr
	}
	return host
}
