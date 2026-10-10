# D09 首个实际 text Runtime 链

冻结目标：`^TestModelTextRuntimePersistentWire$`，仅 `json_success`、`policy_deny` 两 sub；作者方法，不冒独立验收。源码是 `tests/model/runtime_persistence_test.go`、`runtime_native_test.go`。先 race-c 与精确 list，真实执行须 root 新授独占窗口。

## 真正提供方与唯一隔离面

- 每 sub 新 owned PG database，原 Migrator 完整 00001–31；不手建/灌入 Model call/attempt、Usage、Secret lease、snapshot、Project ready、Session 或发送事实。
- 同一个真实 `*postgres.Store` 注入 Account、Project、Model配置/Resolve、RuntimeAuthority/Runtime、Secret router/service、UsageAuthority/Writer、Audit与Outbox。Project 自构造时即注册唯一 Runtime AccessProducer，System Model 管理与 Project 调用 scope 分开。
- 原 Account Bootstrap、私有 recovery sink、Login/Authenticate 产生真实 Session；原 Project Create/现已说明的 test-only Skills initializer产生Project。后者只沿既有fixture正式初始化端口，不宣默认root initializer绑定。
- 唯一新增领域替身是尚无生产实现的 ConsumerAuthority：自有 fixture operation/input表持有原Project/Meeting/Operation/Call/initiator、完整消息、重算摘要、schema、固定BoundedRetry1/空categories、原Resolve与已同Tx绑定snapshot/lease。它不写任何Runtime/Usage事实；原锁内重读mapping，human阶段调用真实Account current Session与Project Owner gate；finalize/retire只认原登记 Runtime process/Invocation/accepted canonical关联，不伪用户授权。
- MeetingSummary沿正式platform选择：实际System Credential/Provider/Model创建与Selection update，原DiscoverResolve→同Tx ResolveModelInTx和consumer输入绑定。System Secret Resolve保Invocation RequestID且不带Project OperationID；D04 AccessDeny必须真实Project scope/原Invocation cause/原Operation，不能用System outbound跳过新checker。
- 新空 owned MinIO/bucket/spool上使用原Object Runtime.Initialize→实际ProcessGuard bound，Account/Project/Outbox process投影都从此guard读同一ID并委派ConfirmStopped。没有fake ProcessID/默认alive/零guard，不宣旧Object恢复或STOP解除。

## 两个场景判据

1. `json_success`：原D04政策只放行owned目标精确private IP/端口；原text adapter通过owned TLS服务收到一次JSON。真实Authorization来自planned Secret committed read；正文/响应/usage均核固定fixture值；相同Call/Invocation的Runtime canonical和原Usage ledger均sent+succeeded，sequence一致、attempt唯一、lease已释放、材料原cell不可再Use。Secret Resolve Audit原Invocation只一条，Project deny零条。
2. `policy_deny`：同一完整构造/Resolve/Secret读，原实际policy禁止目标；owned服务器请求零，响应为零值/错误。Runtime与Usage同一attempt明确not_sent+failed、unknown usage、lease释放、材料已Destroy；真实Project AccessDeny ordinal0/cause与service cause均Invocation、Operation关联准确，不把Audit失败当成功拒绝覆盖。

两场景只从SQL返回bounded事实或泄漏布尔值；正文/credential canary不能进入Runtime持久metadata/Audit或测试日志。Consumer自有输入fixture确实存文本，这是caller canonical输入，不冒Runtime存正文。

## 退出责任与复用

Chat原调用实际返回后检查同一材料cell已Destroy；Runtime Stop/Drain/Joined，原wire Budget Force/Joined及owned handler/connection真正退出；Project、Account、Outbox、recovery sink均实际Drain完成后才原Object Drain退休guard。任何前置owner未Joined保原FAIL并不释放guard；任何Object原Drain失败同样wholeFAIL。没有TTL判死、另起goroutine丢Close或回填成功。

拟复用既有七资源 `scripts/test-objects.sh → test-security.sh → test-postgres.sh` 原 owned fixture chain（PG/MinIO/outbound TLS真实资源），同共享监管器的原540+60、Go6m、Wait/nonce资源ID双退役、input枚举、private/runtime/desc/TCP双尾；本轮尚未修改共享入口，也尚未给该exact selector授权。root指定写者后只增这一字面和本任务输入闭包，不引入另一个任意命令执行器。

原core8/boundary15、duplicate/首错regression03、scopegrant01只按已验输入复用，不再跑整矩阵。首链尚不涵盖SSE/Unknown/持有I/O取消/重启接管，production Consumer/defaultroot/Agent F1仍unbound；这些范围不能由首次JSON/明确policy拒绝推导。
