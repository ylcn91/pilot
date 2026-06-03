package architect

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

func TestGatewayAuthRule_FlagsUnauthenticatedNewRoute(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func register(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
}
`
	writeRelFile(t, dir, "internal/gateway/leak.go", src)

	v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/leak.go"}, dir)
	if len(v) != 1 {
		t.Fatalf("expected 1 gateway-auth violation, got %+v", v)
	}
	if v[0].Rule != "gateway-auth" || v[0].Risk != pilotapi.RiskMedium {
		t.Errorf("unexpected violation: %+v", v[0])
	}
}

func TestGatewayAuthRule_SilentWhenFileWiresAuth(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func register(mux *http.ServeMux, apiMux *http.ServeMux, s *Server) {
	apiMux.HandleFunc("/api/v1/secrets", s.handleSecrets)
	mux.Handle("/api/v1/", s.auth.Middleware(apiMux))
}
`
	writeRelFile(t, dir, "internal/gateway/ok.go", src)

	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/ok.go"}, dir); len(v) != 0 {
		t.Fatalf("file that mounts auth middleware must not be flagged, got %+v", v)
	}
}

func TestGatewayAuthRule_ExemptRoutesNotFlagged(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func register(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/webhooks/github", s.handleGithubWebhook)
}
`
	writeRelFile(t, dir, "internal/gateway/probes.go", src)

	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/probes.go"}, dir); len(v) != 0 {
		t.Fatalf("exempt routes must not be flagged, got %+v", v)
	}
}

func TestGatewayAuthRule_IgnoresNonGatewayFiles(t *testing.T) {
	dir := t.TempDir()
	src := `package other

func register(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
}
`
	writeRelFile(t, dir, "internal/other/routes.go", src)

	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/other/routes.go"}, dir); len(v) != 0 {
		t.Fatalf("non-gateway file must be ignored, got %+v", v)
	}
}

func TestGatewayAuthRule_IgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func register(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
}
`
	writeRelFile(t, dir, "internal/gateway/leak_test.go", src)

	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/leak_test.go"}, dir); len(v) != 0 {
		t.Fatalf("gateway test file must be ignored, got %+v", v)
	}
}

func TestGatewayAuthRule_FlagsEachUnauthenticatedRoute(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func register(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
	mux.HandleFunc("/api/v1/admin", s.handleAdmin)
}
`
	writeRelFile(t, dir, "internal/gateway/two.go", src)

	v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/two.go"}, dir)
	if len(v) != 2 {
		t.Fatalf("expected 2 violations for 2 unauthenticated routes, got %+v", v)
	}
}

func TestGatewayAuthRule_AuthenticateCallSuppresses(t *testing.T) {
	dir := t.TempDir()
	src := `package gateway

func (s *Server) handleSecrets(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Authenticate(r); err != nil {
		return
	}
}

func register(mux *http.ServeMux, s *Server) {
	mux.HandleFunc("/api/v1/secrets", s.handleSecrets)
}
`
	writeRelFile(t, dir, "internal/gateway/inline.go", src)

	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/inline.go"}, dir); len(v) != 0 {
		t.Fatalf("explicit Authenticate call must suppress the flag, got %+v", v)
	}
}

func TestGatewayAuthRule_EmptyWorktreeIsBestEffort(t *testing.T) {
	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/x.go"}, ""); v != nil {
		t.Errorf("empty worktree must yield nil, got %+v", v)
	}
}

func TestGatewayAuthRule_MissingFileSkipped(t *testing.T) {
	dir := t.TempDir()
	if v := NewGatewayAuthRule().Eval(context.Background(),
		[]string{"internal/gateway/ghost.go"}, dir); len(v) != 0 {
		t.Fatalf("missing file must be skipped, got %+v", v)
	}
}

func TestIsVendoredOrMeta(t *testing.T) {
	cases := map[string]bool{
		"internal/x/y.go":         false,
		"vendor/foo/bar.go":       true,
		".git/hooks/x.go":         true,
		"node_modules/a/b.go":     true,
		"dist/out.go":             true,
		"internal/vendorish/x.go": false,
	}
	for path, want := range cases {
		if got := isVendoredOrMeta(path); got != want {
			t.Errorf("isVendoredOrMeta(%q) = %v, want %v", path, got, want)
		}
	}
}
