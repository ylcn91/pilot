package architect

import (
	"context"
	"testing"
)

// TestGatewayAuthRule_AuthReferenceAnywhereSuppressesAllRoutes proves the
// hasAuth flag is file-scoped: when an auth-middleware reference appears
// ANYWHERE in the file — here several functions away from, and AFTER, the
// unauthenticated route registrations — every route in the file is trusted and
// none is flagged. This is the negative path the heuristic deliberately takes
// to stay conservative (a file that wires auth is not nitpicked line-by-line).
func TestGatewayAuthRule_AuthReferenceAnywhereSuppressesAllRoutes(t *testing.T) {
	dir := t.TempDir()
	// Two non-exempt routes registered with no auth on their own lines; the auth
	// mount lives in a completely separate function lower in the file.
	src := `package gateway

func registerPublic(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
	mux.HandleFunc("/api/v1/admin", s.handleAdmin)
}

func unrelated() {
	_ = 1 + 1
}

func mount(root *http.ServeMux, apiMux *http.ServeMux, s *Server) {
	root.Handle("/api/v1/", s.auth.Middleware(apiMux))
}
`
	writeRelFile(t, dir, "internal/gateway/split.go", src)

	v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/split.go"}, dir)
	if len(v) != 0 {
		t.Fatalf("auth middleware referenced anywhere in the file must suppress all route flags, got %+v", v)
	}
}

// TestGatewayAuthRule_AuthReferenceBeforeRoutesSuppresses proves ordering does
// not matter: the auth reference appearing BEFORE any route registration (the
// scanner sets hasAuth once and it sticks) suppresses the later routes too.
func TestGatewayAuthRule_AuthReferenceBeforeRoutesSuppresses(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func mount(root *http.ServeMux, apiMux *http.ServeMux, s *Server) {
	root.Handle("/api/v1/", s.auth.Middleware(apiMux))
}

func registerLater(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
}
`
	writeRelFile(t, dir, "internal/gateway/ordered.go", src)

	v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/ordered.go"}, dir)
	if len(v) != 0 {
		t.Fatalf("an auth reference preceding the routes must still suppress them, got %+v", v)
	}
}
