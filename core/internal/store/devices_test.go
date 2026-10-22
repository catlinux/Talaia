package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// deviceSeq keeps token hashes unique even inside the same nanosecond.
var deviceSeq atomic.Int64

func deviceHash(prefix string) []byte {
	return []byte(fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), deviceSeq.Add(1)))
}

func newTrustedDevice(t *testing.T, s *Store, userID, name string) TrustedDevice {
	t.Helper()

	device, err := s.CreateTrustedDevice(context.Background(), userID, deviceHash("device-token"),
		"pin-hash", name, deviceHash("browser"), time.Now())
	if err != nil {
		t.Fatalf("create trusted device: %v", err)
	}
	return device
}

func TestTrustedDeviceLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	now := time.Now().Truncate(time.Millisecond)

	device, err := s.CreateTrustedDevice(ctx, user.ID, deviceHash("device-token"),
		"pin-hash", "laptop", deviceHash("browser"), now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if device.ID == "" || device.UserID != user.ID || device.Name != "laptop" {
		t.Errorf("device = %+v", device)
	}
	if device.PinHash != "pin-hash" || device.FailedPins != 0 {
		t.Errorf("device = %+v, want the supplied PIN hash and no failures", device)
	}
	if device.CreatedAt.IsZero() || !device.LastMFAAt.Equal(now) {
		t.Errorf("timestamps = created %s, mfa %s", device.CreatedAt, device.LastMFAAt)
	}
	if !device.LastUsedAt.IsZero() || !device.RemovedAt.IsZero() {
		t.Errorf("a new device has last_used_at or removed_at: %+v", device)
	}

	byToken, err := s.GetTrustedDeviceByTokenHash(ctx, device.TokenHash)
	if err != nil {
		t.Fatalf("get by token: %v", err)
	}
	if byToken.ID != device.ID {
		t.Errorf("by token id = %q, want %q", byToken.ID, device.ID)
	}
	byID, err := s.GetTrustedDevice(ctx, user.ID, device.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if byID.TokenHash == nil || byID.TokenHash[0] != device.TokenHash[0] {
		t.Errorf("by id token hash differs")
	}

	if _, err := s.GetTrustedDevice(ctx, "00000000-0000-0000-0000-000000000000", device.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user got the device: %v", err)
	}

	touchedAt := now.Add(time.Minute)
	if err := s.TouchTrustedDevice(ctx, device.ID, touchedAt); err != nil {
		t.Fatalf("touch: %v", err)
	}
	touched, err := s.GetTrustedDevice(ctx, user.ID, device.ID)
	if err != nil {
		t.Fatalf("get touched: %v", err)
	}
	if !touched.LastUsedAt.Equal(touchedAt) || touched.FailedPins != 0 {
		t.Errorf("touched device = %+v", touched)
	}

	list, err := s.ListTrustedDevices(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != device.ID {
		t.Errorf("list = %+v, want the device", list)
	}
}

func TestTrustedDevicePINAttemptsAreBounded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	device := newTrustedDevice(t, s, user.ID, "laptop")

	for attempt := 1; attempt <= maxTestPINFailures; attempt++ {
		got, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if got != attempt {
			t.Errorf("attempts = %d, want %d", got, attempt)
		}
	}
	// No attempt is left: not even a correct PIN may be checked any more.
	if _, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures); !errors.Is(err, ErrNotFound) {
		t.Errorf("reserve past the limit = %v, want ErrNotFound", err)
	}

	// The handler disables the device; a disabled device has no attempts.
	if err := s.DeleteTrustedDevice(ctx, user.ID, device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTrustedDeviceByTokenHash(ctx, device.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Errorf("disabled device is still usable by token: %v", err)
	}
	if _, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures); !errors.Is(err, ErrNotFound) {
		t.Errorf("reserve on a disabled device = %v, want ErrNotFound", err)
	}
}

func TestTrustedDevicePINAttemptsUnderConcurrency(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	device := newTrustedDevice(t, s, user.ID, "laptop")

	const requests = 40
	results := make(chan error, requests)
	for range requests {
		go func() {
			_, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures)
			results <- err
		}()
	}
	granted := 0
	for range requests {
		if err := <-results; err == nil {
			granted++
		} else if !errors.Is(err, ErrNotFound) {
			t.Errorf("reserve: %v", err)
		}
	}
	if granted != maxTestPINFailures {
		t.Errorf("granted %d attempts out of %d parallel requests, want %d", granted, requests, maxTestPINFailures)
	}
}

const maxTestPINFailures = 5

func TestTrustedDeviceTouchResetsFailures(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	device := newTrustedDevice(t, s, user.ID, "laptop")

	if _, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchTrustedDevice(ctx, device.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	failures, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures)
	if err != nil {
		t.Fatal(err)
	}
	if failures != 1 {
		t.Errorf("attempts after a success = %d, want 1", failures)
	}
}

func TestTrustedDeviceLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	for i := 0; i < MaxTrustedDevices; i++ {
		newTrustedDevice(t, s, user.ID, fmt.Sprintf("device-%d", i))
	}
	if _, err := s.CreateTrustedDevice(ctx, user.ID, deviceHash("device-token"),
		"pin-hash", "one too many", deviceHash("browser"), time.Now()); !errors.Is(err, ErrDeviceLimit) {
		t.Fatalf("eleventh device = %v, want ErrDeviceLimit", err)
	}

	list, err := s.ListTrustedDevices(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != MaxTrustedDevices {
		t.Fatalf("devices = %d, want %d", len(list), MaxTrustedDevices)
	}
	if err := s.DeleteTrustedDevice(ctx, user.ID, list[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.CreateTrustedDevice(ctx, user.ID, deviceHash("device-token"),
		"pin-hash", "after a removal", deviceHash("browser"), time.Now()); err != nil {
		t.Fatalf("create after deleting one: %v", err)
	}
}

func TestTrustedDevicesArePerUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	// CreateUser allows a single user, but the table must still isolate rows.
	otherName := fmt.Sprintf("other-%d", time.Now().UnixNano())
	var otherID string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO app_user (username, password_hash) VALUES ($1, 'pin-hash') RETURNING id`,
		otherName).Scan(&otherID); err != nil {
		t.Fatalf("create the second user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, otherID)
	})
	otherDevice := newTrustedDevice(t, s, otherID, "other laptop")

	if _, err := s.GetTrustedDevice(ctx, user.ID, otherDevice.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user's device was readable: %v", err)
	}
	if err := s.DeleteTrustedDevice(ctx, user.ID, otherDevice.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user's device was deleted: %v", err)
	}
	if _, err := s.GetTrustedDevice(ctx, otherID, otherDevice.ID); err != nil {
		t.Errorf("the owner lost the device: %v", err)
	}
}

func TestRevokeUserDevices(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	first := newTrustedDevice(t, s, user.ID, "first")
	second := newTrustedDevice(t, s, user.ID, "second")

	removed, err := s.RevokeUserDevices(ctx, user.ID)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}
	for _, device := range []TrustedDevice{first, second} {
		if _, err := s.GetTrustedDevice(ctx, user.ID, device.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("device %s survived the revocation: %v", device.Name, err)
		}
	}
	if removed, err := s.RevokeUserDevices(ctx, user.ID); err != nil || removed != 0 {
		t.Errorf("second revocation = %d, %v, want 0", removed, err)
	}
}

func TestCredentialChangesRevokeDevices(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("password", func(t *testing.T) {
		user := newTestUser(t, s)
		device := newTrustedDevice(t, s, user.ID, "password device")
		if err := s.SetPassword(ctx, user.ID, "a-new-password-hash"); err != nil {
			t.Fatalf("set password: %v", err)
		}
		if _, err := s.GetTrustedDevice(ctx, user.ID, device.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("password change kept the device: %v", err)
		}
	})

	t.Run("totp reset", func(t *testing.T) {
		user := newTestUser(t, s)
		if err := s.SetTOTP(ctx, user.ID, []byte("sealed secret")); err != nil {
			t.Fatalf("set totp: %v", err)
		}
		device := newTrustedDevice(t, s, user.ID, "totp device")
		if err := s.ResetTOTP(ctx, user.ID); err != nil {
			t.Fatalf("reset totp: %v", err)
		}
		if _, err := s.GetTrustedDevice(ctx, user.ID, device.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("totp reset kept the device: %v", err)
		}
	})
}

func TestCompleteDeviceMFA(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	device := newTrustedDevice(t, s, user.ID, "laptop")
	if _, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReservePINAttempt(ctx, device.ID, maxTestPINFailures); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Truncate(time.Millisecond)
	browser := deviceHash("new-browser")
	if err := s.CompleteDeviceMFA(ctx, device.ID, browser, now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	stored, err := s.GetTrustedDevice(ctx, user.ID, device.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !stored.LastMFAAt.Equal(now) || !stored.LastUsedAt.Equal(now) {
		t.Errorf("timestamps = mfa %s, used %s, want %s", stored.LastMFAAt, stored.LastUsedAt, now)
	}
	if string(stored.BrowserHash) != string(browser) || stored.FailedPins != 0 {
		t.Errorf("stored = %+v", stored)
	}
}

func TestCreateTrustedDeviceValidatesItsInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	now := time.Now()
	tests := []struct {
		name        string
		userID      string
		tokenHash   []byte
		pinHash     string
		deviceName  string
		browserHash []byte
	}{
		{name: "unknown user", userID: "00000000-0000-0000-0000-000000000000", tokenHash: []byte("t"), pinHash: "p", deviceName: "n", browserHash: []byte("b")},
		{name: "no token", userID: user.ID, pinHash: "p", deviceName: "n", browserHash: []byte("b")},
		{name: "no pin", userID: user.ID, tokenHash: []byte("t"), deviceName: "n", browserHash: []byte("b")},
		{name: "blank name", userID: user.ID, tokenHash: []byte("t"), pinHash: "p", deviceName: "  ", browserHash: []byte("b")},
		{name: "no browser", userID: user.ID, tokenHash: []byte("t"), pinHash: "p", deviceName: "n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.CreateTrustedDevice(ctx, tt.userID, tt.tokenHash, tt.pinHash,
				tt.deviceName, tt.browserHash, now); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
