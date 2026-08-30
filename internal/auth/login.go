// Package auth implements tgproxy-panel's login check, signed session
// cookies, and login-attempt rate limiting (plan.md §5). It is
// framework-agnostic except for RequireSession middleware and cookie
// helpers; HTTP handlers live in internal/httpserver.
package auth

import (
	"crypto/sha256"
	"crypto/subtle"

	"golang.org/x/crypto/bcrypt"
)

// CheckLogin reports whether gotLogin/gotPassword match the configured admin
// credentials (Config.AdminLogin, Config.AdminPasswordHash). Both checks run
// unconditionally, in fixed statement order, rather than short-circuiting in
// a single "loginOK && passOK" boolean expression — that would let a wrong
// login skip the (comparatively slow) bcrypt call and return early, giving
// an attacker a timing oracle on whether the login string alone was right.
func CheckLogin(wantLogin, wantHash, gotLogin, gotPassword string) bool {
	loginOK := constantTimeStringsEqual(wantLogin, gotLogin)
	passOK := bcrypt.CompareHashAndPassword([]byte(wantHash), []byte(gotPassword)) == nil
	return loginOK && passOK
}

// constantTimeStringsEqual compares a and b without leaking their lengths or
// content through timing. subtle.ConstantTimeCompare alone isn't enough
// here: it still requires branching on len(a) != len(b) at the call site
// (fine when lengths are secret-independent, not fine when comparing
// attacker-controlled input against a secret login of unknown length to the
// attacker). Hashing both sides first makes the comparison a fixed 32-byte
// ConstantTimeCompare regardless of input length.
func constantTimeStringsEqual(a, b string) bool {
	ah := sha256.Sum256([]byte(a))
	bh := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ah[:], bh[:]) == 1
}
