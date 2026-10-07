# 系统会议 Summary：四源纯契约验证

主线程已采纳独立 **四源纯契约 PASS**，产品已提交推送 `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a` 并核远端一致。[系统会议 Summary rev1](../work-items/d09-system-meeting-summary-selection.md)规格及正式设计已在 `9ff1292d7e64b2ee2e9e049cf4eb625a7d11c6e7` 接受；本结果不完成 S1 服务持久化或整卡验收。

## 接受范围与固定输入

四源均位于 `internal/central/model/contract/`：新 `meeting_summary.go` / `meeting_summary_test.go`，以及 `references.go` / `references_test.go` 的限定增量。交付独立 `MeetingSummarySelection` 的 ID、正字符串 version、显式可空 Model、Clone 与安全格式；更新请求要求 Human 结构、System scope、合法 ID/key/version 和必需 Model，不提供清空。ReferenceOwner 仅增加 `(platform_selector, meeting_summary)` 配对，沿既有必需引用、scope、reasoning 和冻结计划约束。

[作者报告](system-meeting-summary-contract-verification-evidence/author/author-report.md)及[冻结清单](system-meeting-summary-contract-verification-evidence/author/freeze.json)绑定四源 SHA；冻结清单 SHA `623f5d057b1dc677203a073b045babffcbbab1880749ce48dc0217c5c73a60bd`。归档核四源与 `e6cb70bd` 原字节一致；[原依赖比较](system-meeting-summary-contract-verification-evidence/author/accepted-dependency-comparison.json)中 64 个未改仓库输入复用固定 `ba7ce7296b7c08d976d3fc3ce1e1d0b727a3f04f`，保留原清单和 Git 绑定，不复制源码树。已提交旧 PlatformSelection、SelectionRef、Purpose、commands 与 JSON 外形不改，也未提前放开 `platform.meeting_summary` Resolution。

[独立报告](system-meeting-summary-contract-verification-evidence/independent/review.md) SHA `95449beadace00748f7e88fa57fcae8b17c36775ddb65b3ef4000d42fd226004`；[最终核对](system-meeting-summary-contract-verification-evidence/independent/final-audit.json) SHA `21d15b46c22176739e71286e8caead89334012b7d59a67d77e6609fd337a4fdf`，由[独立冻结清单](system-meeting-summary-contract-verification-evidence/independent/freeze.json)绑定。提交源码匹配是输入证据，不宣称提交后重跑测试。

## 实际检查与原失败

| 检查 | 实际结果与边界 |
| --- | --- |
| 作者 pure / race | 各 31 顶层、61 子测 PASS，含 7 个新增 Summary 顶层；实际 exit0，2.314s / 24.492s |
| 作者格式 / vet | 四源 gofmt、`go vet` 通过；vet 实际 exit0，0.471s；没有 test/build/vet 失败或超时 |
| 独立定向 pure / race | 各 3 顶层 PASS，1.157s / 2.757s；覆盖 288 组 owner/role/project/clear/reasoning 组合及 binding 接受域、16 份 Clone 并发隔离、字符串整数精度、非法 version 与值/指针的真实日志 handler |

独立复核并复用作者整包结果，没有重跑整套。两方使用固定 Go 1.27.1、离线 readonly module，driver 为 45s 总上限/42s 子命令上限、subreaper=true；每次直接子进程实际 wait/join，无超时、终止动作或剩余后代。独立两次 PID/starttime 尾核及 driver PID 检查均为空；没有服务、listener、数据库、容器、迁移或后台任务运行。

独立 `independent-pure` 首轮曾把 JSON 反解被拒绝也判为 authority 失败，其余两项当次通过。仅将 scratch 探针条件修为“反解成功且 Validate 成功才算越权”，四产品源不变；原[探针](system-meeting-summary-contract-verification-evidence/independent/probe_test.initial.go.txt)、失败命令/result/stdout/stderr 均保留，修正后 pure/race 通过。该首错属于验证断言，不是产品缺陷。作者两次前置只读路径/缓存探查误差保留在[原观察说明](system-meeting-summary-contract-verification-evidence/author/preparation-observations.md)，没有补造同期 raw。

## 保存范围与未验收能力

[来源映射](system-meeting-summary-contract-verification-evidence/source-map.json)保存作者 33、独立 25，共 **58 原件/247080 bytes**，另加这一份小映射。十组命令及实际结果/raw、两版独立探针、driver、原输入/依赖/尾核清单均可追溯；大 `go list` stdout 和四源 diff 只保存指纹，前者复用原依赖清单，后者复用固定 Git。Go 探针以 `.go.txt` 保存原字节，不进入包发现。原 manifest 不改，不宣称其列出的所有源码/工具均已归档；不复制 cache、工具、binary 或源码树。

本次只核哈希、JSON、链接与格式，未执行产品或旧脚本。DTO 的 Human 结构校验不证明当前管理员权限，ModelID 校验不证明 enabled/System chat 可用；数据库未初始化/未配置/损坏三状态、migration00020、service 事务、canonical/reference 原子写、singleton 并集、旧 receipt 升级、删除替换、HTTP/client、S2/S3 及真实 Meeting consumer 均未在四源结果中验收。完整 S1、D08–D28/E01 仍未完成，E01 未开始，ready503 与 Object runtime join、OpenAI tools 独立验证、SPA concurrent-publication 三停止保持。
