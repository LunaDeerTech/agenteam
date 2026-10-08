# T3 rev2 后端同 SHA 消费确认 — STOP

结论：可以按冻结 rev2 T3 实现新私有 Go harness 的协议消费者，未发现必须修改该草案的后端阻塞。此为 backend 作者的 STATIC 协议消费 agreement；不宣称正式卡、完整验收矩阵或双作者整体交接已通过，不授实现或执行。

冻结对象是 `freeze.json` 03ae01a8b8413ee5fc5d755f3a16dc53d065b103c50bf9ed8fcf62caf18f6d68，`t3-dto.md` 8c82af7cd694fa0b8432330394c304e64fd6e1aa61d943904f533bba8b00c408，`endpoints.json` 6c85ae889c8a86eb67695f2eb29773395b58db4f5bdfecc9c9182ff3e62fd9fa。原 rev1 9197fc97/83e3e652 及 rev0 8b78072a 保留；以 rev2 的精确 DTO 补足原建议，不回写历史。

1. 消费精确 9 action：counts、arm、control-state、release、snapshot、logout、archive-recovery-project、reference-fact、rename-reuse。请求恰 `{protocol,input_hash,sequence,action,args}`；ack 恰 `{protocol,input_hash,sequence,action,ok,result,error}`，成功 error=null，失败 result=null 且只固定错误枚举。逐 action 校验精确字段/union/null、next sequence 与 token，不接受泛型任意对象。17 operation 为 6 GET、9 mutation、2 POST lookup；lookup 无故障 effect，Session 仅原私有 before-dispatch hold。8KiB/64KiB/ack8s 等原界不变；非法控制触发已登记退休而非继续成功。

2. material 的模式键集、安全 seed、7 字段 ProjectLocator 及 navigation 两 System 草稿事实可由正式同 root 准备提供；不把期望子集当完整 HTTP DTO 或权限。navigation Owner 必须经正式账户准备就是 admin；其它普通 Owner 代表不变。snapshot 从同 root 有界只读 Tx 投影已登记安全列，在提交成功且 ctx 有效后一次发布；current、分族 Project 总数、逐 origin 安全历史、引用与 fixture_only 分开。配置仅 committed commands/model Audit+Outbox，Credential 仅 receipt/secret Audit；不复用旧 fixture 的 to_jsonb 全行或全库引用快照。历史 0 行不证明回滚，缺当前行不补造 deleted result；历史观察不替 UI 确认。

3. recovery 可消费排布建议如下，沿已有 Project 键，不新增场景能力或 IPC：

| selected origin tuple | 原有代表 |
| --- | --- |
| main / createProjectModelProvider / null | 配置 create 完整成功响应后 cut；显式 lookup 仍待决，再显式原 Execute |
| config_recovery / deleteProjectModel / 正式 seed ModelID | 完整删除成功响应后 cut；目标已删除仍走原 DELETE，不先 GET 修复 |
| credential_recovery / createProjectModelCredential / null | Credential create 完整成功结果后 disconnect；lookup 不清材料，显式原 Execute 才确认 |

这三个 origin 的 token 均为首个被 arm 选中的实际 mutation request token。每 tuple 只保一个 origin，后续同端点样本无论 key 是否相等都进入比较候选；不能先按 key 筛选而漏掉换 key，也不能把不相等比较解释为同义重放。对这些 tuple，后续步骤只安排明确的原 Execute；其它合法新意图用不同已登记 operation/target/既有 Project，例如已确认 Provider 的明确 update，不能再次在相同 selected collection tuple 发新 create 或覆盖 origin。读/Session hold 不占 mutation origin。

此表只证明协议能容纳原 Recovery top 的两族响应损失及已删目标原 DELETE 代表，绝不替代或缩减 rev0 §7/§9、rev1 的完整恢复/权限/导航/pure-controlled 矩阵。false lookup、坏 receipt、未知粘性、明确拒绝、key reused、局部放弃、两类归档差异及各独立 intent 原要求均保留；没有将它们宣称全部实测，也不借新 IPC 伪造 lookup。当前停止原文没有要求同一 case 超过 4 个独立 selected origin 或同 tuple 二次 selected 新意图。最终六个 case 的详细步骤仍须保持这些要求；若真实分配出现该冲突，实施前必须报告具体场景，不能重置/覆盖 origin、删场景、自动拆轮或扩预算。最多 4 origin 是每 case 限制，不把六个新 top 合成一个 case。

4. 正式 create 的安全结果可精确登记：上游 200 完整 EOF、Content-Type/长度/RequestID、scope/operation 和对应严格 receipt/result 全部通过准入后，在任何 hold/cut/disconnect 前原子登记安全 ID 及关系。配置 create 核 kind/resource_id/version=1/affected=0；Credential 核 credential_id/purpose=model/version=1/deleted=false 和请求所属 Project。Model create 的 provider 关系来自已登记正式请求，不由任意返回 id 猜测。相同原重放返回已有 ID 时验证一致，不重复追加或改变归属。失败/半 body/非法形状不登记、不落盘；不得用 snapshot 或登记事实冒浏览器收到。请求原 bytes 用实际消费时的有界 tap 捕获，不预读重构；两份完整 body/key/登记身份齐备才发布比较，否则 comparison=null。identity_equal 是稳定 Human+Project+namespace+kind，不把 Session 当命令 identity，也不授跨新 Session 保留 UI 材料。

5. 复用已接受 D1 的退出边界：先注册每个 outer handler 的独立 completion；ReverseProxy 的 body/write/ErrorHandler 实际返回或 unwind 后，才在该 handler 的 defer 记 finished/held_joined。双 token release 只收取许可；重复 release 只对应原 token，不能释放下一 arm。ctx.Done、Close、释放信号均不能提前计 join。所有未命中 arm、持有的 handler、控制 loop/child 和失败路径须在启动或 Fatal 前登记解除与实际 join。Go counts 的 browser_eof/schema_bodies/client_bodies 固定 null；Node 独立记录完整原生 EOF、typed client/schema 与 incomplete。最终核 Node 原子结果文件、对应安全原字节和 child 实际终局，不能以 server join 或快照提交代替 EOF、Cookie owner finally 或 UI 成功。

6. Credential 只由 Node 内存产生并经真实受保护输入提交；Go 仅受控内存捕获/比较，不新增传值 IPC。Credential value、原写 body/key 及其 digest 不进入 material/ack/sidecar/日志/截图；0600 登录 bootstrap 例外不扩展到 Credential。安全响应须闭集准入后才落原 bytes；拒绝未知字段/回显时先在内存失败，不先保存。fill 异常替换为固定安全消息，trace/video 关闭，材料清空且安全退出模态后才截图；不承诺 JS string/decoder 内部副本物理擦除。

仍为草案：Audit 整卡、T1 最终 shared-source handover、T4 正式卡/唯一 writer/README 及候选实际闭包/driver/独立验收/逐轮 grant 待后续。29 路径、17 operations、9 IPC、45s browser/120s top 含 cleanup/6m package/TCP75s/5GiB/7ID 原样保留。没有 Go/Node/业务资源/网络/Git 执行，没有仓库或正式卡修改；独立 runtime 的资源窗口未被触碰。原失败与三硬停、生产未绑定边界保持。
