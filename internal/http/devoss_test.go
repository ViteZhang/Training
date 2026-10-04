package http

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/logx"
)

func TestDevOSSUpload(t *testing.T) {
	m := oss.NewMock()
	m.BaseURL = "http://example.test"
	r, err := NewRouter(Deps{Logger: logx.New(&bytes.Buffer{}, slog.LevelInfo), DevOSS: m, Tokens: fakeTokens{}})
	if err != nil {
		t.Fatal(err)
	}
	key := "u/1/b/2/m/3.pdf"
	p, err := m.PresignPut(context.Background(), key, "application/pdf", 4, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(p.URL)

	put := func(target string) int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, target, bytes.NewReader([]byte("data"))))
		return w.Code
	}
	if code := put(u.RequestURI()); code != http.StatusOK {
		t.Fatalf("签名正确应上传成功：%d", code)
	}
	if info, _ := m.Head(context.Background(), key); !info.Exists || info.Size != 4 {
		t.Fatalf("对象：%+v", info)
	}
	if code := put("/dev/oss/u/9/b/2/m/3.pdf?" + u.RawQuery); code != http.StatusForbidden {
		t.Fatalf("换了对象键签名就不对：%d", code)
	}

	// 没有 DevOSS（生产）时不注册这个入口
	prod, _ := newTestRouter(t, fakePinger{}, fakePinger{})
	w := httptest.NewRecorder()
	prod.ServeHTTP(w, httptest.NewRequest(http.MethodPut, u.RequestURI(), nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("生产不应有 /dev/oss：%d", w.Code)
	}
}
