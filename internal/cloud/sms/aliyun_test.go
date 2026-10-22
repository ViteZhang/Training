package sms

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v4/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

type fakeAPI struct {
	req     *dysmsapi.SendSmsRequest
	runtime *util.RuntimeOptions
	resp    *dysmsapi.SendSmsResponse
	err     error
}

func (f *fakeAPI) SendSmsWithOptions(req *dysmsapi.SendSmsRequest, rt *util.RuntimeOptions) (*dysmsapi.SendSmsResponse, error) {
	f.req, f.runtime = req, rt
	return f.resp, f.err
}

func okResp(code, msg string) *dysmsapi.SendSmsResponse {
	return &dysmsapi.SendSmsResponse{Body: &dysmsapi.SendSmsResponseBody{Code: tea.String(code), Message: tea.String(msg), RequestId: tea.String("req-1")}}
}

func TestNewAliyunValidates(t *testing.T) {
	_, err := NewAliyun(AliyunConfig{})
	for _, key := range []string{"ALIYUN_ACCESS_KEY_ID", "ALIYUN_ACCESS_KEY_SECRET", "SMS_SIGN_NAME", "SMS_TEMPLATE_CODE"} {
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("错误信息应提到 %s：%v", key, err)
		}
	}
	a, err := NewAliyun(AliyunConfig{AccessKeyID: "id", AccessKeySecret: "s", SignName: "签名", TemplateCode: "SMS_1"})
	if err != nil || a.templateParam != DefaultTemplateParam {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestAliyunSendCode(t *testing.T) {
	f := &fakeAPI{resp: okResp("OK", "OK")}
	a := newAliyunWithAPI(f, AliyunConfig{SignName: "考研Training", TemplateCode: "SMS_123"})
	if err := a.SendCode(context.Background(), "13812345678", "123456"); err != nil {
		t.Fatal(err)
	}
	if tea.StringValue(f.req.PhoneNumbers) != "13812345678" || tea.StringValue(f.req.SignName) != "考研Training" || tea.StringValue(f.req.TemplateCode) != "SMS_123" {
		t.Errorf("请求参数不对：%+v", f.req)
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(tea.StringValue(f.req.TemplateParam)), &p); err != nil || p["code"] != "123456" || len(p) != 1 {
		t.Errorf("TemplateParam = %q", tea.StringValue(f.req.TemplateParam))
	}
	if tea.BoolValue(f.runtime.Autoretry) {
		t.Error("不应自动重试，避免重复发送")
	}

	// 自定义模板变量名。
	a = newAliyunWithAPI(f, AliyunConfig{SignName: "s", TemplateCode: "t", TemplateParam: "verify_code"})
	_ = a.SendCode(context.Background(), "13812345678", "654321")
	if tea.StringValue(f.req.TemplateParam) != `{"verify_code":"654321"}` {
		t.Errorf("TemplateParam = %q", tea.StringValue(f.req.TemplateParam))
	}
}

func TestAliyunSendCodeErrors(t *testing.T) {
	a := newAliyunWithAPI(&fakeAPI{resp: okResp("isv.BUSINESS_LIMIT_CONTROL", "触发分钟级流控")}, AliyunConfig{SignName: "s", TemplateCode: "t"})
	err := a.SendCode(context.Background(), "13812345678", "123456")
	var ae *AliyunError
	if !errors.As(err, &ae) || ae.Code != "isv.BUSINESS_LIMIT_CONTROL" || ae.RequestID != "req-1" {
		t.Fatalf("业务错误应返回 AliyunError：%v", err)
	}

	a = newAliyunWithAPI(&fakeAPI{err: tea.NewSDKError(map[string]interface{}{"code": "InvalidAccessKeyId.NotFound", "message": "Specified access key is not found."})}, AliyunConfig{})
	err = a.SendCode(context.Background(), "13812345678", "123456")
	if !errors.As(err, &ae) || ae.Code != "InvalidAccessKeyId.NotFound" {
		t.Fatalf("SDK 错误应返回 AliyunError：%v", err)
	}

	// 网络错误里的地址带手机号，不能出现在错误信息里。
	netErr := &url.Error{Op: "Get", URL: "https://dysmsapi.aliyuncs.com/?PhoneNumbers=13812345678", Err: errors.New("i/o timeout")}
	a = newAliyunWithAPI(&fakeAPI{err: netErr}, AliyunConfig{})
	err = a.SendCode(context.Background(), "13812345678", "123456")
	if err == nil || strings.Contains(err.Error(), "13812345678") || !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("网络错误不应包含手机号：%v", err)
	}

	a = newAliyunWithAPI(&fakeAPI{}, AliyunConfig{})
	if err := a.SendCode(context.Background(), "13812345678", "123456"); err == nil {
		t.Fatal("空响应应报错")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeAPI{resp: okResp("OK", "OK")}
	if err := newAliyunWithAPI(f, AliyunConfig{}).SendCode(ctx, "13812345678", "1"); err == nil || f.req != nil {
		t.Fatal("已取消的请求不应发送")
	}
}
