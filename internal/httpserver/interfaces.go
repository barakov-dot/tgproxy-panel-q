package httpserver

import (
	"github.com/barakov-dot/tgproxy-panel/internal/service"
)

// userStore and profileApplier are aliases for internal/service's Store and
// Applier interfaces: this package's read-only handlers (list/detail/
// settings) need a subset of Store, and Server.actions (a *service.Actions)
// needs the whole thing, so there is no reason to keep a separate, narrower
// interface here — a fake passed to httpserver.New must satisfy the same
// contract service.New requires anyway.
type userStore = service.Store
type profileApplier = service.Applier
