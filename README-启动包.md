# Training 启动包（10 月 1 日版）

把两个文件夹的内容分别复制到云效上的两个空仓库里，提交一次，就可以开始用 Claude Code 开发。完整步骤见《Training · Claude Code 开发操作指南》。

## 目录

```
peetraining-server/            后端仓库（Go）
  CLAUDE.md                    Claude Code 每次开工自动读取的项目说明和规矩
  .claude/skills/              斜杠命令：/card 开发一张卡、/review 审查、/fix 修复、/eval 跑评测
  docs/prd.md                  PRD v3（业务规则的依据）
  docs/dev-spec.md             研发规格与开发计划
  docs/tech-plan.md            基础技术方案
  docs/open-questions.md       未决问题（已预填 Q01–Q10 与已定事项）
  docs/adr/README.md           技术决策记录的写法与首批清单
  docs/tasks/                  任务卡清单与后端要做的卡（T01–T32 中的 S 与 S + W 卡）
  evals/README.md              AI 评测记录表；真实样本放 evals/private/，不进仓库
peetraining-web/               前端仓库（Expo App + React 管理后台）
  CLAUDE.md
  .claude/skills/              /card、/review、/fix、/sync-api 同步接口契约
  docs/prd.md、dev-spec.md、tech-plan.md、open-questions.md、adr/README.md
  docs/tasks/                  任务卡清单与前端要做的卡（W 与 S + W 卡）
  docs/design/INDEX.md         页面编号 → 设计稿文件
  docs/design/NOTES.md         每个模块的设计调整说明
  docs/design/pages/           110 个设计稿画板中的 109 个页面源码（.dc.html）
  docs/design/spec.dc.html     旧版设计规范板（颜色以 VI 为准）
  docs/design/vi/README.md     这里需要你放入 VI 规范 HTML 和 Logo SVG
```

## 你还要手动补的两个文件

1. 项目文件「考研Training-VI规范-v1.0.html」→ 放到 peetraining-web/docs/design/vi/
2. Logo 的 SVG 源文件 → 已放到 peetraining-web/docs/design/vi/logo.svg（10 月 4 日，D41）

## 仓库与同步（10 月 1 日已定）

- 正式代码库是云效上的 peetraining-server、peetraining-web 两个仓库，流水线用云效（D9、D10，见两边的 docs/open-questions.md）
- 本 GitHub 仓库是启动包和开发暂存：开发期先在一个分支上做（D14），两个目录各自同步到云效，例如：

```
git subtree split --prefix=peetraining-server -b sync-server
git push <云效 peetraining-server 地址> sync-server:main
git subtree split --prefix=peetraining-web -b sync-web
git push <云效 peetraining-web 地址> sync-web:main
```

- 两个目录里的 prd.md、dev-spec.md、tech-plan.md、open-questions.md、adr/README.md、tasks/README.md 以及 S + W 任务卡是同一份内容，拆成两个仓库后各自需要；修改时两边一起改
