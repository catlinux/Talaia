package auth

import (
	"errors"
	"fmt"
)

const (
	// MinPINLength and MaxPINLength bound a trusted-device PIN.
	MinPINLength = 6
	MaxPINLength = 12
)

// ValidatePIN enforces the PIN policy: digits only, 6 to 12 long, and neither
// every digit the same nor a straight ascending or descending sequence.
func ValidatePIN(pin string) error {
	if len(pin) < MinPINLength || len(pin) > MaxPINLength {
		return fmt.Errorf("the PIN must be between %d and %d digits long", MinPINLength, MaxPINLength)
	}
	for i := 0; i < len(pin); i++ {
		if pin[i] < '0' || pin[i] > '9' {
			return errors.New("the PIN must contain only digits")
		}
	}

	allSame := true
	ascending := true
	descending := true
	for i := 1; i < len(pin); i++ {
		if pin[i] != pin[0] {
			allSame = false
		}
		if pin[i] != pin[i-1]+1 {
			ascending = false
		}
		if pin[i] != pin[i-1]-1 {
			descending = false
		}
	}
	if allSame {
		return errors.New("the PIN must not use the same digit repeatedly")
	}
	if ascending || descending {
		return errors.New("the PIN must not be a straight sequence")
	}
	return nil
}
