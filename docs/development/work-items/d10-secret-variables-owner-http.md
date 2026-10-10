# D10 Secret Variables Owner HTTP

状态：实现前边界方案，尚无本任务产品改动或验证结果。树 `ai/secret-owner-http` 以已接受的 Owner 库 `f9cc11c6` 为输入；正式连续 00028–30/core 由 coordination 组装，默认 root/Account 装配由 root 指定的统一写者负责。

## 1. 已定契约与最小结果

依据 [Secret Owner SPEC](d10-secret-variables-owner.md) §§2–3、7、[现有 OpenAPI](../../../api/openapi/secret-variables.json) 和 [Owner 库工作项](d10-secret-variable-owner-service.md)。正式 main 的 A 合同与 Schema 已交付；本树 SPEC 的旧“未独立审查”页首不是当前状态，不恢复该旧状态到 main。Schema 已明确三条路径，没有需要猜测的 endpoint。

本次交付可注入现成库的独立 Secret HTTP handler，消费既有六 typed 方法，不扩公共 Service/contract，不另造幂等、自动重试或恢复 worker。普通 `/variables` 保留原协议。默认 root、UI、Agent F1、MCP/Runner 和完整 Project 生命周期均另行接入。

令 P=`/api/v1/projects/{project_id}/secret-variables`：

| 路径与方法 | 现有调用与请求/响应 |
| --- | --- |
| GET/HEAD P | `ListSecretVariables(ctx, actor, project, PageRequest)`；安全摘要页 |
| GET/HEAD P/{variable_id} | `GetSecretVariable(ctx, actor, project, id)`；安全 metadata |
| POST P | `CreateSecretVariable(ctx, actor, CommandMeta, project, SecretVariableCreate)`；body 恰 `{request}` |
| PATCH P/{variable_id} | `UpdateSecretVariable(ctx, actor, CommandMeta, project, id, SecretVariableUpdate)`；body 恰 `{expected_version,request}` |
| DELETE P/{variable_id} | `DeleteSecretVariable(ctx, actor, CommandMeta, project, id)`；body 恰 `{expected_version}`，保留安全 receipt，非 204 |
| POST P/commands/lookup | `LookupSecretVariableCommand(ctx, actor, SecretVariableCommandLookupRequest)`；body 为 command/target_id，update/delete 加原 expected_version |

成功沿 Schema 返回 200；写入和 Lookup 使用唯一 `Idempotency-Key` 与 Account CSRF，create 无 expected。Lookup 不接受 request/value/semantic_digest，只有 committed/not_observed，后者不证明回滚；不计算普通变量摘要。显式写重放仍提交原 typed 意图由库与 D04 比较，不能用 Lookup 替代。

## 2. Account 与当前 Session

拟新增同包 `NewSecretHTTPHandler(*projectvariable.SecretService, *account.HTTPBoundary) (http.Handler, error)` 和 `HandlesSecretPath(string) bool`，供后继根分派。生产构造用具体类型，nil 拒绝；私有测试端口不成为新公共 Service。

复用 `HTTPBoundary.CheckRequest/RequireHuman/WriteProblem` 的 Cookie、Origin/CSRF、安全头及失效 Cookie 清除，不从 header/body 接 Actor、不设管理员旁路。精确 raw path、canonical UUIDv7、method/Allow、RequestID、query、媒体及错误遵循既有 HTTP 规则。新 handler 不接普通 variables 路径。

初次认证不是缓存授权：完整 Actor/Session 原样传库。`SecretService.readTx` 持锁后调 `Project.RequireOwnerInTx`，后者在原 Tx 调 `RequireCurrentSession`；写准备与 final gate 保留库的实际重验。initialized active 当前 Human Owner 可读写，archiving/archived 只读及完成历史重放，pending/deleting 拒绝。跨 Owner/Project/type 与失效 Session 不经历史 receipt 绕过。HTTP 不直读 Account/Project/Secret 表。

## 3. 材料与输出

metadata 恰八字段 `id,project_id,type,name,description,version,created_at,updated_at`。响应、错误和日志禁 value、密文、CredentialRef、DEK、值摘要/长度/尾号或 mask。Get/List/Lookup 不调用材料读取；写回执逐项校验 command、Project、目标、version、metadata presence 和 changed。列表整页验证 ID/Project/type、唯一性、顺序、数量及 cursor 后才编码，末项错误不能发半页。

