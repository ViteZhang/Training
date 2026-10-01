// Package oss 封装阿里云 OSS 私有桶：预签名直传、读取、删除（T08 补充上传相关方法）。
//
// 对象键约定：用户的全部文件都放在 u/{userID}/ 前缀下，注销到期时按前缀整体删除（PRD 6.12、16.2）；
// 资料原件放在 u/{userID}/b/{bankID}/ 下，删除专业课时按题库前缀删除。
package oss

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// UserPrefix 返回某用户全部对象的前缀。
func UserPrefix(userID uint64) string { return fmt.Sprintf("u/%d/", userID) }

// BankPrefix 返回某个题库资料原件的前缀；删除专业课时按它整体删除。
func BankPrefix(userID, bankID uint64) string { return fmt.Sprintf("u/%d/b/%d/", userID, bankID) }

// Store 是对象存储。
type Store interface {
	// DeletePrefix 删除某前缀下的全部对象，返回删除个数。
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// Mock 是内存实现，供本地开发与测试。
type Mock struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func NewMock() *Mock { return &Mock{objects: map[string][]byte{}} }

// Put 写入一个对象（测试用）。
func (m *Mock) Put(key string, data []byte) {
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
