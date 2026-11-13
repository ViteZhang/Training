package http

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/auth"
)

// 协议公开网页：应用商店上架、App 备案要填一个浏览器能直接打开的隐私政策链接。
// 页面读后台 7.8 已发布的当前版本，与 App 0.4 页看到的是同一份正文，后台发新版本后网页跟着变。
//
//	GET /legal/privacy          隐私政策
//	GET /legal/user-agreement   用户服务协议
var legalPages = map[string]string{
	"privacy":        "privacy",
	"user-agreement": "user",
}

type agreementGetter func(ctx context.Context, kind string) (auth.Agreement, error)

func registerLegalPages(r *gin.Engine, get agreementGetter) {
	r.GET("/legal/:page", func(c *gin.Context) {
		kind, ok := legalPages[c.Param("page")]
		if !ok {
			_ = c.Error(ErrNotFound())
			return
		}
		a, err := get(c.Request.Context(), kind)
		if err != nil {
			_ = c.Error(err)
			return
		}
		var buf bytes.Buffer
		if err := legalTmpl.Execute(&buf, legalView{
			Title: a.Title, Version: a.Version,
			UpdatedAt:   legalDate(a.PublishedAt.Time),
			EffectiveAt: legalDate(a.EffectiveAt),
			Body:        renderLegalBody(a.Body),
		}); err != nil {
			_ = c.Error(err)
			return
		}
		// 协议改版不频繁，但发布后应尽快生效，只让浏览器和 Nginx 短暂缓存。
		c.Header("Cache-Control", "public, max-age=300")
		c.Data(http.StatusOK, "text/html; charset=utf-8", buf.Bytes())
	})
}

type legalView struct {
	Title, Version, UpdatedAt, EffectiveAt string
	Body                                   template.HTML
}

// 页面上的日期按北京时间显示。
var legalZone = time.FixedZone("Asia/Shanghai", 8*3600)

func legalDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(legalZone).Format("2006 年 1 月 2 日")
}

var legalTmpl = template.Must(template.New("legal").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body{margin:0;background:#FFFFFF;color:#1A1A1A;font:15px/1.8 -apple-system,BlinkMacSystemFont,"PingFang SC","Noto Sans SC","Microsoft YaHei",sans-serif;-webkit-text-size-adjust:100%}
main{max-width:720px;margin:0 auto;padding:32px 22px 64px}
h1{font-size:26px;line-height:1.3;margin:0 0 8px}
.meta{font-size:13px;color:#6E6E6E;margin:0 0 28px}
h3{font-size:17px;margin:32px 0 8px}
p{margin:0 0 14px}
ul,ol{margin:0 0 14px;padding-left:22px}
li{margin-bottom:6px}
.tbl{overflow-x:auto;margin:0 0 14px}
table{border-collapse:collapse;width:100%;font-size:13px;line-height:1.6}
th,td{border:1px solid #E6E6E6;padding:8px 10px;text-align:left;vertical-align:top}
th{background:#F7F7F5;font-weight:600;white-space:nowrap}
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p class="meta">版本 {{.Version}}{{if .UpdatedAt}} · 更新日期 {{.UpdatedAt}}{{end}}{{if .EffectiveAt}} · 生效日期 {{.EffectiveAt}}{{end}}</p>
{{.Body}}
</main>
</body>
</html>
`))

var (
	legalOrdered = regexp.MustCompile(`^\d+\.\s+`)
	legalBold    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	legalRule    = regexp.MustCompile(`^\|[\s:|-]+\|$`)
)

// renderLegalBody 把协议正文转成 HTML。只支持 App 0.4 页支持的写法（docs/legal/README.md）：
// 「### 标题」、**加粗**、「1. 」「- 」列表、表格、空行分段；其余一律按纯文本转义输出。
func renderLegalBody(body string) template.HTML {
	var b strings.Builder
	list := "" // 正在输出的列表标签：ul / ol
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	item := func(tag, text string) {
		if list != tag {
			closeList()
			b.WriteString("<" + tag + ">\n")
			list = tag
		}
		b.WriteString("<li>" + legalInline(text) + "</li>\n")
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
			// 列表项之间允许空行，遇到别的内容再收尾。
		case strings.HasPrefix(line, "### "):
			closeList()
			b.WriteString("<h3>" + legalInline(strings.TrimPrefix(line, "### ")) + "</h3>\n")
		case strings.HasPrefix(line, "- "):
			item("ul", strings.TrimPrefix(line, "- "))
		case legalOrdered.MatchString(line):
			item("ol", legalOrdered.ReplaceAllString(line, ""))
		case strings.HasPrefix(line, "|"):
			closeList()
			j := i
			for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
				j++
			}
			writeLegalTable(&b, lines[i:j])
			i = j - 1
		default:
			closeList()
			b.WriteString("<p>" + legalInline(line) + "</p>\n")
		}
	}
	closeList()
	return template.HTML(b.String()) //nolint:gosec // 文本已逐段转义，只加了固定标签
}

func writeLegalTable(b *strings.Builder, rows []string) {
	b.WriteString("<div class=\"tbl\"><table>\n")
	head := true
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if legalRule.MatchString(row) {
			continue
		}
		tag := "td"
		if head {
			tag = "th"
		}
		b.WriteString("<tr>")
		for _, cell := range strings.Split(strings.Trim(row, "|"), "|") {
			b.WriteString("<" + tag + ">" + legalInline(strings.TrimSpace(cell)) + "</" + tag + ">")
		}
		b.WriteString("</tr>\n")
		head = false
	}
	b.WriteString("</table></div>\n")
}

// legalInline 先转义再处理加粗，正文里的尖括号不会变成标签。
func legalInline(s string) string {
	return legalBold.ReplaceAllString(template.HTMLEscapeString(s), "<strong>$1</strong>")
}
