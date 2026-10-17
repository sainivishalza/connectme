package auth

import (
	"net/http"

	"connectme/pkg/middleware"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleModerator Role = "moderator"
	RoleAdmin     Role = "admin"
)

type Permission string

const (
	PermReviewReports Permission = "reports:review"
	PermBanUser       Permission = "users:ban"
	PermManageHotels  Permission = "hotels:manage"
	PermViewAuditLog  Permission = "audit:view"
)

var rank = map[Role]int{RoleUser: 1, RoleModerator: 2, RoleAdmin: 3}

// permissions is the permission matrix. Unknown roles get nothing.
var permissions = map[Role]map[Permission]bool{
	RoleModerator: {PermReviewReports: true, PermBanUser: true},
	RoleAdmin:     {PermReviewReports: true, PermBanUser: true, PermManageHotels: true, PermViewAuditLog: true},
}

// Can reports whether a role holds a permission.
func Can(role string, p Permission) bool { return permissions[Role(role)][p] }

// RequireRole allows the given role or any higher one (admin > moderator > user).
// It must run after middleware.Auth. The role comes from the access token, so a
// demotion takes effect within the access-token lifetime (15 min).
func RequireRole(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if middleware.UserID(r.Context()) == "" {
				middleware.WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if rank[Role(middleware.Role(r.Context()))] < rank[min] {
				middleware.WriteError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission guards a route by permission rather than role name.
func RequirePermission(p Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if middleware.UserID(r.Context()) == "" {
				middleware.WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !Can(middleware.Role(r.Context()), p) {
				middleware.WriteError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
