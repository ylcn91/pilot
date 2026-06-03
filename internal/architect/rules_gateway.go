package architect

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ylcn91/pilot/internal/pilotapi"
)

// gatewayPathPrefix is the project-relative directory that holds the gateway's
// HTTP server. The auth heuristic only ever looks at files under it.
const gatewayPathPrefix = "internal/gateway"

// handlerRegRe matches an HTTP route registration on a mux, e.g.
// `mux.HandleFunc("/api/v1/tasks", s.handleTasks)` or `apiMux.Handle("/x", h)`.
// Capture group 1 is the receiver (mux name), group 2 is the route path.
var handlerRegRe = regexp.MustCompile(`(\w+)\.Handle(?:Func)?\(\s*"([^"]+)"`)

// unauthenticatedRoutePrefixes are route prefixes that are auth-exempt by
// design (health/readiness probes, websocket upgrade, and inbound webhooks that
// carry their own signature verification). A new registration on one of these
// is not flagged.
var unauthenticatedRoutePrefixes = []string{
	"/health", "/ready", "/live", "/metrics",
	"/ws", "/webhooks/",
}

// GatewayAuthRule is a best-effort heuristic that flags a changed gateway file
// which registers a NEW non-exempt HTTP route while showing no sign of going
// through the auth middleware. Real gateway code mounts protected routes behind
// `s.auth.Middleware(apiMux)`; a sensitive route added straight onto the root
// mux with no auth reference in the file is the regression this catches.
//
// It is deliberately conservative: a gateway file that references auth
// middleware anywhere is trusted (no flag), and exempt route prefixes are never
// flagged. The risk is medium — a heuristic, not a proof.
type GatewayAuthRule struct{}

// NewGatewayAuthRule returns a GatewayAuthRule.
func NewGatewayAuthRule() *GatewayAuthRule { return &GatewayAuthRule{} }

// Name implements Rule.
func (GatewayAuthRule) Name() string { return "gateway-auth" }

// Eval scans each changed gateway *.go file for new non-exempt route
// registrations and flags those in files with no auth-middleware reference.
// Non-gateway files, and files that cannot be read, are skipped.
func (GatewayAuthRule) Eval(ctx context.Context, changedFiles []string, worktreePath string) []Violation {
	if worktreePath == "" {
		return nil
	}
	var out []Violation
	for _, rel := range changedGoFiles(changedFiles) {
		if err := ctx.Err(); err != nil {
			break
		}
		if !isGatewayFile(rel) {
			continue
		}
		routes, hasAuth, ok := scanGatewayFile(filepath.Join(worktreePath, rel))
		if !ok || hasAuth {
			continue
		}
		for _, route := range routes {
			out = append(out, Violation{
				Rule:   "gateway-auth",
				File:   rel,
				Detail: "new gateway route " + route + " registered with no auth middleware in this file",
				Risk:   pilotapi.RiskMedium,
			})
		}
	}
	return out
}

// isGatewayFile reports whether a project-relative path is a gateway source
// file (under internal/gateway, not a test).
func isGatewayFile(rel string) bool {
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(rel, "_test.go") {
		return false
	}
	return rel == gatewayPathPrefix || strings.HasPrefix(rel, gatewayPathPrefix+"/")
}

// scanGatewayFile reads a gateway file and reports (non-exempt route paths it
// registers, whether it references auth middleware, ok). ok is false when the
// file cannot be opened.
func scanGatewayFile(path string) (routes []string, hasAuth bool, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if referencesAuth(line) {
			hasAuth = true
		}
		for _, m := range handlerRegRe.FindAllStringSubmatch(line, -1) {
			if route := m[2]; !isExemptRoute(route) {
				routes = append(routes, route)
			}
		}
	}
	if scanner.Err() != nil {
		return nil, false, false
	}
	return routes, hasAuth, true
}

// referencesAuth reports whether a line shows the file wiring auth — either the
// auth middleware mount or an explicit Authenticate call.
func referencesAuth(line string) bool {
	return strings.Contains(line, "auth.Middleware") ||
		strings.Contains(line, ".Authenticate(") ||
		strings.Contains(line, "Middleware(apiMux")
}

// isExemptRoute reports whether route is auth-exempt by design.
func isExemptRoute(route string) bool {
	for _, p := range unauthenticatedRoutePrefixes {
		if route == p || strings.HasPrefix(route, p) {
			return true
		}
	}
	return false
}
