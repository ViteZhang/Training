# 文件分享导入、语音作答与注册引导优化

- 状态：本地实现及自动化验证完成，待真实模型与真机验收；发布状态以GitHub PR和云效记录为准。
- 用户确认：微信采用分享／其他应用打开导入，App 内提供操作指引；练习和模拟考试接入语音大模型；满分默认选中 150；考试年份改为选择框加底部列表，默认最近一届未结束考试。
- 范围：资料与答案文件导入、文件接收与待导入保留、语音录音及按题后台转写、交卷衔接、注册引导与备考设置。
- 验收：微信导入步骤与保存手机文件兜底；未登录和未选择专业课时不丢文件；录音停止后切题不丢失；确认追加幂等；交卷等待上传登记，后台转写完成才批改；失败可重试或弃用；150 默认选中无需点击；已有分值和年份保留；小屏与 1.3 倍字体布局。
- 边界：不读取微信聊天记录，不开发小程序，不扩写考生语音答案；本地开发不读取生产 `.env.local`，不迁移或部署生产。
- 先前手写稿后台识别的未提交修改保留，本次在其基础上衔接语音流程。


## 实现

1. 导入文件页、补充答案页加入“从微信导入”弹层，明确下载、右上角分享／其他应用打开、选择本 App 的步骤。iPhone 说明“保存待导入，再返回 App”，找不到 App 时查看更多，提供保存到手机后选择文件的备用入口。
2. 全局接收冷启动、热启动与返回前台的文件，复制临时文件到本机文档目录，再保存待导入列表。未登录、未选专业课和正在答题均不强制跳走；浮层提示继续处理。可选专业课和题目／资料／补充答案，解析任务建立后才清理副本。保持原流程的格式、数量与大小限制。
3. 练习与整卷主观题增加语音输入（`voice_answer` 开关）。复用现有 expo-audio，无新增 npm 依赖。麦克风按需授权；结束录音即进入独立持久队列。多段按录制顺序拼接，确认或提交重试不重复追加；失败可重试或弃用。录音器在停止并保存之后释放，切题和组件退出不取消上传任务。
4. 语音模型为 `qwen3-asr-flash`，与视觉、文字的 qwen3.8-flash 配置分开。只转写原话，不补充作答。练习提交等待识别；整卷等待上传登记后可先交卷，Worker 完成识别并默认确认再批改。
5. 满分独立一行，150 分直接选中并打勾，无需先点按钮；保留原有已选满分、目标分默认 70%。年份改为单个选择框、底部纵向列表，展示研考年份和实际考试日期。默认服务端返回的最近未结束一届（不是直接按手机自然年误选），已有有效选择保留。

## 主要文件

- `apps/mobile/app.config.ts`、`plugins/with-file-receive.js`、`plugins/ios/ShareIntoViewController.swift`、`app/+native-intent.ts`：原生文件接收与分享扩展。
- `src/features/import/WeChatImportGuide.tsx`、`SharedFileReceiver.tsx`、`sharedInbox.ts`，`app/import/shared.tsx`、`files.tsx`、`answers.tsx`：指引、持久接收、待导入与原流程衔接。
- `src/features/practice/VoiceRecorder.tsx`、`voiceQueue.ts`、`voiceUpload.ts`、`PracticeVoiceInput.tsx`、`SubjectiveFlow.tsx`、`grading.tsx`：录音、上传、后台任务与草稿。
- `src/features/paper/photos.ts`、`PhotoReview.tsx`、`app/paper/session/[id].tsx`：复用照片队列处理录音与整卷交卷。
- `src/features/profile/FullScoreSelect.tsx`、`ExamYearSelect.tsx`，`app/(onboarding)/subject.tsx`、`target.tsx`、`app/settings/prep.tsx`：默认满分与年份选择。
- 后端 `api/openapi.yaml`、`internal/practice/voice.go`、`paper_handwriting.go`、`internal/http/voice.go`、`internal/cloud/asr/qwen.go`、`internal/ai/voice.go`、`internal/jobs/jobs.go`、`internal/config/config.go`、`db/migrations/00028_voice_answers.sql`。

## 验证范围

- 前端全工作区 typecheck 与 lint 通过；App 27 个测试套件共201项通过，管理端27项、API客户端11项、UI tokens 5项通过。App 回归包含新增语音队列、录音器生命周期、分享文件复制与失败恢复、微信引导、年份切换及默认满分测试。
- 后端 ASR HTTP 合约（模型、音频签名地址、仅转写、非法输出／超时状态／重定向错误脱敏）、AI 输入输出校验、Worker 路由测试。
- 隔离 MySQL/Redis 集成与 race：语音开关、归属隔离、顺序、持久恢复、幂等、失败重试、弃用不复活、考试截止前登记与交卷默认确认；全量内容接口 IDOR 回归。
- 临时目录生成 iOS、Android 原生工程成功；检查分享扩展、App Group、文件 MIME 和 Android ACTION_VIEW 适配；分享扩展 Swift 使用 iPhoneOS SDK 类型检查通过。未编译签名安装包，未用真机微信或麦克风做端到端验收。
- 现有 `make eval-all` 使用 mock 运行：import/kp/rubric/grading/ocr 公开样例通过，essay 公开样例 50% 未达到 75% 门槛（该能力本次未改，不能因此声明全量评测达标）。无真实录音样本和模型凭据，不能声明 ASR 准确率达标。

## 真机验收与发布前置

1. 迁移顺序 00027 → 00028 → API／Worker → 原生新版 App。配置 `ASR_PROVIDER=bailian`、语音模型及百炼凭据，先仅对测试账号打开 `voice_answer`。详见后端 `docs/runbook.md`。
2. iPhone／Android：微信中 PDF、docx、xlsx、图片分别分享；冷／热启动、App 未登录、正在模拟考试、同名文件、多文件、超限、未下载文件及取消分享。确认正确课程、类型、文件名、可删除、重复接收不重复加入，上传失败仍可继续。
3. iPhone 分享列表／其他应用打开中未出现 App 时验证引导和“更多”备用步骤；核对 App 名称随 development／preview／production 变化，App Group 与扩展签名正确。
4. 练习与模拟考试录音：麦克风拒绝后恢复、背景来电／锁屏、停止后立即切题、断网／重启、不同题同时识别、多段录音乱序返回、编辑确认、失败重试和交卷自动采用。核对专业术语、停顿、无声和五分钟长度限制。
5. 考试临近截止须提前结束录音并完成上传；未能在截止前登记的文件仍保留本机，界面提示未包含于答卷。核对手动与到时自动交卷、历史答卷和批改重试。
6. 390×844、小屏与 1.3 倍字体：满分单独成行、默认150无需点击可继续；年份列表可滚动、显示考试日期、切换及重进保留。


最终本地复核：后端 `go test -short -race ./...`、`go vet ./...`、`golangci-lint --new-from-rev=HEAD` 通过；新增语音与先前手写稿隔离集成、全内容接口 IDOR/race 通过。`git diff --check` 通过。测试专用 MySQL/Redis 容器已清理，既有容器未改动。Jest 的套件断言全部通过，最终运行使用 `--forceExit` 结束存活的测试 runner；不将它作为真机后台任务验收。源码位于 `/Volumes/Files/workspace/Training` 的 `codex/input-import-onboarding` 分支，包含上一轮未提交的手写稿功能改动，本轮的GitHub与云效状态以发布记录为准。


逐项验收见前端任务文档 `docs/tasks/2026-10-30-release-acceptance.md`。
