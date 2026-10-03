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
