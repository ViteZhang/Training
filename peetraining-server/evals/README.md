# AI 评测

真实样本与人工标注放在 evals/private/（已在 .gitignore 中排除），格式为 JSONL。门槛见 docs/prd.md 第 12.3 节。
evals/samples/ 是虚构的公开示例，只用来验证评测命令能跑。

## 运行

```
make eval cap=import          # 默认 mock 模型
AI_PROVIDER=bailian BAILIAN_BASE_URL=... BAILIAN_API_KEY=... make eval cap=import
```

## 样本格式（每行一个 JSON）

- 文字：`"pages": ["第 1 页", "第 2 页"]`，或 `"file": "xxx.docx"`（相对 evals/private，支持 .docx / .xlsx / .txt）
- PDF 与拍照：先用识别服务转成文字填进 pages（识别本身的评测是 cap=ocr）
- import：`"expected": [{"stem": "题干", "answer": "答案，没有留空"}]`，指标：题干识别正确率、答案识别正确率，门槛 95%
- kp：`"expected": [{"name": "知识点", "original_text": "原文表述"}]`，指标：知识点召回率（门槛 85%）、原文一致率（门槛 90%）
- grading：每行一道题 `{"name","subject","qtype","stem","reference","points":[{"seq":1,"content":"采分点","score":4}],"answers":[{"text":"考生答案","human":人工评分}]}`；指标：与人工偏差 ≤ 1 分的比例（门槛 80%）、重批一致性（同一答案批 3 次分差 ≤ 1 分，要求 100%）。样本规模：20 道题 × 三档答案（扩到 50 道）

- essay：每行一个题目 `{"name","subject","topic","required_words","dimensions":[{"name":"立意","score":30,"description":"…"}],"essays":[{"text":"全文，空行分段","human":人工总分}]}`；指标：与人工总分偏差 ≤ 10 分的比例（门槛 75%）、重批一致性（同一篇批 3 次总分差 ≤ 6 分，要求 100%）。样本规模：20 篇不同分数档作文

最省事的标注方法（dev-spec 第七节）：先让流水线跑一遍，在 App 1.7 确认页里改对，把改对后的题干与答案整理成 expected。

## 记录

| 日期 | 能力 | 样本数 | 指标 | 是否达标 | 模型 | 提示词版本 | 备注 |
| --- | --- | --- | --- | --- | --- | --- | --- |
