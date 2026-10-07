# D08 Project Owner 只读 HTTP 规格验收

2026-10-07，root 已采纳[正式卡 rev2](../work-items/d08-project-owner-read-http.md)的完整限定 STATIC PASS；当前仅接受工程规格，尚未授权产品实施、Go 执行或真实资源。接受卡 SHA `bf77809f18c2fd5cb3d512ccaf6ee878121baf274f8c2d7f355ce3c77ee80ef0`，规格已提交推送 `ac33ea1516df0c9747c77b5a23ab1f5f018ec1c0`，远端一致由 root 核实；不表示列表/详情 HTTP 已交付或完整 D08 完成。

## 原问题与两行修订

[rev1 原卡](project-owner-read-http-spec-verification-evidence/author/rev1-card.md.txt) SHA `0ac1ab4813ea56fcdb18bff95d122e383af55d2953dc01f626578faa94e59aff` 的[完整独审](project-owner-read-http-spec-verification-evidence/independent/rev1/review.md)保留唯一阻断 D08-STATIC-01：把 CauseID 与公共 HTTP 的 CommitState/RequestID/RetryHint 并列，混淆内部事务证据与公开协议，可能扩大只读规格的公共 Problem 范围。

[rev1→rev2 精确差量](project-owner-read-http-spec-verification-evidence/author/rev1-to-rev2.diff) SHA `ff65874d41235acad61387372b81a3cdcb103f7e80f65705954f450987f8343b` 只改第3行修订号与第80行错误投影，其余173行原字节相同。内部 Fault/UnknownAttempt 保留原 CauseID/attempt；HTTP 沿既有 code/commit_state/request_id/retry_hint，不新增 cause_id 字段/头，不改公共 Problem/schema。

[rev2 复审](project-owner-read-http-spec-verification-evidence/independent/rev2/review.md) SHA `e1feb6719a047a03fcf000e9abbf96d1a149dd890b8fb70726f4ae8f7a812c99`、[结果](project-owner-read-http-spec-verification-evidence/independent/rev2/result.json) SHA `7f1c849f25e968b8ad7dfae20f912e072e90a10b00a5c4963f3d08486d1c2524` 复用 rev1 已完成的全面核对，给出完整 STATIC PASS、无未闭合阻断。root 采纳后只改页首状态，保留[页首差量](project-owner-read-http-spec-verification-evidence/author/adoption-header.diff)及[冻结](project-owner-read-http-spec-verification-evidence/author/header-freeze.json)；§1到末技术 SHA `d607c0c57f1465160de380b2f98eb568fe81b5efac0f1e85d779795dca7a36fa` 未变。

## 接受范围与后续门槛

规格限定当前 Human/Session/Owner 的 `/api/v1/projects` 列表和稳定 ID 详情 GET/HEAD，复用窄 Reader、原 lifecycle/权限/游标边界、完整 limit+1 哨兵校验、5MiB完整编码和预认证起2秒实际IO终局。原 resolve/nested Usage 不抢占；16技术路径和后端README末件分别授权。静态容量算术、旧六组名称发现及真实Unknown fixture可实现性，不等于最大合法编码、schema、native/PG或默认root实际通过。

固定依赖为 `baa6ffac7bdf87dea6f052704e509d7b547e9886`，四份 app 来源固定 `c210d249600d98871513c56fb9a8fff7c50a4c34`；没有把活动S2根源码当接受输入。共享account.go须等S2完整接受、停写并由root唯一移交，再冻结实际传递闭包与验收；该规格不自动授予此交接或任何执行。创建/写/lifecycle runtime/UI/迁移/D24不在范围；生产Resolution/Invocations未绑定、ready503、完整D08–D28/E01未完成、E01未开始及Object runtime join/OpenAI tools独立验证/SPA concurrent-publication三停止保持。

## 原件与归档检查

[source-map.json](project-owner-read-http-spec-verification-evidence/source-map.json)保存15份小原件；rev1全文加精确两次差量可定位各版本，不重复复制rev2全文。34条固定来源及实际只读Git命令沿原输入记录保留，本归档复核对应Git字节/原固定副本；没有源码树、大graph、cache或binary。原卡内部链接仍按工作项目录解释，原件的历史pending状态与空白保持。

作者[检查器首错](project-owner-read-http-spec-verification-evidence/author/rev1-check-first-error.txt)是新增文件diff-check实际退出1且无空白诊断，被原脚本误要求0；原说明保留，不写成产品失败。独审原报告另记录一次scratch错误cwd的Git读取失败，未提供独立raw，本档不补造该原件。[归档自查](project-owner-read-http-spec-verification-evidence/archive-checks.json)核哈希/JSON、两行差量、26个卡链接与正式报告引用、UTF-8/LF和限定diff检查；原patch空白例外单列。未运行Go、schema执行器、测试、浏览器、listener/容器/数据库、旧证据脚本或Git写操作；四入口与S2归档未改。
