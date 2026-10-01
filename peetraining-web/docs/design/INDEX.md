# 设计稿索引

页面编号 → 视觉稿文件。编号与 docs/prd.md 一致。pages/ 下的 .dc.html 只用来读布局、文案和交互，颜色字体按 vi/ 替换（见 CLAUDE.md「视觉规则」）。

在线预览（含交互与画布）：https://claude.ai/code/artifact/0fb1b154-6abd-41dd-bc16-0d03ee1a908c

说明：文件 <title> 里的编号有少数是旧编号，一律以本表为准。spec.dc.html 是旧版设计规范板，组件形态可参考，颜色以 VI 为准。

## 0-启动与账号·登录注册

| 页面 | 文件 |
| --- | --- |
| 0.1 启动页（按 VI 竖版组合重做，旧版仅作布局参考） | pages/m0_splash.dc.html |
| 0.2 手机号登录（可交互） | pages/m0_login.dc.html |
| 0.2a 已输入手机号 | pages/m0_login_filled.dc.html |
| 0.2b 未同意协议 | pages/m0_login_agree.dc.html |
| 0.3 输入验证码 | pages/m0_code.dc.html |
| 0.3b 验证码错误 | pages/m0_code_err.dc.html |
| 0.3c 收不到验证码 | pages/m0_code_help.dc.html |
| 0.4 用户协议与隐私政策 | pages/m0_agreement.dc.html |
| 0.4b 协议更新重新同意 | pages/m0_agreement_update.dc.html |
| 0.5 权限说明 · 相机 | pages/m0_permission.dc.html |
| 0.5b 权限被拒 · 去设置 | pages/m0_permission_denied.dc.html |
| 0.6 版本更新 | pages/m0_update.dc.html |
| 0.6b 强制更新 | pages/m0_update_force.dc.html |

## 1-新用户引导

| 页面 | 文件 |
| --- | --- |
| 1.1 你考哪门专业课（可交互） | pages/m1_subject.dc.html |
| 1.2 设定目标分（可交互） | pages/m1_target.dc.html |
| 1.3 备考安排（可交互） | pages/m1_setup.dc.html |
| 1.4 选择导入方式（可交互） | pages/m1_import.dc.html |
| 1.5 选择文件 | pages/m1_upload.dc.html |
| 1.5b 粘贴文字导入 | pages/m1_paste.dc.html |
| 1.6 AI 解析中 · 设学习提醒 | pages/m1_parsing.dc.html |
| 1.6b 部分文件识别失败 | pages/m1_parse_fail.dc.html |
| 1.7 确认导入结果（可交互） | pages/m1_confirm.dc.html |
| 1.7b 核对主观题采分点 | pages/m1_confirm_item.dc.html |
| 1.8 题库建好了 | pages/m1_done.dc.html |

## 2-今日

| 页面 | 文件 |
| --- | --- |
| 2.1 今日首页 · 正常（完整长图） | pages/m2_home.dc.html |
| 2.1b 今日首页 · 题库整理中 | pages/m2_home_parsing.dc.html |
| 2.1c 今日首页 · 还没导入资料 | pages/m2_home_empty.dc.html |
| 2.1d 今日首页 · 今日已完成 | pages/m2_home_done.dc.html |
| 2.1e 进入新阶段提示 | pages/m2_stage.dc.html |
| 2.1f 修改目标分（可交互） | pages/m2_target.dc.html |
| 2.2 今日训练完成 | pages/m2_complete.dc.html |
| 2.3 消息中心 | pages/m2_messages.dc.html |

## 3-题库

| 页面 | 文件 |
| --- | --- |
| 3.1 题库 · 知识点（可交互） | pages/m3_bank_kp.dc.html |
| 3.1b 题库 · 题目（可交互） | pages/m3_bank_q.dc.html |
| 3.1c 题库 · 资料 | pages/m3_bank_files.dc.html |
| 3.1d 删除资料确认 | pages/m3_delete.dc.html |
| 3.2 搜索 | pages/m3_search.dc.html |
| 3.3 题目详情 | pages/m3_question.dc.html |
| 3.4 知识点卡片（可交互） | pages/m3_card.dc.html |
| 3.5 知识点更多操作 | pages/m3_more.dc.html |
| 3.6 编辑知识点 | pages/m3_edit.dc.html |
| 3.7 原文查看 | pages/m3_source.dc.html |
| 3.8 考情分析 | pages/m3_profile.dc.html |
| 3.9 知识图谱（可交互） | pages/m3_graph.dc.html |
| 3.10 题库 · 908 作文知识库（导入作文资料后） | pages/m3_essaykb.dc.html |

## 4-训练

