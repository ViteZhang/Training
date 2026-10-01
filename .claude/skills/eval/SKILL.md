---
description: 跑 AI 评测并给出报告。只在用户输入 /eval 时使用。
disable-model-invocation: true
argument-hint: [能力：import、kp、grading、essay、ocr 或 all]
---

1. 执行 make eval cap=$0（all 表示依次跑全部）
2. 输出表格：能力、样本数、指标、门槛（docs/prd.md 第 12.3 节）、是否达标、模型、提示词版本、平均时延、每千次成本
3. 对没达标的能力，列出错得最多的 5 个样本和错误类型，给出下一步的调优建议（改提示词、换模型、改切题规则），等我选择后再改
4. 把这次结果追加到 evals/README.md 的记录表
