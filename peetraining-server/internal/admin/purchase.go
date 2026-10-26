package admin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/payment"
)

// ---------- 7.8 购买入口（PRD v3 3.4、6.5a、13.3） ----------

// PurchaseEntry 是后台看到的购买入口配置；QRURL 是二维码预览地址（短时效）。
type PurchaseEntry struct {
	payment.PurchaseEntry
	QRURL   string
	Version int
}

const purchaseEntryKey = "purchase_entry"

// maxQRBytes 是二维码图片大小上限。
const maxQRBytes = 2 << 20

func (s *Service) PurchaseEntry(ctx context.Context) (PurchaseEntry, error) {
	r, err := s.q.GetRuleParam(ctx, purchaseEntryKey)
	if err != nil {
		return PurchaseEntry{}, err
	}
	var out PurchaseEntry
	if err := json.Unmarshal(r.Value, &out.PurchaseEntry); err != nil {
		return PurchaseEntry{}, err
	}
	out.Version = int(r.Version)
	if out.WeCom.QRKey != "" && s.d.OSS != nil {
		p, err := s.d.OSS.PresignGet(ctx, out.WeCom.QRKey, "kefu-qrcode.png", 30*time.Minute)
		if err != nil {
			return PurchaseEntry{}, err
		}
		out.QRURL = p.URL
	}
	return out, nil
}

// UpdatePurchaseEntry 修改开关与客服信息（二维码另传，这里保留原来的）。改动立即生效：走 UpdateParam，会让缓存失效。
func (s *Service) UpdatePurchaseEntry(ctx context.Context, adminID uint64, in payment.PurchaseEntry, version int) (PurchaseEntry, error) {
	cur, err := s.PurchaseEntry(ctx)
	if err != nil {
		return PurchaseEntry{}, err
	}
	in.WeCom.QRKey = cur.WeCom.QRKey
	in.WeCom.Name = strings.TrimSpace(in.WeCom.Name)
	in.WeCom.WeComID = strings.TrimSpace(in.WeCom.WeComID)
	if (in.Android || in.IOS) && (in.WeCom.Name == "" || in.WeCom.WeComID == "") {
		return PurchaseEntry{}, apperr.New(apperr.BadRequest, "打开购买入口前先填好客服企业微信名称和 ID")
	}
	return s.savePurchaseEntry(ctx, adminID, in, version)
}

// UploadWeComQR 上传客服二维码到 OSS（按内容哈希命名，换图即换地址），并写进配置。
func (s *Service) UploadWeComQR(ctx context.Context, adminID uint64, contentType, dataBase64 string) (PurchaseEntry, error) {
	if contentType != "image/png" && contentType != "image/jpeg" {
		return PurchaseEntry{}, apperr.New(apperr.BadRequest, "二维码只支持 PNG 或 JPEG")
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil || len(data) == 0 {
		return PurchaseEntry{}, apperr.New(apperr.BadRequest, "图片内容不对，请重新选择")
	}
	if len(data) > maxQRBytes {
		return PurchaseEntry{}, apperr.New(apperr.BadRequest, "图片不能超过 2 MB")
	}
	if got := http.DetectContentType(data); got != contentType {
		return PurchaseEntry{}, apperr.New(apperr.BadRequest, "图片格式与声明的不一致")
	}
	sum := sha256.Sum256(data)
	ext := ".png"
	if contentType == "image/jpeg" {
		ext = ".jpg"
	}
	key := "config/wecom-qr/" + hex.EncodeToString(sum[:8]) + ext
	if err := s.d.OSS.Put(ctx, key, data, contentType); err != nil {
		return PurchaseEntry{}, err
	}
	cur, err := s.PurchaseEntry(ctx)
	if err != nil {
		return PurchaseEntry{}, err
	}
	e := cur.PurchaseEntry
	e.WeCom.QRKey = key
	return s.savePurchaseEntry(ctx, adminID, e, cur.Version)
}

func (s *Service) savePurchaseEntry(ctx context.Context, adminID uint64, e payment.PurchaseEntry, version int) (PurchaseEntry, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return PurchaseEntry{}, err
	}
	if _, err := s.UpdateParam(ctx, adminID, purchaseEntryKey, raw, version); err != nil {
		return PurchaseEntry{}, err
	}
	return s.PurchaseEntry(ctx)
}
