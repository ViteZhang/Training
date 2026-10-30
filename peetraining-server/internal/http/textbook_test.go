package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"peetraining-server/internal/admin"
)

type textbookAudit struct{ writes int }

func (a *textbookAudit) Authenticate(context.Context, string) (admin.Admin, error) {
	return admin.Admin{}, nil
}
func (a *textbookAudit) Audit(context.Context, uint64, string, string, string, map[string]any, string) error {
	a.writes++
	return nil
}
func TestTextbookAdminRoleEnforcement(t *testing.T) {
	roles, err := AdminRoles()
	if err != nil {
		t.Fatal(err)
	}
	for key, allowed := range roles {
		if !strings.Contains(key, "/admin/textbooks") {
			continue
		}
		if len(allowed) != 2 || allowed[0] != "admin" || allowed[1] != "content_lead" {
			t.Fatalf("教材接口权限: %s %v", key, allowed)
		}
	}
	for _, role := range []admin.Role{"admin", "content_lead", "support", "analyst", "content_editor", ""} {
		t.Run(string(role), func(t *testing.T) {
			r := gin.New()
			audit := &textbookAudit{}
			r.Use(Errors())
			r.Use(func(c *gin.Context) {
				if role != "" {
					c.Set(ctxAdmin, admin.Admin{ID: 1, Roles: []admin.Role{role}})
				}
				c.Next()
			})
			r.Use(AdminGate(roles, audit))
			r.POST(APIPrefix+"/admin/textbooks", func(c *gin.Context) { c.Status(http.StatusOK) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, APIPrefix+"/admin/textbooks", nil))
			want := http.StatusForbidden
			if role == "" {
				want = http.StatusUnauthorized
			}
			if role == "admin" || role == "content_lead" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if want == http.StatusOK && audit.writes != 1 {
				t.Fatal("缺少操作审计")
			}
			if want != http.StatusOK && audit.writes != 0 {
				t.Fatal("拒绝操作不应记录成功审计")
			}
		})
	}
}
