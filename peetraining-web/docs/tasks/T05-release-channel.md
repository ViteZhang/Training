# T05 App 与后台发布通道

- 仓库：前端（peetraining-web）
- 依赖：T02、T04
- 计划完成：10/7
- 状态：审查中

## 目标

让 App 能装到真机上、后台能自动发布。

## 范围

- 后台：流水线构建后同步到应用机 Nginx 的 /admin/ 目录
- EAS：development、preview（内测）、production 三个配置；iOS Bundle ID 与安卓包名 cn.dreamelab.training
- 安卓 preview 包上传 OSS 并生成下载链接；iOS 上传 TestFlight 内部测试
- 版本号规则与「检查更新」接口对接位（0.6 / 0.6b 页面在 T06 做）
- README.md 写真机调试与打包步骤

## 参考

- docs/tech-plan.md「环境与发布」
- docs/open-questions.md Q08

## 验收

- iPhone 能从 TestFlight 安装，安卓机能从 OSS 链接下载安装
- 合并到主分支后，后台网页自动更新到生产

## 不做

- 卡片范围以外的页面、接口和重构；发现需要的，记到 docs/open-questions.md

## 记录（开发中填写）

- 实现要点：
  - apps/mobile/eas.json：development（开发客户端、内部分发、APK）、preview（内测：安卓 APK、iOS 走 TestFlight）、production（商店包、自动递增构建号）；APP_VARIANT 决定显示名与包名后缀，开发版与内测版可以和正式版同时装
  - 包名与 Bundle ID 由 app.config.ts 从 APP_ID 读取，默认 cn.dreamelab.training（D4）
  - scripts/upload-apk.sh：安卓内测包上传 OSS 发布桶，输出版本链接与固定的 latest 链接
  - scripts/deploy-admin.sh：构建管理后台并同步到应用机 /opt/training/admin（云效主机部署或本机执行）
  - 版本号规则与「检查更新」对接位：App 启动调 GET /bootstrap（platform、app_version），0.6 / 0.6b 在 T06 实现
  - README「发布」一节写了三种配置、内测分发、真机调试与后台发布步骤
- 偏离计划的地方与原因：
  - 开发环境里没有 Expo 账号、Apple 账号和 OSS，打包与上传都无法在这里执行，只完成配置与脚本
- 需要手动验证的步骤：
  - `npx eas-cli@latest login`、`cd apps/mobile && npx eas-cli@latest init`，在 eas.json 填 ascAppId
  - `pnpm --filter mobile build:preview`：安卓 APK 用 upload-apk.sh 上传，手机用链接下载安装；iOS 用 submit:testflight 上传，内部测试员从 TestFlight 安装
  - 云效新建「peetraining-web」流水线：代码提交触发，主机部署任务在应用机执行 `bash scripts/deploy-admin.sh`（构建机需 Node 22 + pnpm）
- 验收结果：
  - [ ] iPhone 能从 TestFlight 安装，安卓机能从 OSS 链接下载安装——需要账号就绪后验证
  - [ ] 合并到主分支后，后台网页自动更新到生产——需要云效流水线配置后验证
