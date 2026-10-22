package moderation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	green "github.com/alibabacloud-go/green-20220302/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

type fakeGreen struct {
	texts  []string
	images []string
	level  string
}

func (f *fakeGreen) TextModerationPlusWithContext(_ context.Context, req *green.TextModerationPlusRequest, _ *dara.RuntimeOptions) (*green.TextModerationPlusResponse, error) {
	var p map[string]string
	_ = json.Unmarshal([]byte(dara.StringValue(req.ServiceParameters)), &p)
	f.texts = append(f.texts, p["content"])
	level := "none"
	if strings.Contains(p["content"], "违规") {
		level = f.level
	}
	return &green.TextModerationPlusResponse{Body: &green.TextModerationPlusResponseBody{Code: dara.Int32(200), Data: &green.TextModerationPlusResponseBodyData{
		RiskLevel: dara.String(level),
		Result:    []*green.TextModerationPlusResponseBodyDataResult{{Label: dara.String("contraband"), Description: dara.String("违禁内容")}},
	}}}, nil
}

func (f *fakeGreen) ImageModerationWithContext(_ context.Context, req *green.ImageModerationRequest, _ *dara.RuntimeOptions) (*green.ImageModerationResponse, error) {
	f.images = append(f.images, dara.StringValue(req.ServiceParameters))
	return &green.ImageModerationResponse{Body: &green.ImageModerationResponseBody{Code: dara.Int32(200), Data: &green.ImageModerationResponseBodyData{
		RiskLevel: dara.String("low"),
		Result:    []*green.ImageModerationResponseBodyDataResult{{Label: dara.String("nonLabel")}},
	}}}, nil
}

func newFake(t *testing.T, f *fakeGreen, level string) *Aliyun {
	t.Helper()
	a, err := newAliyunWithAPI(f, AliyunConfig{RejectLevel: level, SignURL: func(_ context.Context, key string, _ time.Duration) (string, error) {
		return "https://oss.test/" + key + "?sig=1", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAliyunText(t *testing.T) {
	f := &fakeGreen{level: "medium"}
	a := newFake(t, f, "")
	long := strings.Repeat("正常的专业课资料。\n", 400) // 4000 字，分段送审
	v, err := a.CheckText(context.Background(), long+"违规")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Pass {
		t.Error("默认只拦 high，medium 应放行")
	}
	if len(f.texts) < 3 {
		t.Errorf("长文本应分段：%d 段", len(f.texts))
	}
	for _, c := range f.texts {
		if utf8.RuneCountInString(c) > textChunk {
			t.Errorf("单段超过 %d 字", textChunk)
		}
	}
	if strings.Join(f.texts, "") != strings.TrimSpace(long+"违规") {
		t.Error("分段后拼回应与原文一致")
	}

	f = &fakeGreen{level: "high"}
	v, _ = newFake(t, f, "").CheckText(context.Background(), "违规")
	if v.Pass || v.Reason != "疑似违禁内容" {
		t.Errorf("high 应拦截并给原因：%+v", v)
	}
	if v, _ := newFake(t, &fakeGreen{}, "").CheckText(context.Background(), "  "); !v.Pass {
		t.Error("空文本应通过")
	}
}

func TestAliyunImage(t *testing.T) {
	f := &fakeGreen{}
	v, err := newFake(t, f, "low").CheckImage(context.Background(), "u/1/p/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if v.Pass || v.Reason == "" {
		t.Errorf("RejectLevel=low 时 low 应拦截：%+v", v)
	}
	if len(f.images) != 1 || !strings.Contains(f.images[0], `"imageUrl":"https://oss.test/u/1/p/a.jpg?sig=1"`) {
		t.Errorf("应把预签名地址交给审核服务：%v", f.images)
	}
}

func TestAliyunRejectLevel(t *testing.T) {
	if _, err := newAliyunWithAPI(&fakeGreen{}, AliyunConfig{RejectLevel: "none"}); err == nil {
		t.Error("none 不是合法的拦截等级")
	}
}

func TestSDKErrorDropsDetail(t *testing.T) {
	err := sdkError("文本审核", &tea.SDKError{Code: tea.String("InvalidAccessKeyId.NotFound"), Message: tea.String("Specified access key is not found."), Data: tea.String(`{"content":"考生原文"}`)})
	if got := err.Error(); got != "阿里云文本审核失败：code=InvalidAccessKeyId.NotFound message=Specified access key is not found." {
		t.Errorf("%q", got)
	}
}
