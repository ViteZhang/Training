package http

import (
	"strings"
	"testing"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
)

// 每个后台接口都必须在契约里声明 x-roles（登录两步除外）；没声明的会被 AdminGate 一律拒绝。
func TestAdminRolesDeclared(t *testing.T) {
	roles, err := AdminRoles()
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := gen.GetSwagger()
	valid := map[admin.Role]bool{"*": true}
	for _, r := range admin.AllRoles {
		valid[r] = true
	}
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/admin/") || strings.HasPrefix(path, "/admin/auth/login") || strings.HasPrefix(path, "/admin/auth/verify") {
			continue
		}
		for method := range item.Operations() {
			key := method + " " + APIPrefix + strings.NewReplacer("{", ":", "}", "").Replace(path)
			rs, ok := roles[key]
			if !ok || len(rs) == 0 {
				t.Errorf("%s 没有声明 x-roles", key)
			}
			for _, r := range rs {
				if !valid[r] {
					t.Errorf("%s 的角色 %q 不存在", key, r)
				}
			}
			if sec := item.Operations()[method].Security; sec == nil || len(*sec) == 0 || (*sec)[0]["adminAuth"] == nil {
				t.Errorf("%s 必须用 adminAuth", key)
			}
		}
	}
}