按现 strict Secret DTO 拒 unknown/duplicate/null/大小写别名、无效 UTF-8、孤立 surrogate、尾随值和错误字段组合。值尽早进入短寿命 `SecretMaterial`，不造含 value string 的长期 DTO；清除 handler 自持 raw bytes/RawMessage，成功、拒绝、取消、panic 路径都 Destroy typed request，不保材料自动重试。标准 JSON 临时 string 与 GC 内部副本不能承诺擦除，诚实保留既有合同限制。若共用 decode 工具无法清除可控副本，限定在新 Secret adapter 处理，不扩改共享 httpapi。

日志只固定模板/request_id/code/state/计数，不写 body、完整 DTO、任意错误链或原始 URL/query。未知成员名也可能含材料，错误路径仅接受已声明 schema path，最终经 Account 安全 Problem 投影。

## 4. 预算与实际退出

read/Lookup 总 2s、mutation 30s，认证前开始并服从更早父期限；库原 Unknown 确认和 Stop/Drain 不变。复用现同包 native 能力检查、Body.Read/Close、Write/Flush deadline 和取消 callback 实际 join；缺能力/写失败 abort，取消不当作函数已返回。HEAD 执行同授权、查询和完整编码，正文空且 Content-Length 同 GET。

input/detail/receipt/Lookup 上限 1 MiB、list 5 MiB；核 65536 B value 最坏 escaping。分页默认 50/最大 100、cursor≤8192 B，C name/id keyset、Secret generation/签名交既有库，HTTP 不解签或重建游标。库已验跨类型、换 Session/stale/no-op/replay 按未变范围复用。

handler 不关闭共享库/Store。后继根须绑定同一个 SecretService 的 Stop/Drain；本构造和受控验证不代表默认 root 或完整进程退出通过。

## 5. 明确文件责任

本实例拟独占以下新增路径，方案确定后才开始实现：

- `internal/central/projectvariable/http/secret_handler.go`、`secret_read.go`、`secret_commands.go`、`secret_wire.go`：精确入口、安全读、strict 意图/材料寿命、typed 投影。
- 同目录 `secret_handler_test.go`、`secret_commands_test.go`、`secret_wire_test.go`、`secret_io_test.go`、`secret_native_test.go`。
- `tests/projectvariable/secret_http_fixture_test.go`、`secret_http_test.go`：既有真实库 fixture 增 Account Cookie/CSRF 与 HTTP 定向组合。
- 本文及本树 `.agent-state/current.md`。

ordinary HTTP、Account.HTTPBoundary、公共 httpapi、Secret/Project/Account 产品、公共 contract、迁移只读；直接复用现同包私有 helper，若必须改动先报具体差异。Schema 形状无需新增，其“contract only”说明待实际接受后统一更新。共享 PG/native 入口由 coordination 唯一写；只提供精确 selector/输入增量。app/account.go、app/security.go、root 构造/路由/Stop-Drain 归统一写者。

## 6. 最小验证与未闭边界

1. 离线受控：路由/Allow/strict 请求、Schema 正负/最大 escaping、安全 metadata/receipt/Lookup 绑定、Destroy/错误输出、whole-page/HEAD、Actor 透传、原 Unknown Attempt/Cause 和单次调用。明确 doubles 只证明 HTTP；运行相邻 ordinary HTTP 回归，不重跑全部库矩阵。
2. 定向真实 PG：真实 Account Cookie/CSRF → HTTP → Owner/D04/Project/Audit 的 CRUD、metadata no-op/覆盖/删除/新 Session 历史；另以 Body.Read 或显式 barrier 在认证后撤 Session，再进入领域 Tx，核拒绝/清 Cookie/无受保护响应与新增事实。Lookup 与原材料重放分别断言。库原 Commit Unknown/30 节点复用；若实际组合出现新缝隙才补对应故障。
3. 单独 native 窗：自然期限、更早父期限、keepalive 清 deadline、disconnect/backpressure，以及 Body/Write/Close/Flush/callback 实际返回；不以纯 ResponseWriter 代 socket 结论。
4. 未参与实现者有限独审及当前 Session/泄露风险独验。真实轮需 fresh 授权、精确 selector、原预算和实际 Wait/资源/runtime/desc/TCP 全尾；原 FAIL 保留。

当前无阻塞 HTTP shape 的未决产品规则。尚待协调的是正式 00028→29→30、harness 并集、独立审者/真实窗口，以及后继默认 root 同 Store 构造与退出绑定；不跨写上述接入点。本阶段只静态核对，未执行 Go、PG、socket 或网络。
