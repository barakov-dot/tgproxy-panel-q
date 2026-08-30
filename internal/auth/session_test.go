package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSessionsRoundTrip(t *testing.T) {
	s := NewSessions("some-session-secret-value")

	cookie := s.New()
	if cookie == "" {
		t.Fatal("New returned empty cookie")
	}
	if !s.Verify(cookie) {
		t.Fatal("Verify rejected a freshly issued cookie")
	}
}

func TestSessionsWrongSecret(t *testing.T) {
	issuer := NewSessions("secret-one")
	verifier := NewSessions("secret-two")

	cookie := issuer.New()
	if verifier.Verify(cookie) {
		t.Fatal("Verify accepted a cookie signed with a different secret")
	}
}

func TestSessionsTamperedPayload(t *testing.T) {
	s := NewSessions("some-session-secret-value")
	cookie := s.New()

	payloadEnc, sigEnc, ok := strings.Cut(cookie, cookieSep)
	if !ok {
		t.Fatalf("cookie %q missing separator", cookie)
	}
	tampered := payloadEnc + "AA" + cookieSep + sigEnc
	if s.Verify(tampered) {
		t.Fatal("Verify accepted a cookie with a tampered payload")
	}
}

func TestSessionsTamperedSignature(t *testing.T) {
	s := NewSessions("some-session-secret-value")
	cookie := s.New()

	payloadEnc, sigEnc, ok := strings.Cut(cookie, cookieSep)
	if !ok {
		t.Fatalf("cookie %q missing separator", cookie)
	}
	tampered := payloadEnc + cookieSep + sigEnc + "AA"
	if s.Verify(tampered) {
		t.Fatal("Verify accepted a cookie with a tampered signature")
	}
}

func TestSessionsExpired(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSessions("some-session-secret-value")
	s.now = func() time.Time { return start }

	cookie := s.New()
	if !s.Verify(cookie) {
		t.Fatal("Verify rejected a fresh cookie")
	}

	s.now = func() time.Time { return start.Add(SessionLifetime + time.Second) }
	if s.Verify(cookie) {
		t.Fatal("Verify accepted an expired cookie")
	}
}

func TestSessionsGarbageInput(t *testing.T) {
	s := NewSessions("some-session-secret-value")

	garbage := []string{
		"",
		".",
		"not-base64!.also-not-base64!",
		"..",
		"a.b.c",
		strings.Repeat("x", 10000),
		"YQ.YQ", // valid base64 ("a"), but not a valid signature or numeric payload
	}
	for _, g := range garbage {
		if s.Verify(g) {
			t.Errorf("Verify(%q) = true, want false", g)
		}
	}
}
