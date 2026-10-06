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
	"os"
	"path/filepath"
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

// Mock 是本地开发与测试用的实现。预签名地址指向 API 进程的 /dev/oss/ 上传入口（只在非生产环境注册）。
// 默认存在内存里；设置了 dir（NewDirMock）时存到本机目录，让 API 与 Worker 两个进程看到同一批对象。
type Mock struct {
	mu      sync.Mutex
	objects map[string][]byte
	dir     string
	// BaseURL 是预签名地址的前缀，如 http://192.168.1.10:8080（真机调试时填本机局域网地址）。
	BaseURL string
	secret  []byte
}

func NewMock() *Mock {
	return &Mock{objects: map[string][]byte{}, BaseURL: "http://localhost:8080", secret: []byte("mock-oss")}
}

// NewDirMock 返回把对象存到 dir 目录下的 Mock；本地 API 与 Worker 分两个进程运行时用它共享上传的文件。
func NewDirMock(dir string) (*Mock, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建本地 OSS 目录：%w", err)
	}
	m := NewMock()
	m.dir = dir
	return m, nil
}

// path 返回对象在本地目录里的路径；拒绝跳出目录的键。
func (m *Mock) path(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	if clean == "/" || clean != "/"+key {
		return "", fmt.Errorf("非法对象键：%q", key)
	}
	return filepath.Join(m.dir, filepath.FromSlash(clean)), nil
}

func (m *Mock) read(key string) ([]byte, bool, error) {
	if m.dir == "" {
		b, ok := m.objects[key]
		return b, ok, nil
	}
	p, err := m.path(key)
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(p) // #nosec G304 -- p 由 path 校验过，不会跳出 m.dir
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return b, err == nil, err
}

func (m *Mock) write(key string, data []byte) error {
	if m.dir == "" {
		m.objects[key] = data
		return nil
	}
	p, err := m.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	// 先写临时文件再改名，另一个进程不会读到写了一半的对象。
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (m *Mock) remove(key string) error {
	if m.dir == "" {
		delete(m.objects, key)
		return nil
	}
	p, err := m.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// keys 返回全部对象键，已排序。
func (m *Mock) keys() ([]string, error) {
	var keys []string
	if m.dir == "" {
		for k := range m.objects {
			keys = append(keys, k)
		}
	} else {
		err := filepath.WalkDir(m.dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasSuffix(p, ".tmp") {
				return err
			}
			rel, err := filepath.Rel(m.dir, p)
			if err != nil {
				return err
			}
			keys = append(keys, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// Seed 写入一个对象（测试用）。
func (m *Mock) Seed(key string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.write(key, data)
}

// Keys 返回全部对象键（测试用）。
func (m *Mock) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys, _ := m.keys()
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
	b, ok, err := m.read(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Exists: ok, Size: int64(len(b))}, nil
}

func (m *Mock) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok, err := m.read(key)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *Mock) Put(ctx context.Context, key string, data []byte, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.write(key, append([]byte(nil), data...))
}

func (m *Mock) Delete(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		if err := m.remove(k); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mock) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	all, err := m.keys()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, k := range all {
		if strings.HasPrefix(k, prefix) {
			if err := m.remove(k); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}
