package oss

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewAliyunConfig(t *testing.T) {
	if _, err := NewAliyun(AliyunConfig{}); err == nil || !strings.Contains(err.Error(), "OSS_BUCKET") {
		t.Fatalf("缺配置应报错：%v", err)
	}
	if _, err := NewAliyun(AliyunConfig{Endpoint: "oss.example.test", Bucket: "training-prod", AccessKeyID: "id", AccessKeySecret: "s"}); err == nil {
		t.Fatal("推断不出地域时应要求 OSS_REGION")
	}
}

func TestAliyunPresign(t *testing.T) {
	// 只填了内网地址：预签名地址必须是公网的，App 才能直传。
	a, err := NewAliyun(AliyunConfig{Endpoint: "oss-cn-beijing-internal.aliyuncs.com", Bucket: "training-prod", AccessKeyID: "id", AccessKeySecret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	put, err := a.PresignPut(ctx, "u/1/b/2/m/3.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", 10, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(put.URL, "https://training-prod.oss-cn-beijing.aliyuncs.com/u/1/b/2/m/3.docx?") {
		t.Errorf("上传地址应为公网地址：%s", put.URL)
	}
	if put.Headers["Content-Type"] == "" || put.ExpiresAt.Before(time.Now().Add(29*time.Minute)) {
		t.Errorf("应返回签名用到的请求头与过期时间：%+v", put)
	}
	get, err := a.PresignGet(ctx, "u/1/e/1.pdf", "政治学原理.pdf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(get.URL)
	if d := u.Query().Get("response-content-disposition"); d != "attachment; filename*=UTF-8''"+url.PathEscape("政治学原理.pdf") {
		t.Errorf("下载文件名：%q", d)
	}
	if a.internal == a.public {
		t.Error("服务端读写应走内网地址")
	}
	if _, err := a.DeletePrefix(ctx, ""); err == nil {
		t.Error("空前缀不能删除")
	}
}
