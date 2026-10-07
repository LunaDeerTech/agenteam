# Project Owner Usage HTTP：阶段 A 验证

主线程已采纳独立 **A 限定 PASS**，五路径产品已提交推送 `ba7ce7296b7c08d976d3fc3ce1e1d0b727a3f04f` 并核远端一致。本结果只接受 RequireHuman、strict query、安全 DTO/完整编码、纯测试及正式 schema；[工作卡 rev1](../work-items/d09-project-usage-read-http.md)整卡 HTTP/root/真实资源尚未接受。作者 B 已实际 ACK，仅获授 `handler.go` / `handler_test.go` 两新源；本档不读取或接受 B 活动输入。

## 固定输入与实际范围

[作者交接](project-usage-read-http-stage-a-verification-evidence/author/STAGE-A-HANDOFF.md)和[最终独立报告](project-usage-read-http-stage-a-verification-evidence/independent/a02/review.md)绑定同一 [a02 五源清单](project-usage-read-http-stage-a-verification-evidence/author/a02-current-five.json)，SHA `8d78d3b4de0fb8fcb8f0fb8af6ea4ff92fe03c40554d24014b0be422b0e8b7a9`。五路径为 `internal/central/account/http_boundary.go`、其测试、`internal/central/usage/http/wire.go`、其测试及 `api/openapi/project-usage.json`；归档逐一核与 `ba7ce729` 原字节一致，直接复用该提交，不另存候选源码。

独立报告 SHA `51d30cd8f647f26d158c6dcf1593449cffffb978b23714a334a9391b180cb341`；[旧报告原件](project-usage-read-http-stage-a-verification-evidence/independent/a02/review-original.md)保留。[最终执行输入](project-usage-read-http-stage-a-verification-evidence/author/stage-a-final-input.json) SHA `ba247577940319f250b2a5d409848db6c569e38aa4e5d8bdaa5f82ab2ee4a3e0`，[最终状态](project-usage-read-http-stage-a-verification-evidence/author/stage-a-final-state.json) SHA `199222a69c75431bf9c596942f2e6c985fb9ca9fe009baed9c7c8aa0feedf7f1`。

作者原 nonrace 清单为 377 包/2348 文件；后补实际 race 图为 379 包，另列 11 个输入和 7 个 driver/binary/schema/tool 原件指纹，不混称一次完整图。唯一旧输入变化是 scratch runner 的 subreaper 修补，旧 runner 原件保留。独立[依赖核对](project-usage-read-http-stage-a-verification-evidence/independent/a02/dependency-result.json)覆盖 288 项仓库输入：五候选和 common 共六项匹配冻结输入，其余 282 项匹配固定 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`；归档也核对这组 Git 绑定。提交绑定不代表在提交后重新运行业务测试。

## 实际检查

| 范围 | 作者证据 | 独立证据 |
| --- | --- | --- |
| Go 纯行为 | a06 Account 5 顶层/36 子例，a08 Wire 11 顶层/99 子例；race binary，实际 exit0 | 四个独立风险顶层 `TestUsageIndependent*`，race、实际 exit0，外层 2.082s/Go 1.019s |
| 编译/静态 | a05/a07 分包 race compile；a11/a12 分包 `go vet -race -p=1`；gofmt；实际 exit0 | 三路径六操作/查询集合/HEAD 无 content、完整 DTO/原 RequireSystem 字节及 gofmt 核对通过 |
| 标准 schema | Draft202012：129 例；Node：34 pattern、40 scalar、2 Unicode | jsonschema 4.26/referencing 0.37：382 例全过；Node 24.19.0：34 pattern 编译、147 边界例及 128/129 astral 字符计数全过；250 个引用全解析 |

作者实际最大合法默认转义测量为 Invocation 4564 B、List 100 行 464720 B、Aggregate 100 行 93858 B、Project 49672 B；保留最终完整 1 MiB 编码门。这不增加任意伪造 service cursor 或内部对象的容量承诺。RequireHuman 的受控 SQL 行 fixture 只证明 Account 边界组合，不证明持久 Session/Owner 授权。schema 的 format 是 annotation；字节限、日期/先后关系、ID 相等及计数加法仍由正式 Go Validate 执行，不声称 Ajv 或通用 OpenAPI 元规范验收。

命令的 argv/cwd/env、前后输入、实际退出及 raw 在各原目录保存；[作者复用清单](project-usage-read-http-stage-a-verification-evidence/independent/a02/author-evidence-references.json)区分复用与独立执行。独立 Go probe 以 `.go.txt` 保存原字节，避免加入 Go 包发现；382 例输入、schema/Node/结构/引用脚本及原失败均保存。作者 schema 执行输入保留其原有 document/common/cases，并未另外复制已提交源码树。

## 原失败与终局限制

- a01 是固定依赖缺失导致 setup 失败。另线 [Go 依赖恢复结果](project-usage-read-http-stage-a-verification-evidence/dependencies/evidence/recovery-result.json)记录 31 个固定模块、补齐 25 项、ZIP/解压源/go.mod h1 全通过，`go.mod/go.sum` 不变；原自定义 source hash 检查因 Path 排序失败，改为 Go 完整相对名字符串排序后通过，首错与诊断原件保留。该恢复只证明依赖可用，191053771 bytes 的新增模块内容未复制。
- a04 合并 cold race build、a09 cold nonrace vet 均在原 45s 外层预算 TERM，实际 direct wait 为 -15、raw 为空，没有测试断言已开始的证据。后续按原预算分包并使用已热 race 配置通过；不声称 nonrace vet 通过或原轮全绿。
- a04 compiler PID `22765` / starttime `81512` / pgrp `20527` 为 Z、PPID1，未被作者或独立负责人实际 wait；**不能宣称原轮进程清零或已 join**。a09 实际 adopted wait PID30312/status15；[独立尾部记录](project-usage-read-http-stage-a-verification-evidence/independent/a02/tail-result.json)所列作者 a05–a12 与独立七组均为空，不能倒填 a04 清理。
- 独立 schema 首轮 runner 用 `$id` 覆盖已注册 resource，382 例均 PointerToNowhere；原 script/raw/result 保留。修正的是 runner URI 定位，候选五源不变；后续同例集及最终带实际 wait 元数据的运行通过。这是验证工具首错，不是产品 RED；早期直接结果与最终七组 command 元数据分开保留。

## 保存范围与后继

[来源映射](project-usage-read-http-stage-a-verification-evidence/source-map.json)仅登记本次保存的原件及五个只留指纹的 dependency-list raw：作者 a03/a10、依赖恢复三组 offline list。保存作者 42、独立 39、依赖恢复 30，共 **111 原件/1488590 bytes**，另加这一小份来源映射；复用原执行/输出清单，不重建全树索引。未复制二进制、工具、cache、module 源码或重复的大型 list summary；独立六源快照直接由固定 Git 和原 inputs.json 恢复。原清单保持原文，不能把它们列出的所有对象都说成已归档。

本次文档归档只核原件 SHA、Git 输入、JSON、相对链接及空白，不运行产品或旧脚本。A 没有使用 listener、容器或真实数据库；B 的 handler/HEAD 执行、自然 2s/parent deadline/实际 I/O 尾部，以及 C/D 的 PG 当前权限/锁/游标/名称变化、默认 root、native TCP/keepalive、真实新旧回归和 README 均待后续验收。生产 Invocations 仍真正 nil，不新增 Runtime Facts/Skills/lifecycle；ready503、Object/tools/SPA publication 三停止和完整 D08–D28/E01 未完成边界保持。
