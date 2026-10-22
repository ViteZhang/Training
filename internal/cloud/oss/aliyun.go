package oss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

// AliyunConfig 是阿里云 OSS 配置。
type AliyunConfig struct {
	// Region 如 cn-beijing；为空时从 Endpoint 推断（oss-cn-beijing.aliyuncs.com）。
	Region string
	// Endpoint 是公网地址，预签名地址用它，App 直传与下载都走这里，如 oss-cn-beijing.aliyuncs.com。
	Endpoint string
	// InternalEndpoint 是服务端读写用的内网地址（可空），如 oss-cn-beijing-internal.aliyuncs.com；
	// API 与 Worker 在同地域 ECS 上时填它，读原件不走公网流量。
	InternalEndpoint string
	Bucket           string
	AccessKeyID      string
	AccessKeySecret  string
}

// Aliyun 是阿里云 OSS 私有桶的实现：预签名用 V4 签名，服务端读写用 SDK。
type Aliyun struct {
	public   *alioss.Client // 生成给客户端的预签名地址
	internal *alioss.Client // 服务端读写；没配内网地址时与 public 相同
	bucket   string
}

var _ Store = (*Aliyun)(nil)

var regionInEndpoint = regexp.MustCompile(`oss-([a-z0-9-]+?)(-internal)?\.aliyuncs\.com`)

// NewAliyun 校验配置并创建客户端；缺配置在启动时就报错，而不是等到用户上传时才发现。
func NewAliyun(cfg AliyunConfig) (*Aliyun, error) {
	var missing []string
	if cfg.Endpoint == "" {
		missing = append(missing, "OSS_ENDPOINT")
	}
	if cfg.Bucket == "" {
		missing = append(missing, "OSS_BUCKET")
	}
	if cfg.AccessKeyID == "" || cfg.AccessKeySecret == "" {
		missing = append(missing, "OSS_ACCESS_KEY_ID / OSS_ACCESS_KEY_SECRET（或 ALIYUN_ACCESS_KEY_ID / SECRET）")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("OSS_PROVIDER=aliyun 缺少配置：%s", strings.Join(missing, "、"))
	}
	if strings.Contains(cfg.Endpoint, "-internal.") {
		// 填了内网地址：App 在公网上访问不到，预签名改用对应的公网地址，服务端读写仍走内网。
		if cfg.InternalEndpoint == "" {
			cfg.InternalEndpoint = cfg.Endpoint
		}
		cfg.Endpoint = strings.Replace(cfg.Endpoint, "-internal.", ".", 1)
	}
	if cfg.Region == "" {
		if m := regionInEndpoint.FindStringSubmatch(cfg.Endpoint); m != nil {
			cfg.Region = m[1]
		} else {
			return nil, errors.New("OSS_PROVIDER=aliyun 需要设置 OSS_REGION（如 cn-beijing），无法从 OSS_ENDPOINT 推断")
		}
	}
	newClient := func(endpoint string) *alioss.Client {
		c := alioss.LoadDefaultConfig().
			WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.AccessKeySecret)).
			WithRegion(cfg.Region).
			WithEndpoint(endpoint).
			WithConnectTimeout(5 * time.Second).
			WithReadWriteTimeout(60 * time.Second)
		return alioss.NewClient(c)
	}
	a := &Aliyun{public: newClient(cfg.Endpoint), bucket: cfg.Bucket}
	a.internal = a.public
	if cfg.InternalEndpoint != "" {
		a.internal = newClient(cfg.InternalEndpoint)
	}
	return a, nil
}

func (a *Aliyun) PresignPut(ctx context.Context, key, contentType string, _ int64, ttl time.Duration) (Presigned, error) {
	r, err := a.public.Presign(ctx, &alioss.PutObjectRequest{
		Bucket: alioss.Ptr(a.bucket), Key: alioss.Ptr(key), ContentType: alioss.Ptr(contentType),
	}, alioss.PresignExpires(ttl))
	if err != nil {
		return Presigned{}, fmt.Errorf("oss: 生成上传地址：%w", err)
	}
	h := map[string]string{"Content-Type": contentType}
	for k, v := range r.SignedHeaders {
		h[k] = v
	}
	return Presigned{URL: r.URL, Headers: h, ExpiresAt: r.Expiration}, nil
}

