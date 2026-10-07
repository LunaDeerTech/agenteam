# Project Usage HTTP Stage B 独立受控验收

结论：**B 限定独立 PASS，无需产品返修**。接受卡 rev1 #3/5 的 handler 与受控无服务测试结果；native 三顶层只接受编译/发现，不接受真实网络行为。C/D根装配、PG、真实资源与整卡产品验收仍未完成。

冻结两源：`handler.go` SHA256 `e56b1050691ef5c10b0649d4d06e94a6d4a819f832db85ed49e9faca0f0eebe9`；`handler_test.go` `6cb52c4553409b5edad5a7e641d55e966eea48b2a6ef452403e9ec24553102da`。作者 input01 SHA `1ac71b65f3566c07a4fb9e03250e82ac226836a134ac4a5289d4d17d6287d6b9`、final-input SHA `f99553429fffdb163086eadda94e28d2cbc1d78f80cff1b4955f5530faf5f3a7`、final-state SHA `54fba54d3a9d08f40553217346a05983a26f90f0dfc072d3c1565168adec3be0` 已核。仅复制两份B候选到本目录，其余使用既有冻结清单及固定Git。

A五源与common原字节未变。Summary四依赖最初为冻结候选；root随后独立采纳并提交 `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a`，实际四源字节匹配该提交，依赖门槛已闭合，无因提交重跑。新/变更29个产品图输入已分别匹配候选/固定Git；最终复用作者既有图实核291个产品路径及2个driver/binary原件，没有重造大型清单。见 `bind-result.json`、`inputs.json`、`final-check.json`。

实质审查确认：在HTTPBoundary/RequireHuman之前生成同一 `min(parent,now+2s)` context并传入三个正式调用；正式构造仅接具体Project/Usage/Account实例，私有受控接缝没有公开替代authority。原边界优先，三个精确资源GET/HEAD，其他方法405/Allow，canonical ID及A的typed query/完整投影复用。服务error及Unknown保留原Fault/CommitState，忽略同时返回的非空候选；完整Body.Close在成功/Problem发布之前。HEAD同样完成查询/投影/编码且不输出success/Problem body。实体防御读取最多一个实际byte，非EOF及零进展失败；原始长度/Transfer-Encoding拒绝。

正常与失败路径设置真实ResponseController读写deadline，取消callback实际完成后才清除deadline；Body.Close只调用一次，Write短写/error、Flush/error、Close/error、清deadline/error与已取消context均安全abort。成功完整Write之后仍同步Flush。unexpected handler panic转换为原生abort，外层使用既有全局RequestID/Recover；route Pattern安全模板、Problem固定instance及cause/query不外露。该静审和下列受控证据不代替真实net/http socket证明。

## 独立动态证据

实际命令：

```text
/workspace/toolchains/go1.27.1/bin/go test -race -count=1 -p=1 -timeout=35s -overlay=/workspace/scratch/usage-http-verification/b01/overlay.json -run '^TestUsageIndependentB' -v ./internal/central/usage/http
```

由自有 `../b01-run.py` 启动，45s外层预算、offline/readonly module、root允许的热build cache，自有TMPDIR，所有AGENTEAM环境变量清除。无socket/listener/native/DB/容器。实际exit0，外层5.141005s、Go3.251s，**4顶层/5子例PASS**；12个冻结输入前后hash相同，direct PID实际消失、adopted waits0。精确命令/env/hash/原始输出在 `independent-go01/command.json` 与 `raw.log`，probe为 `independent_overlay_test.go`，没有改仓库测试源。

- 自然2s及较早70ms parent：实等Context.Done/DeadlineExceeded，核自然parent仍有效、deadline未延长。故意同时扣住取消callback和Body.Close；先放Close后仍不返回、不清deadline，双尾部放行后才实际join。HEAD全程零service、零候选、零Flush，Close恰一次；自然耗时2.041988s、parent组110.966ms。这里超出到期点的受控尾部等待是join证明，不宣称任意不合作适配器都能在2s内返回。
- Body.Read已进入后取消：读尚未返回时handler不能退出或提前Close；放行实际EOF后仍不得调用Usage、不得发布/Flush，随后Close一次并清deadline。
- GET部分短写、HEAD成功Flush及HEAD Unknown-Problem Flush：扣住实际writer/Flush后取消，返回必须等尾部释放。GET只保留已写2byte且不追加Problem/Flush；HEAD无body但有完整表示长度，Unknown保持503；每组真实Close/尾部终局后清deadline。已发布部分响应无法撤回，此处没有声称取消能撤回已写字节。
- 同一个受控writer顺序GET/HEAD/GET：每请求当前认证/服务读，旧context退出时确已retire；恰好三对读/写deadline设置与清零，无旧取消callback改下一次deadline。这只证明受控顺序规则，不是native keepalive验收。

两源 `gofmt -l` 独立exit0无输出，证据 `independent-format`。未重跑无变化A schema/全部作者测试。

## 复用的同输入作者证据

b01实际race依赖发现、b02 race compile、b03受控pure、b04 native-list、b05 race vet五命令均actual0。实核命令metadata/raw hashes及前后输入一致；b03独立数出9顶层61子例，外层5.186793s，包含预认证boundary/auth的自然2s及50ms parent、同步auth/read/resolve/list/aggregate/close/write/flush尾部、GET/HEAD正常和Unknown、实体/能力/短写/Flush/Close/clear失败及安全route/error。b04仅发现约定三名：TestProjectUsageHTTPNativeSlowBody、TestProjectUsageHTTPNativeWriteAndClose、TestProjectUsageHTTPNativeKeepAlive。它们没有运行，也没有把默认skip计入通过。

作者原件路径/hash见 `final-check.json`，不把复用作者断言称为独立重跑。A的已接受query/DTO/schema结论保持；此前A缺依赖、冷编译/nonrace-vet超时及独立schema runner首错沿原报告保留。

## 实际尾部与后续边界

末尾实际 `/proc` 核作者B五组和独立Go组均无成员，所有B命令真实wait0、无adopted waits；独立format亦实际wait0/PID消失。历史A编译PID22765/starttime81512未join状态不纳入B清零，不声称它已被回收。自有写入、源码读取及命令现已停止，无自有服务或活进程。

仍待C/D：默认root同实例构造/Initialize/dispatcher、真实当前Session/Owner与PG锁和事务终局、名称改动/稳定ID与cursor、三新PG组和六旧回归、native实际TCP/EOF/写deadline/keepalive及每轮唯一资源窗口/双cleanup。受控writer不能证明TCP事务提交前零字节；自然HTTP期限也不能由PG1s lock_timeout替代。README末件继续待产品接受。生产Invocations真正nil、无Facts/Skills/lifecycle新增绑定；Summary已确定系统管理员统一会议模型，当前这份B证据仅消费其已接受纯契约，不代表Summary功能实现。Object/tools/SPA停止及ready503边界保持。
