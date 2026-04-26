package api

import (
	"net/http"
	"strings"

	"github.com/go-logr/logr"
)

// Role defines the RBAC roles for the API.
type Role string

const (
	// RoleViewer can perform GET requests only.
	RoleViewer Role = "viewer"
	// RoleEditor can perform GET and POST/PUT on policies.
	RoleEditor Role = "editor"
	// RoleAdmin has full access to all operations.
	RoleAdmin Role = "admin"
)

// roleHeader is the HTTP header used to pass the user's role.
// In production, this would be derived from a Kubernetes service account token
// or an OIDC identity provider.
const roleHeader = "X-FinOps-Role"

// RBACMiddleware returns a chi middleware that enforces role-based access control.
// Roles are determined from the X-FinOps-Role header:
//   - viewer: GET only
//   - editor: GET + POST/PUT on policies
//   - admin: all operations
//
// Requests without a valid role header are treated as viewer by default.
// Unauthorized requests receive HTTP 403.
func RBACMiddleware(logger logr.Logger) func(http.Handler) http.Handler {
	log := logger.WithName("rbac")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := Role(strings.ToLower(r.Header.Get(roleHeader)))
			if role == "" {
				role = RoleViewer
			}

			if !isAuthorized(role, r.Method, r.URL.Path) {
				log.Info("Unauthorized API request",
					"role", string(role),
					"method", r.Method,
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				writeError(w, http.StatusForbidden, "forbidden: insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isAuthorized checks whether the given role is allowed to perform the
// specified HTTP method on the given path.
func isAuthorized(role Role, method, path string) bool {
	switch role {
	case RoleAdmin:
		// Admin can do everything.
		return true

	case RoleEditor:
		// Editor can GET anything and POST/PUT on policies and reports.
		if method == http.MethodGet {
			return true
		}
		if method == http.MethodPost || method == http.MethodPut {
			if strings.HasPrefix(path, "/api/v1/policies") ||
				strings.HasPrefix(path, "/api/v1/reports") ||
				strings.HasPrefix(path, "/api/v1/recommendations") {
				return true
			}
		}
		return false

	case RoleViewer:
		// Viewer can only GET.
		return method == http.MethodGet

	default:
		// Unknown role — deny.
		return false
	}
}
