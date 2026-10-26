package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/payment"
)

// 购买入口与企业微信客服售卖兑换码（PRD v3 3.4、6.5a、7.4、7.8、13.3，10 月 26 日更新）。

func (h *Handlers) GetPurchaseEntry(c *gin.Context, p gen.GetPurchaseEntryParams) {
	v, err := h.deps.Payment.PurchaseEntry(c.Request.Context(), string(p.Platform))
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.PurchaseEntry{Enabled: v.Enabled}
	if v.Contact != nil {
		out.Contact = &gen.WeComContact{Name: v.Contact.Name, WecomId: v.Contact.WeComID, QrUrl: opt(v.Contact.QRURL), ReplyTime: v.Contact.ReplyTime,
			Note: v.Contact.Note}
	}
	c.JSON(http.StatusOK, out)
}

func toGenPurchaseEntry(e admin.PurchaseEntry) gen.AdminPurchaseEntry {
	out := gen.AdminPurchaseEntry{Android: e.Android, Ios: e.IOS, Version: e.Version}
	out.Wecom.Name = e.WeCom.Name
	out.Wecom.WecomId = e.WeCom.WeComID
	out.Wecom.ReplyTime = e.WeCom.ReplyTime
	out.Wecom.Note = e.WeCom.Note
	out.Wecom.QrUploaded = e.WeCom.QRKey != ""
	out.Wecom.QrUrl = opt(e.QRURL)
	return out
}

func (h *Handlers) GetAdminPurchaseEntry(c *gin.Context) {
	e, err := h.deps.Admin.PurchaseEntry(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPurchaseEntry(e))
}

func (h *Handlers) UpdateAdminPurchaseEntry(c *gin.Context) {
	var body gen.AdminPurchaseEntryInput
	if !bind(c, &body) {
		return
	}
	in := payment.PurchaseEntry{Android: body.Android, IOS: body.Ios, WeCom: payment.WeComContact{Name: body.Wecom.Name, WeComID: body.Wecom.WecomId,
		ReplyTime: body.Wecom.ReplyTime, Note: body.Wecom.Note}}
	e, err := h.deps.Admin.UpdatePurchaseEntry(c.Request.Context(), currentAdmin(c).ID, in, body.Version)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPurchaseEntry(e))
}

func (h *Handlers) UploadAdminWecomQr(c *gin.Context) {
	var body gen.UploadAdminWecomQrJSONBody
	if !bind(c, &body) {
		return
	}
	e, err := h.deps.Admin.UploadWeComQR(c.Request.Context(), currentAdmin(c).ID, string(body.ContentType), body.DataBase64)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenPurchaseEntry(e))
}

func (h *Handlers) IssueManualRedeemCode(c *gin.Context) {
	var body gen.IssueManualRedeemCodeJSONBody
	if !bind(c, &body) {
		return
	}
	in := admin.ManualSale{Phone: body.Phone, Tier: string(body.Tier), AmountCents: body.AmountCents, PayMethod: body.PayMethod}
	if body.Remark != nil {
		in.Remark = *body.Remark
	}
	r, err := h.deps.Admin.IssueManualCode(c.Request.Context(), currentAdmin(c).ID, in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": r.Code, "order_no": r.OrderNo, "batch_id": r.BatchID})
}
