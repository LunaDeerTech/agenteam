# D09 Project Model Owner read HTTP — 作者限定验收交接

作者 #1–13 实施及授权验证序列完成；候选06已停写，独立最终产品接受待 root 收取。README #14 未写，未执行 Git。最终JSON SHA `6f9b4dae3267e6fbe0cd1d30a67e0b4eada6ef8b8cc3366428a9d39636003e11`；13源及各snapshot见 `effective-candidate06.json`，其 SHA `4de418fb72147ad1c19f88ef1a7d490d2374cc88c299fcbe93c91eafa20f7c12`。

普通/race 每种：Model 精确纯测试 67 top / 270 sub，App 安全纯测试 16 top / 139 sub；schema 6正/33负。实际图、compile/vet、两cmd、list及各受影响增量均在JSON精确绑定。生产五源自 production03 未变；后续变化只在测试，不将旧结果冒称最终二进制重跑。

Native 组合：v02 keepalive、v02 slowbody、v03 writeclose，3 top / 13 sub。原 v02 writeclose 首红保留。新证据明确 Write/Flush 实际进入/退出、net timeout、实际 direct/adopted wait，以及所有 TCP 含 TIME_WAIT 退役后两次空；清理观察不计入测试业务预算。

| 作者 PG 轮次 | 终局 | 全链秒 | 自有PID | daemon shim Z |
|---|---|---:|---:|---|
| v03/new-projection01 | PASS | 64.534 | 92 | 208→212 |
| v03/new-authority01 | PASS | 53.637 | 83 | 212→216 |
| v03/new-bounded01 | FAIL_RETAINED | 59.289 | 81 | 216→220 |
| v04/new-bounded-rerun01 | FAIL_RETAINED | 59.973 | 82 | 220→224 |
| v05/new-bounded-rerun02 | PASS | 66.299 | 81 | 224→228 |
| v05/new-root01 | FAIL_RETAINED | 53.707 | 83 | 228→232 |
| v06/new-root-rerun01 | PASS | 54.751 | 85 | 232→236 |
| v06/old01 | PASS | 91.533 | 86 | 236→240 |

八轮全部实际等待、完整4容器3网络及owned PID双清、输入前后相同；无强制尾部动作/adopted wait。新增32个 daemon/PID1 shim Z不属于作者owned等待链，未wait；同期另2个非owned git Z单列，绝不声称全机零。最终旧10 top / 23 sub PASS后资源窗已归还。

成功版本组合为 projection01、authority01、bounded-rerun02、root-rerun01、old01。三次PG原FAIL分别是 execute 后ctx已过期、旧代理1MiB帧边界、测试错误found字段。新大型DataRow实际9437584B，同唯一连接完整透传→send/tag COMMIT→Z(I)→ACKdrop，later GET在另一连接；旧失败中没有的阶段证据不倒填。

16份成功轮安全响应原字节及实际X-Request-ID、status/Content-Type、target/run/input绑定见 `safe-body-pass-index06.json`。失败轮partial body及原raw分别保留。所有原STATIC/编译/native/PG问题与精确修复链列于JSON，不复制代码树、缓存、二进制或凭据。

8MiB不是DB前序分配/RSS保证；原app shutdown与内部join限制保留，私有proxy只对当前openStore ForceClose先行调用链作证明。无新mutation/Resolver/ResolutionInvocations/D24/三停止接受；README和最终独立接受由后续明确授权收尾。
