# T05 App 与后台发布通道

- 仓库：前端（peetraining-web）
- 依赖：T02、T04
- 计划完成：10/7
- 状态：未开始

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
- 偏离计划的地方与原因：
- 需要手动验证的步骤：
