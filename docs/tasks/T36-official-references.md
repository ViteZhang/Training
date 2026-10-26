# T36 官方参考素材（PRD 10 月 26 日更新）

- 仓库：后端 + 前端（管理后台）
- 依赖：T30
- 计划完成：10/28
- 状态：开发完成（生产待验证）

## 目标

7.11 录入有授权的教材、讲义，切分后供 AI 生成官方题库的参考答案与采分点时优先引用并标出处（PRD 7.11、11.16 第 5 条，D48）。

## 范围

- 迁移 00025：official_references（名称、教材 / 讲义、授权说明、段数、字数）与 official_reference_segments（章节标题、段落）
- 接口：GET / POST /admin/official/projects/{id}/references、DELETE …/references/{referenceId}（录入、删除只限内容负责人）；POST /admin/official/projects/{id}/answer-candidates（内容负责人与被分配的编辑）
- 切分：按章节标题与自然段，约 600 字一段；没勾选授权或没写授权说明不能录入
- 生成：相关度最高的 3 段 + 教材书名 → answer_generate@v2；返回答案、采分点（合计等于分值）与出处
- 后台 7.11 项目详情「官方参考素材」卡片（列表、录入、删除）；7.12 录题「AI 生成（引用官方参考素材）」，填入答案与采分点并显示出处

## 验收

- official 单元测试：章节切分、长段硬切、长句不当标题
- practice 集成测试 TestOfficialReferences：未授权拒绝、按章切段、引用相关段落、无关题目不引用、删除级联
- 后台 official.test：素材列表、未勾选授权不能提交、录题生成后填入答案与出处

## 不做

- 文件上传与扫描件识别（粘贴原文）
- 出处写进题目（只给编辑核对）
