// Package vector 只存放官方教材片段的向量与定位标识，不存正文及用户内容。
package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Scope struct{ Book, Edition, Revision, Generation string }

func (s Scope) payload() map[string]string {
	return map[string]string{"book": s.Book, "edition": s.Edition, "revision": s.Revision, "generation": s.Generation}
}
func (s Scope) filter() any {
	must := []any{}
	for _, key := range []string{"book", "edition", "revision", "generation"} {
		must = append(must, map[string]any{"key": key, "match": map[string]string{"value": s.payload()[key]}})
	}
	return map[string]any{"must": must}
}

type Point struct {
	ID      uint64            `json:"id"`
	Vector  []float64         `json:"vector"`
	Payload map[string]string `json:"payload"`
}
type Hit struct {
	ID      uint64            `json:"id"`
	Score   float64           `json:"score"`
	Payload map[string]string `json:"payload"`
}
type Store interface {
	Ensure(context.Context, string, int) error
	Upsert(context.Context, string, Scope, uint64, []float64) error
	Verify(context.Context, string, int, Scope, int) error
	Search(context.Context, string, Scope, []float64, int) ([]Hit, error)
}
type Qdrant struct {
	base, key string
	http      *http.Client
}

func New(base, key string) *Qdrant {
	return &Qdrant{strings.TrimRight(base, "/"), key, &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

var collectionRE = regexp.MustCompile(`^tb_[a-f0-9]{16}_[0-9]+$`)

func (q *Qdrant) call(ctx context.Context, method, path string, input, output any) error {
	if q.base == "" {
		return errors.New("vector: 未配置 Qdrant")
	}
	var data []byte
	var err error
	if input != nil {
		data, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, q.base+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("vector: 地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	if q.key != "" {
		req.Header.Set("api-key", q.key)
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return errors.New("vector: 请求失败或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("vector: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return errors.New("vector: 响应无法读取或超限")
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Status json.RawMessage `json:"status"`
	}
	if json.Unmarshal(raw, &envelope) != nil || string(envelope.Status) != `"ok"` {
		return errors.New("vector: 响应状态错误")
	}
	if output != nil && json.Unmarshal(envelope.Result, output) != nil {
		return errors.New("vector: 响应格式错误")
	}
	return nil
}
func collectionPath(name string) (string, error) {
	if !collectionRE.MatchString(name) {
		return "", errors.New("vector: 集合名无效")
	}
	return "/collections/" + name, nil
}
func (q *Qdrant) Ensure(ctx context.Context, name string, dim int) error {
	path, err := collectionPath(name)
	if err != nil {
		return err
	}
	// PUT 已存在时 Qdrant 返回冲突；随后读取并严格验证配置，允许进程重启继续原代次。
	err = q.call(ctx, http.MethodPut, path, map[string]any{"vectors": map[string]any{"size": dim, "distance": "Cosine"}}, nil)
	if err != nil {
		return q.verifyConfig(ctx, path, dim)
	}
	return q.verifyConfig(ctx, path, dim)
}
func (q *Qdrant) verifyConfig(ctx context.Context, path string, dim int) error {
	var out struct {
		Status string `json:"status"`
		Config struct {
			Params struct {
				Vectors struct {
					Size     int    `json:"size"`
					Distance string `json:"distance"`
				} `json:"vectors"`
			} `json:"params"`
		} `json:"config"`
	}
	if err := q.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return err
	}
	if out.Status != "green" || out.Config.Params.Vectors.Size != dim || out.Config.Params.Vectors.Distance != "Cosine" {
		return errors.New("vector: 集合未就绪或维度不匹配")
	}
	return nil
}
func (q *Qdrant) Upsert(ctx context.Context, name string, scope Scope, id uint64, v []float64) error {
	path, err := collectionPath(name)
	if err != nil {
		return err
	}
	var out struct {
		Status string `json:"status"`
	}
	if err = q.call(ctx, http.MethodPut, path+"/points?wait=true", map[string]any{"points": []Point{{id, v, scope.payload()}}}, &out); err != nil {
		return err
	}
	if out.Status != "completed" {
		return errors.New("vector: 写入尚未完成")
	}
	return nil
}
func (q *Qdrant) Verify(ctx context.Context, name string, dim int, scope Scope, count int) error {
	path, err := collectionPath(name)
	if err != nil {
		return err
	}
	if err = q.verifyConfig(ctx, path, dim); err != nil {
		return err
	}
	var out struct {
		Count int `json:"count"`
	}
	if err = q.call(ctx, http.MethodPost, path+"/points/count", map[string]any{"filter": scope.filter(), "exact": true}, &out); err != nil {
		return err
	}
	if count < 1 || out.Count != count {
		return errors.New("vector: 向量数量不完整")
	}
	return nil
}
func (q *Qdrant) Search(ctx context.Context, name string, scope Scope, v []float64, limit int) ([]Hit, error) {
	path, err := collectionPath(name)
	if err != nil {
		return nil, err
	}
	var out []Hit
	if err = q.call(ctx, http.MethodPost, path+"/points/search", map[string]any{"vector": v, "filter": scope.filter(), "limit": limit, "with_payload": true}, &out); err != nil {
		return nil, err
	}
	for _, h := range out {
		for key, value := range scope.payload() {
			if h.Payload[key] != value {
				return nil, errors.New("vector: 检索范围不一致")
			}
		}
	}
	return out, nil
}
