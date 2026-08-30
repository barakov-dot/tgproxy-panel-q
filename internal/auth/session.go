package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// SessionLifetime is how long a session cookie stays valid after issuance.
// 24h: long enough that the single admin isn't re-prompted to log in
// mid-session on a small internal panel, short enough to bound how long a
// leaked cookie stays useful. plan.md §5 asks for a timeout but doesn't
// mandate a value, and it isn't exposed as a config knob.
const SessionLifetime = 24 * time.Hour

// cookieSep separates the base64 payload from its base64 signature in a
// cookie value. It can't appear in unpadded base64url output, so splitting
// on it is unambiguous.
const cookieSep = "."

// Sessions issues and verifies signed session cookie values, HMAC-SHA256'd
// with Config.SessionSecret. There is deliberately no session store: the
// cookie itself carries everything needed to verify it (an expiry) and is
// tamper-evident, so verification needs no shared state beyond the secret.
type Sessions struct {
	secret []byte

	// now defaults to time.Now; tests override it to check expiry without
	// sleeping for real.
	now func() time.Time
}

// NewSessions builds a Sessions verifier/issuer for the given secret
// (Config.SessionSecret). secret is copied so callers can reuse or discard
// their slice freely.
func NewSessions(secret string) *Sessions {
	s := make([]byte, len(secret))
	copy(s, secret)
	return &Sessions{secret: s, now: time.Now}
}

// New issues a fresh cookie value good for SessionLifetime from now. The
// payload is just an expiry timestamp — there's exactly one admin account,
// so there's nothing else worth encoding.
func (s *Sessions) New() string {
	expiresAt := s.now().Add(SessionLifetime).Unix()
	payload := strconv.FormatInt(expiresAt, 10)
	payloadEnc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	sig := s.sign(payloadEnc)
	return payloadEnc + cookieSep + base64.RawURLEncoding.EncodeToString(sig)
}

// Verify reports whether cookieValue is a well-formed, correctly signed,
// not-yet-expired session cookie previously issued by New with the same
// secret. Every failure mode — malformed input, a tampered payload, a
// tampered signature, or an expired-but-otherwise-valid cookie — reports as
// false, not an error: to a caller deciding whether to let a request
// through, an expired session isn't exceptional, it's just not valid
// anymore, and this keeps garbage input (arbitrary attacker-supplied cookie
// bytes) something Verify simply rejects rather than something that must be
// separately guarded against panicking.
func (s *Sessions) Verify(cookieValue string) bool {
	payloadEnc, sigEnc, ok := strings.Cut(cookieValue, cookieSep)
	if !ok {
		return false
	}

	sig, err := base64.RawURLEncoding.DecodeString(sigEnc)
	if err != nil {
		return false
	}
	if !hmac.Equal(sig, s.sign(payloadEnc)) {
		return false
	}

	payload, err := base64.RawURLEncoding.DecodeString(payloadEnc)
	if err != nil {
		return false
	}
	expiresAt, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		return false
	}

	return s.now().Before(time.Unix(expiresAt, 0))
}

func (s *Sessions) sign(payloadEnc string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payloadEnc))
	return mac.Sum(nil)
}
