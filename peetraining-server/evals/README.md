# AI 评测

真实样本与人工标注放在 evals/private/（已在 .gitignore 中排除），格式为 JSONL。门槛见 docs/prd.md 第 12.3 节。
evals/samples/ 是虚构的公开示例，只用来验证评测命令能跑。

## 运行

```
make eval cap=import          # 默认 mock 模型
make eval-all                 # 依次跑全部
# 真实模型：密钥只放在本机环境变量里，不写进文件和命令历史（可以用 read -s 输入）
AI_PROVIDER=bailian BAILIAN_BASE_URL=... BAILIAN_API_KEY=... AI_PRICES="模型=输入价:输出价,…" make eval cap=grading
# 对比候选：指定模型与提示词版本，结果追加到下面的记录表
make eval cap=grading args="-model qwen-plus -prompt v2 -record -note 采分点判定加了同义改写的例子"
```

每次输出门槛指标、每千次调用成本（需配 AI_PRICES，元 / 百万 token）和时延 P50 / P95。选定方法见 docs/adr/0012-ai-model-selection.md。
中转接口（AI_PROVIDER=relay）不是备案模型，只能跑 evals/samples 里的虚构示例，不能跑 evals/private 的真实样本（D20）。

## T31 调优步骤

1. 整理样本到 evals/private/（下面的格式），每项先用当前默认模型跑一遍，`-record` 记下基线
2. 不达标的能力：在 internal/ai/prompts 下复制一份 <能力>@v2.tmpl 修改（旧版本保留），`-prompt v2` 重跑对比；也可以 `-model` 换模型
3. 达标后按 ADR 0012 选主用与对照，填 ADR 的结果表；把选定的提示词版本改成能力定义里的默认版本，模型名写进生产环境变量

## 样本格式（每行一个 JSON）

- 文字：`"pages": ["第 1 页", "第 2 页"]`，或 `"file": "xxx.docx"`（相对 evals/private，支持 .docx / .xlsx / .txt）
- PDF 与拍照：先用识别服务转成文字填进 pages（识别本身的评测是 cap=ocr）
- import：`"expected": [{"stem": "题干", "answer": "答案，没有留空"}]`，指标：题干识别正确率、答案识别正确率，门槛 95%
- kp：`"expected": [{"name": "知识点", "original_text": "原文表述", "rubric": ["采分点", "…"]}]`，指标：知识点召回率（门槛 85%）、原文一致率（门槛 90%）、采分点召回率（标了 rubric 时，门槛 85%）
- rubric：每行一道题 `{"name","qtype","stem","reference","score","points":[{"content":"采分点"}]}`，指标：从参考答案提采分点的召回率（门槛 85%）；同义改写按字面重合度 ≥ 0.6 算召回
- ocr：每行一张图 `{"name","file":"ocr/xxx.jpg","handwriting":true,"text":"人工逐字转写"}`（图片放 evals/private/ocr/），指标：字准确率 = 1 − 编辑距离 / 字数（门槛 95%），比较前去掉空白、统一中英文标点；需 OCR_PROVIDER 接真实服务
- grading：每行一道题 `{"name","subject","qtype","stem","reference","points":[{"seq":1,"content":"采分点","score":4}],"answers":[{"text":"考生答案","human":人工评分}]}`；指标：与人工偏差 ≤ 1 分的比例（门槛 80%）、重批一致性（同一答案批 3 次分差 ≤ 1 分，要求 100%）。样本规模：20 道题 × 三档答案（扩到 50 道）

- essay：每行一个题目 `{"name","subject","topic","required_words","dimensions":[{"name":"立意","score":30,"description":"…"}],"essays":[{"text":"全文，空行分段","human":人工总分}]}`；指标：与人工总分偏差 ≤ 10 分的比例（门槛 75%）、重批一致性（同一篇批 3 次总分差 ≤ 6 分，要求 100%）。样本规模：20 篇不同分数档作文

最省事的标注方法（dev-spec 第七节）：先让流水线跑一遍，在 App 1.7 确认页里改对，把改对后的题干与答案整理成 expected。

## 记录

| 日期 | 能力 | 样本数 | 指标 | 是否达标 | 能力@提示词版本 · 模型 | 成本与时延 | 备注 |
| --- | --- | --- | --- | --- | --- | --- | --- |
