package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// TextbookConfig 的凭证仅供 API/Worker 使用，不能返回到客户端。
type TextbookConfig struct {
	EmbeddingProvider   string
	EmbeddingBaseURL    string
	EmbeddingAPIKey     string
	EmbeddingModel      string
	EmbeddingDimensions int
	VectorURL           string
	VectorAPIKey        string
}

func loadTextbook(get func(string, string) string) TextbookConfig {
	dimensions, _ := strconv.Atoi(get("TEXTBOOK_EMBEDDING_DIMENSIONS", "1024"))
	return TextbookConfig{
		EmbeddingProvider:   get("TEXTBOOK_EMBEDDING_PROVIDER", ProviderDisabled),
		EmbeddingBaseURL:    get("TEXTBOOK_EMBEDDING_BASE_URL", "https://llm-fr3l8h9861g95r7x.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"),
		EmbeddingAPIKey:     get("TEXTBOOK_EMBEDDING_API_KEY", ""),
		EmbeddingModel:      get("TEXTBOOK_EMBEDDING_MODEL", "qwen3.7-text-embedding-flash"),
		EmbeddingDimensions: dimensions,
		VectorURL:           get("TEXTBOOK_VECTOR_URL", ""), VectorAPIKey: get("TEXTBOOK_VECTOR_API_KEY", ""),
	}
}
func (c TextbookConfig) Validate(env string) error {
	switch c.EmbeddingProvider {
	case ProviderDisabled:
	case ProviderMock:
		if env == EnvProduction {
			return errors.New("生产不能使用 TEXTBOOK_EMBEDDING_PROVIDER=mock")
		}
	case "openai":
		if c.EmbeddingAPIKey == "" || c.EmbeddingModel == "" {
			return errors.New("教材嵌入需要设置 TEXTBOOK_EMBEDDING_API_KEY 和 TEXTBOOK_EMBEDDING_MODEL")
		}
		u, err := url.Parse(c.EmbeddingBaseURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" {
			return errors.New("TEXTBOOK_EMBEDDING_BASE_URL 必须是无凭证和参数的 HTTPS 地址")
		}
	default:
		return errors.New("TEXTBOOK_EMBEDDING_PROVIDER 只能为 disabled、mock、openai")
	}
	switch c.EmbeddingDimensions {
	case 256, 512, 768, 1024:
	default:
		return fmt.Errorf("TEXTBOOK_EMBEDDING_DIMENSIONS 仅支持 256、512、768、1024")
	}
	if c.VectorURL != "" {
		u, err := url.Parse(c.VectorURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("TEXTBOOK_VECTOR_URL 必须是无凭证和参数的 HTTP(S) 地址")
		}
	}
	return nil
}