func (a *Aliyun) PresignGet(ctx context.Context, key, fileName string, ttl time.Duration) (Presigned, error) {
	req := &alioss.GetObjectRequest{Bucket: alioss.Ptr(a.bucket), Key: alioss.Ptr(key)}
	if fileName != "" {
		// 中文文件名按 RFC 5987 编码，手机保存时才不会乱码。
		req.ResponseContentDisposition = alioss.Ptr("attachment; filename*=UTF-8''" + url.PathEscape(fileName))
	}
	r, err := a.public.Presign(ctx, req, alioss.PresignExpires(ttl))
	if err != nil {
		return Presigned{}, fmt.Errorf("oss: 生成下载地址：%w", err)
	}
	h := map[string]string{}
	for k, v := range r.SignedHeaders {
		h[k] = v
	}
	return Presigned{URL: r.URL, Headers: h, ExpiresAt: r.Expiration}, nil
}

func isNotFound(err error) bool {
	var se *alioss.ServiceError
	return errors.As(err, &se) && se.StatusCode == http.StatusNotFound
}

func (a *Aliyun) Head(ctx context.Context, key string) (ObjectInfo, error) {
	r, err := a.internal.HeadObject(ctx, &alioss.HeadObjectRequest{Bucket: alioss.Ptr(a.bucket), Key: alioss.Ptr(key)})
	if isNotFound(err) {
		return ObjectInfo{}, nil
	}
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("oss: 查询对象：%w", err)
	}
	return ObjectInfo{Exists: true, Size: r.ContentLength}, nil
}

func (a *Aliyun) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := a.internal.GetObject(ctx, &alioss.GetObjectRequest{Bucket: alioss.Ptr(a.bucket), Key: alioss.Ptr(key)})
	if isNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("oss: 读取对象：%w", err)
	}
	return r.Body, nil
}

func (a *Aliyun) Put(ctx context.Context, key string, data []byte, contentType string) error {
	req := &alioss.PutObjectRequest{Bucket: alioss.Ptr(a.bucket), Key: alioss.Ptr(key), Body: bytes.NewReader(data)}
	if contentType != "" {
		req.ContentType = alioss.Ptr(contentType)
	}
	if _, err := a.internal.PutObject(ctx, req); err != nil {
		return fmt.Errorf("oss: 写入对象：%w", err)
	}
	return nil
}

// deleteBatch 是 DeleteMultipleObjects 单次最多的对象数。
const deleteBatch = 1000

func (a *Aliyun) Delete(ctx context.Context, keys ...string) error {
	for len(keys) > 0 {
		n := min(len(keys), deleteBatch)
		objs := make([]alioss.DeleteObject, n)
		for i, k := range keys[:n] {
			objs[i] = alioss.DeleteObject{Key: alioss.Ptr(k)}
		}
		if _, err := a.internal.DeleteMultipleObjects(ctx, &alioss.DeleteMultipleObjectsRequest{
			Bucket: alioss.Ptr(a.bucket), Objects: objs, Quiet: true,
		}); err != nil {
			return fmt.Errorf("oss: 删除对象：%w", err)
		}
		keys = keys[n:]
	}
	return nil
}

func (a *Aliyun) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if prefix == "" {
		// 防止误删整个桶。
		return 0, errors.New("oss: 前缀不能为空")
	}
	p := a.internal.NewListObjectsV2Paginator(&alioss.ListObjectsV2Request{
		Bucket: alioss.Ptr(a.bucket), Prefix: alioss.Ptr(prefix), MaxKeys: deleteBatch,
	})
	n := 0
	for p.HasNext() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return n, fmt.Errorf("oss: 列出对象：%w", err)
		}
		keys := make([]string, 0, len(page.Contents))
		for _, o := range page.Contents {
			keys = append(keys, alioss.ToString(o.Key))
		}
		if err := a.Delete(ctx, keys...); err != nil {
			return n, err
		}
		n += len(keys)
	}
	return n, nil
}
