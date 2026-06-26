// Package api is the panel's authenticated HTTP layer (Gin). It is transport
// only — request parsing, authentication, CSRF, and response shaping; business
// logic lives in the service/ and internal/ layers.
//
// Each feature domain is a subpackage (auth, config, dbtransfer, importxui,
// realtime, telemetry, telegram, remotesub) that exposes a Handler. The
// Handler's cross-cutting dependencies (response writers, audit, login-user,
// token-scope checks) are injected as function fields, so a domain handler
// never imports package api and stays unit-testable in isolation.
//
// Security middleware is installed once, at the group level, in apiHandler.go
// BEFORE any route is registered, in this order:
//
//  1. checkLogin    — session gate; skipped only for the exact login and
//     logout paths.
//  2. csrfMiddleware — CSRF protection for state-changing requests.
//
// Because these are group-level, the place where a route is registered does not
// weaken them. HTTP methods are security-relevant (CSRF guards POST, not GET),
// so a route's method must never be changed as part of an unrelated refactor.
//
// Route registration is orchestrated from apiHandler.go. The experimental
// paidsub admin module shows the target convention for a self-contained domain:
//
//	<domain>.RegisterRoutes(group, <domain>.Deps{...})
//
// where Deps carries exactly the injected glue that domain needs.
package api
