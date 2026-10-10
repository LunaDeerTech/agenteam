# D10 Secret Variables Owner HTTP

状态：方案已获 Skills HTTP 有限只读审接受并由 root 授权实施；首片段四产品/四 pure 测试已准备，基础 pure/Schema 按限定版本组合通过，八技术源已获独立有限静审接受；作者真实 PG 组合已完整通过，native 与独立动态仍待。树 `ai/secret-owner-http` 以已接受的 Owner 库 `f9cc11c6` 为输入；正式连续 00028–30/core 由 coordination 组装，默认 root/Account 装配由 root 指定的统一写者负责。

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

本实例独占以下新增路径：

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

当前无阻塞 HTTP shape 的未决产品规则。00028→29→30/core 已进入隔离候选，正式交付仍由 root 统筹；本任务尚待 harness 并集、独立动态验收/真实窗口，以及后继默认 root 同 Store 构造与退出绑定；不跨写上述接入点。基础离线验证见下一节，PG 作者结果见§9；尚未 native socket 与默认 root。


## 7. 基础片段实际验证

- 首次 pure01 构建整体 FAIL：局部变量重名和未用 import，未执行业务；仅机械修正后编译进入测试。
- pure02 整体 FAIL：11 top 中10 top通过，包含24个实际 Secret Schema向量；唯路由测试查了中间件克隆前的外部 Request。仅将该观察改为实际 boundary Request，产品未改；原 FAIL保留。
- pure03 原路由与新增日志反例两 top race实际通过。结合未变的10 top，基础12 top/9直接子覆盖正常 CRUD/identity-only Lookup、严格拒绝/字节边界、safe receipt/whole-page/HEAD、owned material销毁、原调用/取消callback实际join和实际middleware安全日志。样例端口只证明 HTTP，不证明真实 Session/SQL。
- 相邻 ordinary HTTP 9 top/8直接子 race实际通过，26个原 Schema向量通过；ordinary源码和共用IO未改。这些是进程内controls，名称含 NativeCapability 的用例也没有socket，不能代native证明。
- 实际命令与终态见本树[检查点](../../../.agent-state/current.md)。Skills HTTP对基础八技术源实际diff有限静审接受，无产品must-fix；本节基础阶段未证明真实 Cookie/CSRF/Session；后继 PG 作者结果见§9，native与默认root仍未验。原 Owner 库30节点与定向独验继续按未变范围复用。

## 8. 定向真实方法

- 新增 `tests/projectvariable/secret_http_fixture_test.go` 与 `secret_http_test.go`，精确 `^TestSecretVariableHTTPBoundary$` 一 top 两直接子。复用真实 Account/Project/D04/Owner fixture：真实 Cookie/CSRF CRUD、GET/HEAD/List、安全历史与同材料显式覆盖；独立子在认证后 Body.Read barrier 调正式 Logout，create/历史 Lookup 均须原领域 Tx 拒绝、清 Cookie、Secret 事实不变。现 fixture 的 Skills 初始化仍为已披露受控能力，HTTP recorder 不证明 native。
- 新增 `secret_native_test.go`，精确 `^TestSecretHTTPNativeTransport$` 一 top 两直接子，总两 loopback listener。第一子顺序覆盖 2s GET 拒绝未完整正文后的原 Body.Close 排空、Lookup Body.Read、30s mutation Body.Read、更早父期限及同连接跨期限复用；第二子真实响应 backpressure，随后断开正在调用的材料写入，核实际 call join 后 Destroy/abort。控制端口不冒真实授权；两 listener/连接/handler 沿原 native helper 实际 join。需独立 native gate 和 root 新窗口，禁止普通离线运行开启。
- 三测试源已获 Skills HTTP 有限方法审接受；两个候选 race-c/精确发现实际通过，session18887→166067 actual0。GET Close 场景不冒正常 GET 领域查询耗时；fixture recorder 不冒真实 socket，native 控制端口不冒实际授权。后继 PG 作者结果见§9；native未运行，产品四路径、公共/共享 IO 和默认 root 保持不变。
- coordination 已完成两精确入口的共享 harness 增量，作者五个纯方法控制已通过、Skills HTTP 实际方法审有限接受，两个 driver 离线 race build 实际通过；不以编译或 parser 控制称完整真实尾接受。实际执行分别使用新证据目录/私有 telemetry off/空 Docker config 与同进程 fresh≥5GiB，原资源和 123s+3s supervisor/75s host TCP 尾保持。完整动态结果随后按实际填写。

## 9. PG 作者有限组合通过

精确 `^TestSecretVariableHTTPBoundary$` 一 top 两直接子实际通过。真实 Account Cookie/CSRF→Owner/D04/Project/Audit 验证 CRUD/GET/HEAD/List、metadata no-op、显式同材料仍替换、新 Session 历史 Lookup 与原意图重放、值与摘要无泄露；另一子在认证后 Body.Read 调正式 Logout，create/历史 Lookup 均由原领域 Tx 拒绝、清 Cookie且无新增 Secret 事实。fixture Skills 初始化受控范围保持披露，不代表默认 root。

该轮固定候选一次执行，Go/driver/supervisor/outer均实际退出0，PG两自有资源精确ID双退役、私有文件清除、后代/TCP双空、初末输入字节及枚举一致，wholePASS并释放窗口。精确会话/命令与原件位置见本树 current；未重试、未扩大原库30节点。native传输、未参与实现者独立动态与后继默认root/F1仍待，不以本轮作者PG关闭这些范围。
