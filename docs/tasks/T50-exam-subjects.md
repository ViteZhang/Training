# T50 考试科目库：院校 + 学科 + 考试代码 + 科目名称，全国统考（11 月 6 日产品更新）

- 仓库：后端 + App（peetraining-web）+ 管理后台（云效 peetraining-admin）
- 依赖：T44、T46
- 状态：开发完成（后台页面在 peetraining-admin 分支 t50-t51-exam-subjects，待推送）

## 背景

院校专业课库按「院校 + 专业 + 专业课」录入：同一套卷子被几个专业共用时要录好几行，1.1 里重复显示；
官方题库立项又在另一张表里重新填一遍院校、专业、代码、名称，两边靠字符串匹配，后台很乱。
全国统考（311、333 等）各院校同一套卷子，原来只能按院校逐个录。需求见 [official-first.md「考试科目库」](../features/official-first.md)。

## 范围 · 后端

- 迁移 00043：catalog_courses 加 scope（school / national）、majors（适用专业）、course_key（去重键，唯一）；
  同一院校同一代码的多行合并成一条（保留 id 最小的，专业收进 majors，用户专业课和待入库改指向保留的那条），去掉按专业去重的唯一键；
  新表 catalog_school_exams（院校登记使用的统考）；official_projects.catalog_course_id（旧立项按院校 + 代码回填）；
  official_demand_marks 改按（school, subject_code）；录入第一批统考 311、312、313、333
- rules：固定学科选项（学科门类）、ExamSubjectKey
- GET /catalog/courses 加 include_national：新版 App 同时拿统考科目（本校有同代码自命题卷时不列）；不带时与旧版一致
- GET /catalog/schools 的课数算上登记的统考，只登记了统考的院校也能搜到
- 后台：考试科目列表按命题方式、学科筛选，显示登记院校数和立项；新增 / 修改校验学科、统考必须有代码、同名去重；
  旧版后台只传 major 时保留其他适用专业；GET /admin/catalog/disciplines；GET/PUT /admin/catalog/courses/{id}/schools
- 立项：POST /admin/official/projects 可只传 catalog_course_id，一门考试科目只能立一个项目，统考院校记为「全国统考」；
  旧版后台按院校、代码、名称立项时自动挂到考试科目
- 待入库合并到统考科目时登记这所院校
- 1.1 建专业课时带上用户自己的目标院校和选填的专业（选统考科目时也是）
- 7.10 需求洞察按「院校 + 代码」统计，统考只按代码；标记未规划可带 school

## 范围 · App

- 1.1：本校科目少于 4 门时直接列出，另列「全国统考科目」；4 门及以上按学科分组；统考标「全国统考」，自命题显示适用专业；
  「报考专业」选填；手动添加不再填专业，填的代码是统考代码时提示直接选统考科目

## 范围 · 管理后台（peetraining-admin，分支 t50-t51-exam-subjects，待推送云效）

- 「院校专业课库」改名「考试科目库」：命题方式、学科下拉（GET /admin/catalog/disciplines）、适用专业多选、筛选；
  统考科目的「登记院校」编辑；每行「立项做官方题库」（传 catalog_course_id）
- 7.10 需求洞察显示院校列，标记未规划时带 school
- 待入库「合并」可选统考科目

## 验收

- internal/store/exam_subjects_migration_integration_test.go：TestExamSubjectsMigration（多专业行合并、引用改指向、旧立项回填、统考已录入）；TestMigrationsUpDownUp
- internal/practice/exam_subjects_integration_test.go：TestExamSubjects（旧版 App 列表不变、统考对任何院校可见、本校自命题优先、学科与去重校验、登记院校、跨院校统考需求合并、从考试科目立项与重复立项、上线通知跨院校、旧版后台立项挂科目、待入库合并到统考）
- internal/practice/official_first_integration_test.go：TestOfficialFirstRoute 照旧通过
- App：src/__tests__/official-first.test.tsx「1.1 考试科目分组」

## 生产手动验证

- 发布前在生产库执行迁移 00043（运维手册第 8 节）；执行后核对 catalog_courses 每个 course_key 只有一条
- 新版 App：搜一所只有一两门课的院校，确认下方有「全国统考科目」；搜课多的院校确认先选学科
- 旧版 App：1.1 列表与以前一致（不出现统考科目）

## 不做

- 题目「必答 / 限选」标签与专业分组（见 [T51](T51-required-elective.md)）
- 申请收录（1.11）的专业仍必填，表单不变
