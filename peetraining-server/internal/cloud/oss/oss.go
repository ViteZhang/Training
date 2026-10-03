// Package oss 封装阿里云 OSS 私有桶：客户端预签名直传、服务端读写与删除。
//
// 对象键约定：用户的全部文件都放在 u/{userID}/ 前缀下，注销到期时按前缀整体删除（PRD 6.12、16.2）；
// 资料原件放在 u/{userID}/b/{bankID}/ 下，删除专业课时按题库前缀删除。
package oss

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// UserPrefix 返回某用户全部对象的前缀。
func UserPrefix(userID uint64) string { return fmt.Sprintf("u/%d/", userID) }

// BankPrefix 返回某个题库资料原件的前缀；删除专业课时按它整体删除。
func BankPrefix(userID, bankID uint64) string { return fmt.Sprintf("u/%d/b/%d/", userID, bankID) }

// MaterialKey 返回资料原件的对象键。
func MaterialKey(userID, bankID, materialID uint64, ext string) string {
	return fmt.Sprintf("%sm/%d.%s", BankPrefix(userID, bankID), materialID, ext)
}

// Presigned 是客户端直传用的预签名请求。
type Presigned struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// ObjectInfo 是对象元信息。
type ObjectInfo struct {
	Exists bool
	Size   int64
}

// ErrNotFound 表示对象不存在。
var ErrNotFound = errors.New("对象不存在")

// Store 是对象存储。
type Store interface {
	// PresignPut 生成客户端直传的预签名 PUT 地址；客户端必须带上返回的请求头。
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (Presigned, error)
	// PresignGet 生成短时效的下载地址；fileName 是保存到手机时的文件名（Content-Disposition）。
	PresignGet(ctx context.Context, key, fileName string, ttl time.Duration) (Presigned, error)
	Head(ctx context.Context, key string) (ObjectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Delete(ctx context.Context, keys ...string) error
	// DeletePrefix 删除某前缀下的全部对象，返回删除个数。
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// Mock 是内存实现，供本地开发与测试。预签名地址指向 API 进程的 /dev/oss/ 上传入口（只在非生产环境注册）。
type Mock struct {
	mu      sync.Mutex
	objects map[string][]byte
	// BaseURL 是预签名地址的前缀，如 http://192.168.1.10:8080（真机调试时填本机局域网地址）。
	BaseURL string
	secret  []byte
}

func NewMock() *Mock {
	return &Mock{objects: map[string][]byte{}, BaseURL: "http://localhost:8080", secret: []byte("mock-oss")}
}

// Seed 写入一个对象（测试用）。
func (m *Mock) Seed(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = data
}

// Keys 返回全部对象键（测试用）。
func (m *Mock) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objects))
	for k := range m.objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (m *Mock) sign(key string, exp int64) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(key + "|" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *Mock) PresignPut(ctx context.Context, key, contentType string, _ int64, ttl time.Duration) (Presigned, error) {
	if err := ctx.Err(); err != nil {
		return Presigned{}, err
	}
	exp := time.Now().Add(ttl)
	q := url.Values{"exp": {strconv.FormatInt(exp.Unix(), 10)}, "sig": {m.sign(key, exp.Unix())}}
	return Presigned{
		URL:       strings.TrimRight(m.BaseURL, "/") + "/dev/oss/" + key + "?" + q.Encode(),
		Headers:   map[string]string{"Content-Type": contentType},
		ExpiresAt: exp,
	}, nil
}

func (m *Mock) PresignGet(ctx context.Context, key, fileName string, ttl time.Duration) (Presigned, error) {
	if err := ctx.Err(); err != nil {
		return Presigned{}, err
	}
	exp := time.Now().Add(ttl)
	q := url.Values{"exp": {strconv.FormatInt(exp.Unix(), 10)}, "sig": {m.sign(key, exp.Unix())}, "name": {fileName}}
	return Presigned{URL: strings.TrimRight(m.BaseURL, "/") + "/dev/oss/" + key + "?" + q.Encode(), Headers: map[string]string{}, ExpiresAt: exp}, nil
}

// VerifyUpload 校验 /dev/oss/ 上传请求的签名（本地开发用）。
func (m *Mock) VerifyUpload(key, exp, sig string) bool {
	e, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > e {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(m.sign(key, e)))
}

func (m *Mock) Head(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	return ObjectInfo{Exists: ok, Size: int64(len(b))}, nil
}

func (m *Mock) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *Mock) Put(ctx context.Context, key string, data []byte, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.Seed(key, append([]byte(nil), data...))
	return nil
}

func (m *Mock) Delete(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.objects, k)
	}
	return nil
}

func (m *Mock) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			delete(m.objects, k)
			n++
		}
	}
	return n, nil
}
