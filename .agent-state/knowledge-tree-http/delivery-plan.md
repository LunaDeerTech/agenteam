# Knowledge 树命令 HTTP 有限交付准备

本文件是装配清单，**不是正式接受记录**。目标是向正式 main `4c1db71cf0f86cb6d6330167577944b0466c8664` 添加独立五 POST adapter；不等待完整 D12，也不接默认 root。当前作者 PG/native 均未执行，候选与产品冻结。

## 正式候选路径（18）

四产品、四纯测试、一个 native 测试：

1. `internal/central/knowledge/commandhttp/handler.go`
2. `internal/central/knowledge/commandhttp/input.go`
3. `internal/central/knowledge/commandhttp/io.go`
4. `internal/central/knowledge/commandhttp/wire.go`
5. `internal/central/knowledge/commandhttp/handler_test.go`
6. `internal/central/knowledge/commandhttp/input_test.go`
7. `internal/central/knowledge/commandhttp/io_test.go`
8. `internal/central/knowledge/commandhttp/schema_test.go`
9. `internal/central/knowledge/commandhttp/native_test.go`

五作者 PG 测试：

10. `tests/knowledge/owner_tree_commands_fixture_test.go`
11. `tests/knowledge/owner_tree_commands_test.go`
12. `tests/knowledge/owner_tree_commands_authority_test.go`
13. `tests/knowledge/owner_tree_commands_transactions_test.go`
14. `tests/knowledge/owner_tree_commands_unknown_test.go`

公开 Schema、可移植的实际 handler 向量验证器、正式规格与单行台账：

15. `api/openapi/knowledge-tree-commands.json`
16. `.agent-state/knowledge-tree-http/check-schema.py`
17. `docs/development/work-items/d12-knowledge-owner-tree-http.md`
18. `docs/development/agent-team/tasks.md`：仅增加本有限树命令结果一行，保正式只读 HTTP 及其他台账；未达验收前不写“已接受”。

前17路径在当前 main 均不存在。helper从自身位置解析仓库；使用已有 `jsonschema`/`referencing` 进行离线 Draft202012 校验，不下载、不启动 Go/HTTP。向量由真实本包 handler 与明确领域端口替身生成，原 JSON 不加入正式交付，不把它当 PG 输出。

## 与正式 main 的闭包

- 只读比较实际 `4c1db71c..HEAD`：B02（排除独立 read/command 子包）、Account、Project、Object、Secret、Audit、Outbox、Foundation、Identity、httpapi、postgres、共享 contract、迁移、go.mod/go.sum、common Schema 与原 commitproxy 均无差异。没有上游 WIP 生产依赖或新 SQL。
- main 的 `internal/central/knowledge/http`、`knowledge-owner.json`、三个 `owner_read_http*` 测试及其专属 helper 保留，不能整目录覆盖。新 commandhttp 不 import read HTTP；两个公开 constructor 都只消费正式 B02 Service/Account HTTPBoundary，没有 mutable 注册或生命周期副作用。
- 当前五命令精确路由不被已交付 read `HandlesPath` 命中；read 的 collection/detail/children/ancestors/search-titles 也不由 command `HandlesPath` 接收。未来默认 root 分派仍归另项唯一 writer，本次不修改 root/共享 HTTP。
- 三个 main read PG 源与本五命令 PG 源的直接顶层声明静态集合无同名项；这只是静核。正式装配后仍需一次必要 integration 编译/精确发现确认同包组合，不需据此重跑全部 B02/read 动态矩阵。

## 已有证据与尚缺门槛

| 层级 | 当前范围 |
| --- | --- |
| 规格与产品 | Runner独立接受；作者纯race 10top/86sub、Schema33及vet已过；独立实际adapter/digest/安全投影/原callback join两top九sub已过。未改输入复用。 |
| 实际入口 | Vars独立接受：作者132及独立37纯控制，精确PG四top十三sub/native三top六sub、旧入口逆差异/预算/完整尾保持；不代资源运行。 |
| 作者PG | candidate01已race-c/精确四top发现；真实Account/当前Owner、两锁序、同key/历史token、COMMIT Unknown及最终SQL/全部原资源尾尚未实际。 |
| 作者native | candidate01/driver已编译、精确三top发现；原2s/更早父期限、keepalive、Close失败、背压与断开实际尾尚未实际；Account/domain明确controlled。 |
| 新测试独审 | 产品审查不包含后增五PG源及native全部测试设计；两处peer-close收紧和入口已审，仍需未参与者核新增实际矩阵及实际结果。必要独立动态补集按明确剩余风险定，不用私有port纯控代真实权限/事务。 |
| 主线装配 | root精确复制17新路径＋tasks单行，核上游和已交付read保留；同包integration编译/发现及最终有限审查后发布。两作者窗口PASS可启动装配，不能回填旧失败或直接称完整D12。 |

## 不进入正式结果的恢复项

- 本树 current、本文、三个临时入口工具、selector-controls 与 ignored 候选/日志只留任务分支；正式验收源使用仓库原测试脚本，不交 WIP driver。
- Runner独审的 `.agent-state/knowledge-tree-http-review/{risk_test.go,run-risk.py,implementation-review.md}` 已在其远端任务分支保存。运行器绑定作者树/原cache且用overlay，当前不复制进正式 main；源码与有限结论可恢复。后继若确需正式动态独立测试，由独审者提供稳定可移植测试，并相应追加明确路径。
- 既有失败与未验范围留原 current/卡/日志；没有删除旧结果或重标成功。

## 已准备的作者入口

PG：本树原 supervisor `--root-chain`、原 `root_chain_driver.py`、`knowledge-tree-commands-race-01.test`，精确 `^TestKnowledgeTreeCommandHTTP(Mutations|Authority|Transactions|Unknown)$`，新 `/tmp/ktc-pg-01`。实际只读配置预检发现本树固定 MinIO 尚缺，root负责恢复同 SHA immutable 文件；不改配置绕过。原 Go6m/root540+60+3/七资源/TCP75与实际Wait/双尾/input保持。

native：本树原 supervisor、已编 `tree-command-native-driver` 与 `knowledge-tree-commands-native-race-01.test`，精确 `^TestTreeCommandsHTTPNative(ReadDeadlines|KeepAliveAndClose|WriteAndDisconnect)$`；真实启动另设未用 output 并获 fresh grant，原 Go90/driver105/outer123+3/TCP75不变。

两候选尺寸/SHA及完整固定 Go/off/cache 环境见本树 current。任何实际启动均须本人同process fresh ≥5GiB、UTC/bytes flush 与root新授权；没有本文件授权的自动执行。
