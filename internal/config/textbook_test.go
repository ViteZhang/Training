package config

import "testing"

func TestTextbookEmbeddingConfig(t *testing.T) {
	get := func(k, d string) string { return d }
	c := loadTextbook(get)
	if c.EmbeddingProvider != "disabled" || c.EmbeddingDimensions != 1024 || c.Validate(EnvLocal) != nil {
		t.Fatal("默认配置应关闭")
	}
	for _, dim := range []int{0, 128, 2048} {
		bad := c
		bad.EmbeddingDimensions = dim
		if bad.Validate(EnvLocal) == nil {
			t.Fatal(dim)
		}
	}
	c.EmbeddingProvider = "openai"
	if c.Validate(EnvLocal) == nil {
		t.Fatal("缺密钥应失败")
	}
	c.EmbeddingAPIKey = "test"
	if c.Validate(EnvLocal) != nil {
		t.Fatal("合法配置被拒")
	}
	c.EmbeddingProvider = "mock"
	if c.Validate(EnvProduction) == nil {
		t.Fatal("生产 mock 应失败")
	}
}
