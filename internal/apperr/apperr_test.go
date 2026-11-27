package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestCodeStatusAndParams(t *testing.T) {
	base := WithParams(http.StatusBadRequest, CodeAuthPasswordTooShort, "too short", map[string]any{"minLength": 12})
	wrapped := fmt.Errorf("bootstrap: %w", base)

	if got := Code(wrapped); got != CodeAuthPasswordTooShort {
		t.Fatalf("Code = %q, want %q", got, CodeAuthPasswordTooShort)
	}
	if got := Status(wrapped); got != http.StatusBadRequest {
		t.Fatalf("Status = %d, want %d", got, http.StatusBadRequest)
	}
	if got := Params(wrapped)["minLength"]; got != 12 {
		t.Fatalf("Params = %#v, want minLength=12", Params(wrapped))
	}
	if !IsCode(wrapped, CodeAuthPasswordTooShort) {
		t.Fatal("IsCode returned false for matching error")
	}

	plain := errors.New("boom")
	if got := Code(plain); got != CodeInternalError {
		t.Fatalf("plain Code = %q, want %q", got, CodeInternalError)
	}
	if got := Status(plain); got != http.StatusInternalServerError {
		t.Fatalf("plain Status = %d, want %d", got, http.StatusInternalServerError)
	}
}
