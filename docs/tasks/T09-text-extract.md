# T09 文件取文本

- 仓库：后端（peetraining-server）
- 依赖：T08
- 计划完成：10/8
- 状态：部分完成（识别服务对比等样本与账号）

## 目标

把各种格式的资料变成带页码的文字，供 AI 结构化。

## 范围

- Word（.docx）：解析文档 XML，保留段落、编号与表格；旧版 .doc 返回明确提示
- Excel（.xlsx）：excelize 读表，第一行作表头；提供导入模板
- 粘贴文字：直接使用
- 文字版 PDF、印刷体拍照、扫描版 PDF：封装两种候选识别服务，用你提供的少量样本先对比识别正确率、版面还原、每页成本，结论写 ADR 0007
- 统一输出：页序号、页内文字、表格、低置信度位置；计费页数按 PRD v3 11.12
- 作为导入流水线第 3 步接入 Worker，失败可单步重试

## 参考

- docs/dev-spec.md 第六节「格式」表
- docs/open-questions.md Q01

## 验收

- 每种格式至少 2 个样本能正确取出文字与页码（样本放 evals/private/，不提交）
- ADR 0007 写明候选对比结果和选择
- 扫描版 PDF 默认受功能开关控制

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - internal/extract（纯解析，不访问数据库与云服务）：.docx 保留段落、自动编号（按 numbering.xml 还原「一、」「1.」「(1)」「①」等）、表格（第一行作表头，同时写进页面文字），修订模式删除的文字不要，每 1500 字一页、段落尽量不跨页；.xlsx 用 excelize，每张可见工作表第一行作表头，每行写成「表头：值」，每 50 行一页；粘贴文字每 1500 字一页；旧版 .doc / .xls、加密文件、损坏文件返回用户能看懂的原因
  - Excel 导入模板：extract.Template()，`go run ./cmd/api import-template 路径` 生成文件，T12 放进 App 静态资源供 1.5 下载
  - internal/cloud/ocr 新增 PDFParser 接口（逐页文字、低置信度位置、是否扫描页）；mock 按换页符分页
  - material.Extract：导入流水线第 3 步，写 material_pages（tables、low_confidence 为 JSON）、实际页数与计费页数；可重复执行，重跑删除多出的旧页；文件本身的问题标 failed 并写原因、任务不重试；扫描页受 scanned_pdf 开关控制；PDF 超过 200 页拒绝
  - Asynq 任务 material:extract（TaskID 去重、重试 3 次、10 分钟超时）；T10 的流水线编排会在上传确认后入队，1.6b「重试」重新入队这一步
- 偏离计划的地方与原因：
  - 识别服务的样本对比没做：需要你提供样本和能访问阿里云的环境（本开发环境访问不了阿里云）。ADR 0007 已写候选、对比方法和已定部分，Q01 保持待定
  - PDF 不在服务端解析文字层，统一交给识别服务（Go 的 PDF 库对中文字体与表格还原不稳定，见 ADR 0007）
- 需要手动验证的步骤：
  - 你提供样本（每种格式至少 2 个，放 evals/private/）后，接真实识别服务跑对比，结论补进 ADR 0007，并实现选中的 PDFParser / Recognizer
  - 用 Word、WPS 各存一份带自动编号与表格的真题 .docx，确认题号与表格正确
- 验收结果：
  - [x] Word、Excel、粘贴：测试覆盖编号、表格、分页、修订、坏文件（internal/extract 覆盖率 93%）；PDF、图片在 mock 下取文字与页码正确（TestExtract）
  - [ ] 每种格式 2 个真实样本：等样本
  - [ ] ADR 0007 候选对比结果：等样本与账号（候选与方法已写）
  - [x] 扫描版 PDF 默认受功能开关控制，只对指定用户打开后可导入（TestExtractFailures）
