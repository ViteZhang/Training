package ai

import (
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/config"
)

// Routing 按配置给出主用平台的模型和备用平台（API 与评测命令共用，保证两边一致）。
//
//   - AI_PROVIDER=bailian：百炼，模型 AI_MODEL_STRONG / AI_MODEL_CHEAP
//   - AI_PROVIDER=relay：中转接口为主，模型 AI_RELAY_MODEL_STRONG / AI_RELAY_MODEL_CHEAP；
//     配了百炼时作为备用，主用平台出错时改用百炼的模型
//
// AI_PREFER_STRONG 默认开启：便宜档能力也用高阶模型。
func Routing(cfg config.AIConfig, fallback cloudai.Client) (Models, *Fallback) {
	bailian := Models{Strong: cfg.ModelStrong, Cheap: cfg.ModelCheap, PreferStrong: cfg.PreferStrong}
	if cfg.Provider != "relay" {
		return bailian, nil
	}
	primary := Models{Strong: cfg.RelayModelStrong, Cheap: cfg.RelayModelCheap, PreferStrong: cfg.PreferStrong}
	if fallback == nil {
		return primary, nil
	}
	return primary, &Fallback{Client: fallback, Models: bailian}
}

// PricesFrom 把配置里的价格（元 / 百万 token）换成引擎用的单位（百万分之一元 / 千 token）。
func PricesFrom(cfg config.AIConfig) map[string]Price {
	out := make(map[string]Price, len(cfg.Prices))
	for m, p := range cfg.Prices {
		out[m] = Price{InputPer1K: int64(p[0]*1000 + 0.5), OutputPer1K: int64(p[1]*1000 + 0.5)}
	}
	return out
}
