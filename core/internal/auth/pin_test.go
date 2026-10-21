package auth

import (
	"strings"
	"testing"
)

func TestValidatePIN(t *testing.T) {
	tests := []struct {
		name    string
		pin     string
		wantErr string
	}{
		{name: "six digits", pin: "482915"},
		{name: "twelve digits", pin: "482915730264"},
		{name: "zero is allowed", pin: "482905"},
		{name: "five digits", pin: "48291", wantErr: "between 6 and 12"},
		{name: "thirteen digits", pin: "4829157302648", wantErr: "between 6 and 12"},
		{name: "empty", pin: "", wantErr: "between 6 and 12"},
		{name: "letters", pin: "48291a", wantErr: "only digits"},
		{name: "spaces", pin: "482 915", wantErr: "only digits"},
		{name: "all the same", pin: "000000", wantErr: "same digit"},
		{name: "all the same long", pin: "777777777777", wantErr: "same digit"},
		{name: "ascending", pin: "123456", wantErr: "straight sequence"},
		{name: "ascending with zero", pin: "012345", wantErr: "straight sequence"},
		{name: "descending", pin: "654321", wantErr: "straight sequence"},
		{name: "descending with zero", pin: "987654", wantErr: "straight sequence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePIN(tt.pin)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidatePIN(%q) = %v", tt.pin, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidatePIN(%q) = %v, want an error containing %q", tt.pin, err, tt.wantErr)
			}
			if tt.pin != "" && strings.Contains(err.Error(), tt.pin) {
				t.Errorf("the error echoes the PIN: %v", err)
			}
		})
	}
}
