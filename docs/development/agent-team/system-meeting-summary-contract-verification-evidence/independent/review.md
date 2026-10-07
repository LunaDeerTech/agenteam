# Meeting Summary 四源纯契约独立验收

结论：**PASS（仅四源纯契约交付）**。未发现阻断或待修正产品缺陷。此结论不代表 S1 完整实现或验收；没有验证 migration/SQL、service 事务授权、canonical/reference 原子写、singleton 并集、旧命令历史 receipt 升级、删除替换、HTTP/client 或 Meeting runtime。

## 固定输入与边界

作者冻结 `/workspace/scratch/meeting-summary-contract-author/freeze.json` SHA256 `623f5d057b1dc677203a073b045babffcbbab1880749ce48dc0217c5c73a60bd`，报告 SHA256 `5a52f2ba3b18b215a281f7c8a2a4dbbe39e0a7d75a3100cf1f2e13776cc81dba`；基线 `ba7ce7296b7c08d976d3fc3ce1e1d0b727a3f04f`。四份 candidate 快照、工作区四源、完整差量与其 manifest 一致；64 个非授权仓库依赖的固定 Git 基线逐字比对及末尾当前指纹均一致，目标 contract 目录 Go 文件集合没有额外源。未读取或消费活动 Usage 两源。

| 源码 | SHA256 |
| --- | --- |
| meeting_summary.go | `02ffab6bfddda7417575d422a17cf31f563dbe234079fbd3ebd0e45192628e1c` |
| meeting_summary_test.go | `d029a73d9922b6a66a6acc6fa6fe3e0b9ad1d6de18899f1196d1919cf76525d1` |
| references.go | `b7f3b3eef67aa1d3ba707649489db5700c2e23e890bb02147038409affb49ae9` |
| references_test.go | `5619eb29438961afaf68eba73cfafff157f74ea82ca9f9ab9db5057dbda45ab9` |

以上均位于 `internal/central/model/contract/`。本审仅写自有 scratch；测试通过 Go overlay 读取作者冻结四源及独立探针，未修改冻结源、业务代码或 Git。

## 独立判断

- `MeetingSummarySelection` 独立 ID/version/可空 model 形状与已接受 rev1 一致；Validate 校验 ID、正版本及非空 model 的有效 ID；Clone 复制 model 指针；JSON 保留显式 null 与十进制字符串版本。纯结构校验不冒充数据库三状态不变量。
- `UpdateMeetingSummarySelectionRequest` 复用 CommandMeta 的 Human actor/key 结构校验，要求 System scope、合法 selection ID、正 expected version、必需 ModelID。没有 clear 或 reasoning 字段；当前管理员资格、模型可用性/来源/用途仍须后续 service 在事务内校验。
- ReferenceOwner 的变更仅增加精确 `(platform_selector, meeting_summary)` 配对；project ID 限制、必需引用不可清空、禁止 reasoning 由既有规则完整覆盖。既有 project_summary/agent/platform 四用途闭集保持；绑定与 replacement plan 未绕过 Validate 或冻结事实。
- `configuration.go`、`types.go`、`model/commands.go` 与固定基线逐字一致，旧 PlatformSelection、SelectionRef、Purpose、command 定义/语义生成代码未修改；没有提前新增 Summary SelectionRef 或改变旧 JSON 外形。既有库/HTTP/client 的旧类型无需在本四源交付中迁移。此项是源码兼容判断，不替代未来完整 S1 的 HTTP/client 联验。
- DTO/request 的值与指针格式化、实际 slog TextHandler/JSONHandler 均输出固定安全标识；未暴露 model/selection/user ID 或 command key。

## 有界执行证据

复核并复用作者已冻结的六个命令 result/raw 指纹；作者整包 pure/race 各 31 顶层、61 子测通过，vet 通过，不重复整套。独立探针仅执行三个新顶层测试：

| 独立检查 | 结果 | 驱动耗时 |
| --- | --- | --- |
| pure，`-run '^TestIndependentSummary' -count=1 -timeout=40s` | 3 顶层 PASS | 1.157s |
| race，同上加 `-race` | 3 顶层 PASS | 2.757s |

探针独立枚举 288 组 owner/role/project/clear/reasoning 闭集，同时核验 ReferenceBinding 接受域；16 份 Clone 并发独立修改不影响源；检查公开 JSON 不重建命令 authority、>2^53 字符串精度、六种非法版本编码，以及值/指针格式化和真实日志 handler。

保留一次自有探针失败：`independent-pure/` 的 authority 断言错误地把 JSON 反解被拒绝也判为失败；将条件修为“反解成功且 Validate 成功才算越权”，只改 scratch 探针，其余两项当次已通过。原探针保存在 `probe_test.initial.go.txt`，失败 stdout/stderr/result 均未删除或改写；修正后 pure/race 均通过。这是验证代码断言错误，不是产品缺陷。

Go 1.27.1 工具链指纹与作者一致；GOPROXY/GOSUMDB=off、GOTOOLCHAIN=local、GOENV/GOWORK=off、GOFLAGS=-mod=readonly。每次执行 driver 的 45s 总上限/42s 子命令上限，subreaper=true；直接子进程实际 wait/join，均无超时、终止动作或剩余后代。尾核两次按 PID/starttime 检查所有已跟踪进程均消失，driver PID 也均消失。未启动服务、监听、数据库、容器、migration 或后台任务。

`final-audit.json` SHA256 `21d15b46c22176739e71286e8caead89334012b7d59a67d77e6609fd337a4fdf` 绑定全部输入、工具链、独立命令/raw、原失败及尾核。`freeze.json` 绑定本报告与上述证据。至此本轮命令全部结束、四源未变、审查方停止写入，交 root 判定接收。
