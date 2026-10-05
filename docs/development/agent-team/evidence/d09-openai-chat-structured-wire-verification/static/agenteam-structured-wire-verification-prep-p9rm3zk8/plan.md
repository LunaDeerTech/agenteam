D09 structured wire 独立验收准备（冻结；非验收通过）

依据 b6297143dce6a479d7c9ce7d463900288d5b0915 中正式卡，SHA256 747c879b4d930de949570a72ad6f3418bac3870f1ea23d639e19ce8a456799a0；与已审卡 103ef5b0 的差量仅为状态、职责及静审交接，技术规则和验收正文不变。生产基线仍为 ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7。本计划复用前次静审，不读活动生产或测试。

执行分界：先等待 4 生产文件（openai_chat.go、transport.go、sse.go、structured_schema.go）的原字节副本、精确 manifest 及相对固定 ac5 差量，再静审。作者 10 源和运行证据最终冻结后，按下面风险核纯测试/真实覆盖并决定最小补证。当前不复制源码树、不写 probe、不运行任何测试或占用 fixture。

最小风险集合：

| 风险 | 独立核查和必要证据 |
| --- | --- |
| 完整 schema 约束 | 核所有嵌套对象、array、nullable、enum 与严格 required/AP 联合生效；未知成员和错类型不能忽略。核原 schema 语义完整编码，编码与校验共享不可变私有输入；缺/多键、错误值、非法 Unicode/重复键/尾随 JSON 必须由真实响应验证拒绝。优先复用作者完整分支测试，再选一项独立反例，不能用先验 typed 拒绝代替响应验证。 |
| 数值与计算成本 | 核 schema 64 KiB/AST 1024/深度、输出 16 MiB/32 层/65536 value、128 字节 number 上限的真实边界和错误分类。数学整数含 1.0/1e2、大于 float64 精确区间的值；巨大正负指数及零值只作有界词法判定，无指数展开、截断或无 ctx 长扫描。作者应保留下界/刚超界、取消和成本证据；不机械另造全部极限组。 |
| 修订兼容/先验准入 | 旧 text-v1 拒 schema；同新 revision 的 text 成功且 native body 无 response_format、不累计 SSE 全文。schema 拒绝发生于 accept/Do 前；Start 后更改调用方 slice 不改变请求和规则。核原 text 纯测试和 3 个旧真实顶层原样回归，不能仅以旧函数未改证明兼容。 |
| 失败终态与 usage | JSON 必须 stop + 全文匹配；SSE 必须合法 DONE + 全文匹配 + actual join。验证 length/filter/refusal、缺 DONE、schema mismatch 都不发成功结果；usage 按实际观察保留，PartialOutput 只对应已交付 delta；错误不含 schema/value/material，错误一次后 EOF，无重试。 |
| ctx/Close/所有权 | 编译期间取消零准入；输出扫描、背压和等待 join 可被原 ctx/Close 唤醒。校验成功后到发布结果之间仍不能交付已取消的迟到成功。Body/Client/parser 的实际终局而非单个标志决定 slot/借用材料退休；超时保留 handle。新旧 revision 使用同具体 Budget，8/64 和原剩余预算不变。 |

默认独立真实增量仅两类；顶层名称和实现等源/测试冻结后确定，不提前造全套：

1. 通过正式 D04/Account/Audit/PG fixture，返回合法 native envelope，实际交付可解析但不满足 schema 的内容，并提供合法 usage。优先 SSE：消费非空前缀、观察 finish/final usage/DONE 后确认失败、可靠 usage、PartialOutput、零 StreamEnd 及随后 EOF；原 body/schema/material canary 不进入安全错误。必须有实际请求和服务器/客户端观察，不以调用前非法输入替代。
2. 正式 D04 的既有受控 hold/背压场景中取消。记录确实 Do 和实际持有的 body/handler，调用 Close/Force 的原有界路径，观察 parser/Client Drain、server handler/connection 和 Budget owner。取消不迟到成功，未 join 时不得释放 slot/Destroy 材料，终局后才可退休。必要的验证中取消/极限成本在纯层补证；不增加网络代理或任何暂停方法。

作者最终证据要求：10 源全 SHA/基线/差量及不可变来源 manifest；两 SDK 源可恢复原字节与固定 commit/blob/SHA；adapter 纯 unit/race/vet、integration compile、两 cmd build，3 新真实顶层与 3 旧 wire 顶层的精确 selector/argv/env/exit/raw。大于 512 KiB fixture 上限的成本测试只能按纯层记录。首红及每次修正输入保留，修前复用须说明生产语义未受影响，不能把 compile 或无实际请求当通过。

未来动态执行须由 root 单独交唯一窗口，固定候选/私有 cache/TMPDIR，原 driver/race/count1/6m 不变。首个非预期失败先保输入/日志归因；结束 owned exactID 双不存在、既有基线不变、进程/runtime 清零后即交窗口，报告归档随后。独立补证以作者有效覆盖去重，不重跑无关整树。

本次仅准备：无 Go/Docker/npm/browser/网络、仓库或 Git 写，无活动实现审查，无运行通过声明。结果仅拟验 structured wire 库；Resolver/Memory canonical schema/consumer、Secret Resolve、Invocation/Usage、生产 root/账号能力和 Object/Artifact 共享 guard 阻塞均不在本次通过范围。准备完成，all-stop，等 4 生产冻结及正式续派。