| 页面 | 文件 |
| --- | --- |
| 4.1 训练首页 | pages/m4_practice.dc.html |
| 4.2 自定义练习（可交互） | pages/m4_setup.dc.html |
| 4.3 答题 · 单选（可交互） | pages/m4_answer.dc.html |
| 4.4 答题 · 主观题 | pages/m4_subj.dc.html |
| 4.5 拍手写稿识别 | pages/m4_photo.dc.html |
| 4.6 AI 批改中 | pages/m4_grading.dc.html |
| 4.7 批改结果 | pages/m4_result.dc.html |
| 4.8 批改有异议（可交互） | pages/m4_dispute.dc.html |
| 4.9 批改次数用完 | pages/m4_quota.dc.html |
| 4.10 退出训练确认 | pages/m4_exit.dc.html |
| 4.11 本组训练总结 | pages/m4_summary.dc.html |
| 4.12 错题本 | pages/m4_wrong.dc.html |
| 4.13 答题规范 | pages/m4_norm.dc.html |
| 4.14 背诵 · 挖空（可交互） | pages/m4_recite.dc.html |
| 4.15 背诵 · 默写结果 | pages/m4_recite_write.dc.html |
| 4.16 背诵 · 口述 | pages/m4_recite_speak.dc.html |
| 4.17 背诵完成 | pages/m4_recite_done.dc.html |
| 4.18 整卷列表（可交互） | pages/m4_papers.dc.html |
| 4.19 选择作答模式（可交互） | pages/m4_mode.dc.html |
| 4.20 整卷 · 练习模式答题 | pages/m4_exam.dc.html |
| 4.21 整卷 · 模拟考试答题 | pages/m4_mock.dc.html |
| 4.22 答题卡 | pages/m4_examcard.dc.html |
| 4.23 交卷确认 | pages/m4_submit.dc.html |
| 4.24 整卷报告 | pages/m4_examreport.dc.html |
| 4.25 时间分析报告 | pages/m4_timereport.dc.html |

## 5-作文

| 页面 | 文件 |
| --- | --- |
| 5.1 作文训练 · 导入作文资料后（可交互） | pages/m5_topics.dc.html |
| 5.1b 作文 · 还没导入资料 | pages/m5_empty.dc.html |
| 5.2 写作文 | pages/m5_write.dc.html |
| 5.3 素材弹层 | pages/m5_material.dc.html |
| 5.4 拍照上传手写稿 | pages/m5_upload.dc.html |
| 5.5 作文批改中 | pages/m5_grading.dc.html |
| 5.6 作文批改结果（可交互） | pages/m5_result.dc.html |
| 5.7 范文详情 | pages/m5_model.dc.html |
| 5.8 作文本 | pages/m5_book.dc.html |
| 5.9 评分标准 | pages/m5_rubric.dc.html |

## 6-我的

| 页面 | 文件 |
| --- | --- |
| 6.1 我的 | pages/m6_me.dc.html |
| 6.2 提分看板 | pages/m6_dashboard.dc.html |
| 6.3 我的资料 | pages/m6_library.dc.html |
| 6.4 导出题库 | pages/m6_export.dc.html |
| 6.5 会员中心（可交互） | pages/m6_member.dc.html |
| 6.6 支付结果 | pages/m6_payresult.dc.html |
| 6.7 兑换码 | pages/m6_redeem.dc.html |
| 6.8 邀请研友 | pages/m6_invite.dc.html |
| 6.9 备考设置（可交互） | pages/m6_prep.dc.html |
| 6.10 设置 | pages/m6_settings.dc.html |
| 6.11 账号与安全 | pages/m6_account.dc.html |
| 6.12 注销账号 | pages/m6_delete.dc.html |
| 6.13 意见反馈（可交互） | pages/m6_feedback.dc.html |
| 6.14 考后回访（可交互） | pages/m6_survey.dc.html |

## 7-管理后台

| 页面 | 文件 |
| --- | --- |
| 7.1 概览 | pages/a7_overview.dc.html |
| 7.2 用户 | pages/a7_users.dc.html |
| 7.3 会员与订单 | pages/a7_orders.dc.html |
| 7.4 兑换码 | pages/a7_codes.dc.html |
| 7.5 资料解析监控 | pages/a7_parse.dc.html |
| 7.6 批改异议 | pages/a7_disputes.dc.html |
| 7.7 用户反馈 | pages/a7_feedback.dc.html |
| 7.8 AI 与额度 | pages/a7_config.dc.html |
| 7.9 消息与公告 | pages/a7_notice.dc.html |
| 7.10 需求洞察 | pages/a7_demand.dc.html |
| 7.11 官方题库 | pages/a7_official.dc.html |
| 7.12 内容生产 | pages/a7_produce.dc.html |
| 7.13 审核队列 | pages/a7_review.dc.html |
| 7.14 版本发布 | pages/a7_release.dc.html |
| 7.15 后台账号与权限 | pages/a7_roles.dc.html |
