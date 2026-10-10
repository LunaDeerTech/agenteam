# Knowledge 树命令 HTTP 有限交付准备

本文件是装配清单，**不是正式接受记录**。目标是向正式 main `fb6ab7f492850bf1d3c025a59acbff381312a989` 添加独立五 POST adapter；不等待完整 D12，也不接默认 root。作者PG修后限定组合4top/13sub及native3top/6sub均已通过并完成原全尾；原PG01整体FAIL保留，最终未参与者收口及主线装配仍待，候选与产品冻结。

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

- 原作者对4c1的无共享改动闭包复用；再实际只读核 `4c1db71c..fb6ab7f4`：B02/read HTTP、Account、Project、Object、Secret、Outbox、Foundation、httpapi、postgres及原commitproxy仍逐字不变，前17新路径在目标main均不存在（27e166）。Runner新增共享面仅闭集Audit action/producer分派、限定System的RunnerIdentity注册、gorilla/websocket新依赖、00026和默认app的Runner owner；旧Knowledge分派/身份路径未减，00026沿原实际Audit CHECK加Runner分支。不能继续称整个Audit/Identity/SQL/go.mod逐字未变；本模块不回退这些新增main能力，不新增SQL。
- main新增默认Runner owner在原Account assembly先于Account/DB收尾，原路由由Runner静态闭集分派后回落；本adapter不import app或Runner、不默认注册，装配保留该owner及原root，不因本候选未消费的binding改动自动重编/重跑旧B02或read动态矩阵。
- main 的 `internal/central/knowledge/http`、`knowledge-owner.json`、三个 `owner_read_http*` 测试及其专属 helper 保留，不能整目录覆盖。新 commandhttp 不 import read HTTP；两个公开 constructor 都只消费正式 B02 Service/Account HTTPBoundary，没有 mutable 注册或生命周期副作用。
- 当前五命令精确路由不被已交付 read `HandlesPath` 命中；read 的 collection/detail/children/ancestors/search-titles 也不由 command `HandlesPath` 接收。未来默认 root 分派仍归另项唯一 writer，本次不修改 root/共享 HTTP。
- 三个 main read PG 源与本五命令 PG 源的直接顶层声明静态集合无同名项；这只是静核。正式装配后仍需一次必要 integration 编译/精确发现确认同包组合，不需据此重跑全部 B02/read 动态矩阵。

## 已有证据与尚缺门槛

| 层级 | 当前范围 |
| --- | --- |
| 规格与产品 | Runner独立接受；作者纯race 10top/86sub、Schema33及vet已过；独立实际adapter/digest/安全投影/原callback join两top九sub已过。未改输入复用。 |
| 实际入口 | Vars独立接受：作者132及独立37纯控制，精确PG四top十三sub/native三top六sub、旧入口逆差异/预算/完整尾保持；不代资源运行。 |
| 作者PG | PG01整轮FAIL保留；未变Authority3/Transactions4与修后candidate03的Mutations3/Unknown3定向整轮PASS组合为4top/13sub。PG03 9527→b8dcb4 actual0/117.055s，全部原Wait、七资源双退役、TCP/input齐；不称当前HEAD一次全量。 |
| 作者native | candidate02＋原driver的3top/6sub已整轮PASS，52400→0b3bce actual0/67.850s；原2s/更早父期限、keepalive、Close失败、背压Timeout/下界与全Close断开取消、原Wait/private/desc/TCP/input齐。Account/domain明确controlled，不称半关闭或PG权限。 |
| 新测试独审 | Skills已有限接受五PG与native六源方法及两处判据修正；Knowledge已接受Session时间前置和定向2top入口/61控。两实际原结果已交Skills只读最终收口；Runner实现独审2top9sub按不变范围复用，纯控不代真实权限/事务。 |
| 主线装配 | root精确复制17新路径＋tasks单行，核上游和已交付read保留；同包integration编译/发现及最终有限审查后发布。两作者窗口PASS可启动装配，不能回填旧失败或直接称完整D12。 |

## 不进入正式结果的恢复项

- 本树 current、本文、三个临时入口工具、selector-controls 与 ignored 候选/日志只留任务分支；正式验收源使用仓库原测试脚本，不交 WIP driver。
- Runner独审的 `.agent-state/knowledge-tree-http-review/{risk_test.go,run-risk.py,implementation-review.md}` 已在其远端任务分支保存。运行器绑定作者树/原cache且用overlay，当前不复制进正式 main；源码与有限结论可恢复。后继若确需正式动态独立测试，由独审者提供稳定可移植测试，并相应追加明确路径。
- 既有失败与未验范围留原 current/卡/日志；没有删除旧结果或重标成功。

## 已完成的作者入口与装配边界

PG01原四top首轮整体FAIL保留；修后PG03使用candidate03及唯一 `^TestKnowledgeTreeCommandHTTP(Mutations|Unknown)$`，新 `/tmp/ktc-pg-02`，整轮PASS。原Go6m/root540+60+3/七资源/TCP75及全部原Wait/双尾/input保持；未变Authority/Transactions仅按原实际输入复用。

native02使用原driver、精确 `^TestTreeCommandsHTTPNative(ReadDeadlines|KeepAliveAndClose|WriteAndDisconnect)$` 与新 `/tmp/ktc-native-01`，原Go90/driver105/outer123+3/TCP75全部尾齐，整轮PASS。两候选尺寸/SHA、原命令及完整固定env见current；无自动后继执行授权。

最终main装配仍由root唯一writer执行；只复制17新路径并最小增加tasks单行，不覆盖read HTTP或Runner新增root/共享闭集。必要同包integration编译/精确发现与未参与者最终有限审查完成后才正式发布。
