# D04 Project Owner Audit HTTP — 作者技术交付

候选 candidate02 的 14 个技术路径完成作者限定自测；完整产品接受仍由 root 结合独立验收决定。实际源码与 14 项冻结快照逐项相同，生产五源和 schema 保持此前接受的字节。文档 #15–16 未获授权、未修改。资源窗口已明确归还，无活动作者命令或资源。

新增公开 ListProject/GetProject 在同一真实事务内先持 User SH、再持 Project SH，重新验证当前 Session/Owner；全行和哨兵检查、游标绑定及终局后的候选发布按卡实现。GET/HEAD 使用三秒发布及 I/O 预算并实际等待回调/Body.Close/服务终局，保留 HTTP tracked writer。根使用同 Store 和真实 Authority/Audit，未新增 initializer 或 worker。Unknown 代表是同一原读事务在同 backend 收到 C(COMMIT)+Z(I) 后丢失 ACK，保留原 Cause/Attempt，不是结果装饰或命令 receipt 确认。

离线组合见 [offline-summary01](../offline-summary01.json)、[旧精确发现](../offline-old-list-summary01.json) 和 [candidate02 受影响检查](../candidate02/offline-results.json)。普通/race、vet、两命令构建与实际图均保留原输入和 actual wait。wire 实际覆盖 31 动作及有限可选分支/反例，341 标准 schema 例通过；最大合成合法 200 行页为 598220 B，8192 B shape cursor 仅容量输入，真实 Sign/Verify 的 556 B cursor 另有 590584 B 页。原失败版本不删除，也不将有限分支映射称笛卡尔穷举。

[native 汇总](../native-driver-v01/result-summary.json) 为三轮、3 top/8 nested、9 listeners；direct3 与 adopted3 均实际 wait 返回0，全 TCP 含 TIME_WAIT 及 owned 进程各两次为空。无源变化的 native、生产和 schema 证据沿用，不因两个集成测试预期修正重跑。

[PG 汇总](../pg-driver-v01/result-summary.json) 保留九个真实窗口：原 new1 首红；candidate02 的 new2–new5 四新 top/12 nested 及 old1–old4 八旧 top/12 nested 全 PASS。每轮 full20 fixture、每 top 120 秒含 Cleanup、包6分钟；全部实际 wait、七资源和 owned/runtime 双清、输入一致，无强制尾部动作。旧组含 System Audit 查询/事务、应用根生产者、安全分页，以及凭据/Owner 更新/Usage/配置写入的默认根。

原 new1 只记录第82行 HTTP400，未记录当时 action，不能回填动态迭代。静态确定两个新测试误用 `model.provider.*` 命令名称，正确 Audit 常量是 `provider.*`；先前 STATIC 漏检如实保留。仅 #13/#14 改为正式常量并增加公开 action 诊断，默认 limit=50 和生产代码没有修改。原 raw、candidate01、最小 delta 及修复后的 format/race compile/精确 list/vet 均由 [result](result.json) 指向。

[资源及 daemon 集合](resource-daemon.json) 从原表核出 63 个不同精确资源 ID，而非仅计算9×7。原九轮 PID1 zombie 集合388→424，36个新增均为 containerd-shim，轮间差集为空；这些 daemon 不是本任务后代，未被本任务 wait，不声称全机清零。100ms 强制 root 返回和随后 fixture/client/backend 的实际退役分别记账，不能据后者倒称 root 全部 inner join。

[真实响应索引](safe-http-index.json) 固定 new2/new4 的四份 GET 原字节及两份空 HEAD；原执行内标准 Draft202012/FormatChecker 验证 GET，metadata 绑定实际 method/path/status/Content-Type/Length/request ID、项目、候选和 schema。未重组响应 JSON。

正式 Account 身份链与真实生产者参与主链；Skills 测试辅助的隔离边界保留。生产 D24/Resolution/Invocations 未绑定，ready503；Object runtime join、OpenAI tools 独审、SPA concurrent-publication 停止边界不变。本结果不表示 D04 全模块、D08–D28 或 E01 完成。
