package payment

import (
	"context"
	"time"

	"peetraining-server/internal/params"
)

// PurchaseEntry 是 rule_params.purchase_entry（PRD v3 3.4、6.5a、7.8）：
// 按平台控制 App 是否显示会员购买与兑换码入口，以及客服企业微信信息。
type PurchaseEntry struct {
	Android bool         `json:"android"`
	IOS     bool         `json:"ios"`
	WeCom   WeComContact `json:"wecom"`
}

// WeComContact 是 6.5a 获取兑换码弹层上的客服企业微信信息。QRKey 是二维码在 OSS 的对象键。
type WeComContact struct {
	Name      string `json:"name"`
	WeComID   string `json:"wecom_id"`
	QRKey     string `json:"qr_key"`
	ReplyTime string `json:"reply_time"`
	Note      string `json:"note"`
}

// EntryFor 返回某个平台的购买入口是否打开。web 与未知平台按关闭处理：只有 App 有购买入口。
func (e PurchaseEntry) EntryFor(platform string) bool {
	switch platform {
	case "android":
		return e.Android
	case "ios":
		return e.IOS
	}
	return false
}

// PurchaseEntryConfig 读配置；后台改了以后缓存立即失效（params.Store.Invalidate）。
func PurchaseEntryConfig(ctx context.Context, ps *params.Store) (PurchaseEntry, error) {
	var e PurchaseEntry
	err := ps.Get(ctx, "purchase_entry", &e)
	return e, err
}

// EntryView 是 App 看到的购买入口：关闭时不带客服信息，App 上不出现会员、价格与兑换码（4.9b）。
type EntryView struct {
	Enabled bool
	Contact *ContactView
}

// ContactView 是客服信息；QRURL 是二维码的短时效下载地址。
type ContactView struct {
	Name, WeComID, QRURL, ReplyTime, Note string
}

const qrURLTTL = 30 * time.Minute

// PurchaseEntry 返回某个平台的购买入口（6.1、6.5、6.5a、6.7、4.9 / 4.9b 都按它显示）。
func (s *Service) PurchaseEntry(ctx context.Context, platform string) (EntryView, error) {
	e, err := PurchaseEntryConfig(ctx, s.d.Params)
	if err != nil {
		return EntryView{}, err
	}
	if !e.EntryFor(platform) {
		return EntryView{}, nil
	}
	c := &ContactView{Name: e.WeCom.Name, WeComID: e.WeCom.WeComID, ReplyTime: e.WeCom.ReplyTime, Note: e.WeCom.Note}
	if e.WeCom.QRKey != "" && s.d.OSS != nil {
		p, err := s.d.OSS.PresignGet(ctx, e.WeCom.QRKey, "kefu-qrcode.png", qrURLTTL)
		if err != nil {
			return EntryView{}, err
		}
		c.QRURL = p.URL
	}
	return EntryView{Enabled: true, Contact: c}, nil
}
