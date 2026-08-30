package auth

import (
	"testing"
	"time"
)

func TestLoginLimiterAllowsUpToMax(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 10*time.Minute)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return start }

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d: Allow = false, want true", i)
		}
		l.RecordFailure("1.2.3.4")
	}
}

func TestLoginLimiterBlocksAfterMax(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 10*time.Minute)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return start }

	for i := 0; i < 3; i++ {
		l.RecordFailure("1.2.3.4")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("Allow = true after maxAttempts failures, want false")
	}
}

func TestLoginLimiterOtherKeysUnaffected(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 10*time.Minute)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return start }

	for i := 0; i < 3; i++ {
		l.RecordFailure("1.2.3.4")
	}
	if !l.Allow("5.6.7.8") {
		t.Fatal("Allow = false for an unrelated key")
	}
}

func TestLoginLimiterResetsAfterCooldown(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 10*time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		l.RecordFailure("1.2.3.4")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("Allow = true immediately after lockout, want false")
	}

	now = now.Add(10*time.Minute + time.Second)
	if !l.Allow("1.2.3.4") {
		t.Fatal("Allow = false after cooldown elapsed, want true")
	}
}

func TestLoginLimiterSuccessResetsCounter(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 10*time.Minute)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return start }

	l.RecordFailure("1.2.3.4")
	l.RecordFailure("1.2.3.4")
	l.RecordSuccess("1.2.3.4")

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d after success reset: Allow = false, want true", i)
		}
		l.RecordFailure("1.2.3.4")
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("Allow = true after re-accumulating maxAttempts failures post-reset")
	}
}

func TestLoginLimiterWindowExpiryDoesNotAccumulateAcrossWindows(t *testing.T) {
	l := NewLoginLimiter(3, time.Minute, 10*time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return now }

	l.RecordFailure("1.2.3.4")
	l.RecordFailure("1.2.3.4")

	// Window elapses without reaching maxAttempts; the next failure should
	// start a fresh window rather than tipping the old count over the edge.
	now = now.Add(2 * time.Minute)
	l.RecordFailure("1.2.3.4")

	if !l.Allow("1.2.3.4") {
		t.Fatal("Allow = false after a slow trickle of failures across windows, want true")
	}
}

func TestLoginLimiterConcurrent(t *testing.T) {
	l := NewDefaultLoginLimiter()
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			key := "concurrent-key"
			if l.Allow(key) {
				l.RecordFailure(key)
			} else {
				l.RecordSuccess(key)
			}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

func TestDefaultLoginLimiterConstants(t *testing.T) {
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"DefaultMaxAttempts", DefaultMaxAttempts, 5},
		{"DefaultCooldown", DefaultCooldown, 15 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}
