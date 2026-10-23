package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/catlinux/Talaia/core/internal/store"
)

func TestWriteValidationError(t *testing.T) {
	tests := []struct {
		name, field, reason string
	}{
		{"invite stack name", "scope.name", "required"},
		{"unknown role", "role", "invalid_value"},
		{"scope type", "scope.type", "invalid_value"},
		{"missing server", "scope.server_id", "required"},
		{"unknown server", "scope.server_id", "unknown"},
		{"api key binding index", "bindings[0].scope.name", "required"},
		{"team empty name", "name", "required"},
		{"team long name", "name", "too_long"},
		{"oidc issuer", "issuer", "invalid_format"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			writeValidationError(r, fieldError{tt.field, tt.reason})
			var body errorBody
			if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if r.Code != 400 || body.Error.Code != "invalid_request" || body.Error.Field != tt.field || body.Error.Reason != tt.reason {
				t.Fatalf("got status %d, error %#v", r.Code, body.Error)
			}
		})
	}
}

func TestValidateAdminName(t *testing.T) {
	for _, tt := range []struct{ name, reason string }{{"", "required"}, {string(make([]byte, 65)), "too_long"}} {
		err := validateAdminName("name", tt.name)
		var fieldErr fieldError
		ok := errors.As(err, &fieldErr)
		if !ok || fieldErr.Field != "name" || fieldErr.Reason != tt.reason {
			t.Fatalf("got %#v, want name/%s", err, tt.reason)
		}
	}
}

func TestDuplicateGroupNameKeepsNameTaken(t *testing.T) {
	r := httptest.NewRecorder()
	newUserAdminHandler(UserAdminDeps{}).writeStoreError(r, store.ErrGroupNameExists)
	var body errorBody
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if r.Code != 409 || body.Error.Code != "name_taken" {
		t.Fatalf("got status %d, error %#v", r.Code, body.Error)
	}
}
