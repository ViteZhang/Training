package http

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/auth"
	"peetraining-server/internal/logx"
)

func TestRenderLegalBody(t *testing.T) {
	got := string(renderLegalBody("第一段 **重点** <script>\n\n### 一、标题\n\n- 甲\n- 乙\n\n1. **委托**：一\n2. 二\n\n| 权限 | 用途 |\n| --- | --- |\n| 相机 | 拍照 |\n\n结尾"))
	for _, want := range []string{
		"<p>第一段 <strong>重点</strong> &lt;script&gt;</p>",
		"<h3>一、标题</h3>",
		"<ul>\n<li>甲</li>\n<li>乙</li>\n</ul>",
		"<ol>\n<li><strong>委托</strong>：一</li>\n<li>二</li>\n</ol>",
		"<tr><th>权限</th><th>用途</th></tr>\n<tr><td>相机</td><td>拍照</td></tr>",
		"<p>结尾</p>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q\n实际：\n%s", want, got)
		}
	}
	if strings.Contains(got, "---") {
		t.Errorf("表格分隔行不应输出：\n%s", got)
	}
}

func TestLegalPages(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	var logs bytes.Buffer
	r.Use(RequestID(logx.New(&logs, slog.LevelDebug)), Errors())
	registerLegalPages(r, func(_ context.Context, kind string) (auth.Agreement, error) {
		if kind != "privacy" {
			return auth.Agreement{}, apperr.NotFoundErr()
		}
		return auth.Agreement{
			Title: "隐私政策", Version: "1.0", Body: "由某公司运营。",
			// UTC 16:30 是北京时间次日，页面按北京时间显示。
			PublishedAt: sql.NullTime{Time: time.Date(2026, 10, 25, 16, 30, 0, 0, time.UTC), Valid: true},
			EffectiveAt: time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC),
		}, nil
	})

	w := do(r, http.MethodGet, "/legal/privacy", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("隐私政策页返回 %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	for _, want := range []string{"<title>隐私政策</title>", "版本 1.0", "更新日期 2026 年 10 月 26 日", "生效日期 2026 年 10 月 26 日", "<p>由某公司运营。</p>"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
	// 还没发布的协议、不对外的种类都是 404。
	for _, path := range []string{"/legal/user-agreement", "/legal/membership", "/legal/user"} {
		if w := do(r, http.MethodGet, path, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s 返回 %d", path, w.Code)
		}
	}
}
