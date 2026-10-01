# T08 资料上传、额度台账与内容安全

- 仓库：后端（peetraining-server）
- 依赖：T03
- 计划完成：10/8
- 状态：审查中

## 目标

安全地接收用户文件，按页计额度，过内容安全审核。

## 范围

- 上传：申请预签名地址 → 客户端直传 OSS 私有桶 → 回调确认；sha256 相同的文件直接提示，不重复扣额度
- 单次上限：10 个文件，每个 ≤ 200 页、50 MB；图片 ≤ 30 张；粘贴 ≤ 2 万字（参数可配）
- quota_ledger：解析页数、导入题数、批改次数、AI 出题数、整卷批改、作文批改，按日 / 周 / 月 / 累计计；预占与退回
- 内容安全：文本与图片过阿里云内容安全；不通过的整份标失败并给原因
- 资料列表、删除资料（按 PRD v3 11.12 连带删除并触发预估分重算）
- 额度查询接口（供 3.1c、6.3、4.9 显示）

## 参考

- docs/prd.md 第 11.12 节、第 13.1 节
- docs/dev-spec.md 第六节第 1–4 步

## 验收

- 额度预占、失败退回、并发扣减在集成测试里都正确（同一用户并发两次上传不会超额）
- 拿别人的资料 ID 删除或查看返回 404
- 内容安全 mock 返回拒绝时，资料状态与原因正确

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - internal/material：申请上传（校验使用权确认、单次上限读 rule_params.import_limits、专业课归属、sha256 去重含同一请求内重复、解析额度在开始前预检返回 402 带 need / remaining）→ 建资料记录、对象键 u/{uid}/b/{bank}/m/{id}.{ext}、预签名直传；回调核对对象存在与大小；粘贴文字按 1500 字分页（尽量在换行处断开）存 material_pages，计费页数同页数；资料列表（含组成的真题卷数、待复核题数）、删除前的连带影响、删除
  - 删除连带（PRD 11.12）：从它识别出的题连同作答记录、错题删除（外键级联）；只来自它的知识点删除，其他资料也有的保留并把出处改指到另一份资料；它组成的试卷删除，paper_sessions.paper_id 置空、成绩保留；删除 OSS 原件；OnDeleted 钩子留给 T22 触发预估分重算
  - 内容安全 Moderate：图片按对象键送审，文字逐页按 500 字分段送审；不通过整份标 rejected，原因写明第几页；由 T10 导入流水线第 4 步调用
  - internal/quota：免费版累计 / 每日 / 每周、会员按月与不限；Consume / Reserve / Settle / Refund 都在调用方事务里执行、带幂等键；计数行在事务外创建，事务内 SELECT … FOR UPDATE 锁行
  - 接口：createUploadRequests、confirmMaterialUploaded、createPastedMaterial、listMaterials、getMaterial、getMaterialDeletionImpact、deleteMaterial、getQuota（各项已用、上限、周期、下次重置时间）
  - 本地 mock OSS：预签名地址指向 API 的 PUT /dev/oss/*key（HMAC 签名、有效期），只在非生产环境注册；真机调试用 OSS_MOCK_BASE_URL 指向本机局域网地址
  - sqlc 可空 JSON 列改用 dbtypes.NullJSON（json.RawMessage 读不了 NULL）
- 偏离计划的地方与原因：
  - 并发测试发现事务内 INSERT IGNORE + SELECT FOR UPDATE 会互相死锁（各自先拿共享锁再争排他锁），改为计数行在事务外创建
  - 「并发两次上传不超额」：申请上传只做预检，真正的页数预占在 T10 解析时按实际页数执行（PDF 页数以服务端解析为准）；台账层面的并发扣减不超额已由集成测试覆盖
  - 资料原文页 getMaterialPage（3.7）依赖解析结果，放到 T13
- 需要手动验证的步骤：
  - 接真实 OSS 后：真机选 PDF 直传私有桶、回调成功；预签名过期后上传被拒
  - 接阿里云内容安全后：用测试样本确认文本与图片拒绝时资料状态与原因
- 验收结果：
  - [x] 额度预占、结算退回、幂等、事务回滚、10 个并发扣减只成功 3 个（TestQuotaLedger）
  - [x] 拿别人的资料 ID 查看、确认、删除、看影响、审核，拿别人的专业课上传、粘贴、看列表都返回 404（TestOwnership）
  - [x] 内容安全 mock 拒绝时资料状态 rejected、原因写明第 2 页（TestPasteAndModeration）
  - [x] 删除连带规则（TestDeleteCascade）；单次上限与额度不足预检（TestUploadLimits）
