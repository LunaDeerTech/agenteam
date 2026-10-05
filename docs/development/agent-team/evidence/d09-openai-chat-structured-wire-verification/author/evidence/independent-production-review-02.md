D09 structured wire production-review-02 窄复审（冻结）

结论：PASS，review01 的 F1/F2 静态阻断已由两份生产差量闭合，未发现新的确定阻断。允许继续原卡测试与后续独立真实验收；不是 structured revision 最终交付或真实 D04 验收通过。原 review01 BLOCKED 报告保持原字节，不回写为动态报告。

固定输入为 production-review-02 manifest SHA256 5d846a337a5b41b3e04621cfc51b6f2ebddf311f6974c98dd5b16264751f8351、delta.patch e293b5d74550f0492686119d3d95e15b5063379d43d053ca02b8b7dba867a50a；4 源逐 SHA/长度匹配，openai_chat.go/sse.go 与 review01 相同，仅 structured_schema.go/transport.go 变化。基线仍 ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7，复用此前未受影响的静审结论。

F1 已闭合：structured_schema.go:296–316 直接使用原 text 和游标，输出路径不再使用 Decoder 或整串 Unicode 预扫。325–358 使长空白和扫描按 1024 字节检查步长响应原 ctx，各 value 还检查 ctx/节点/深度；结束再次检查完整消耗与 ctx。497–612 对字符串逐 rune/escape 验证，plain string 不物化全文，key/enum scratch 分别只允许 256/1024 解码字节；原始控制字符、错误 UTF-8、非法 escape、孤立/错配 UTF-16 surrogate 拒绝，合法 surrogate pair 计算在 Unicode scalar 范围内。超长未知 key/enum 不能匹配任何合法编译项，因而早拒不丢合法匹配。614–679 检查 JSON number 的符号/前导零/小数/指数形状，第 129 个数字 token 字节即 limit，保留 <=128 字节原 string 视图；原整数指数有界判定未改。对象/数组的冒号、逗号、闭合与顶层 trailing 检查共同拒绝 trailing comma、literal/number 后非法分隔及尾随值。未发现越界推进路径或第二份同阶累计输出。

F2 已闭合：transport.go:246–255 先沿原 waitJoined 确认真实终局，再无条件检查 caller 和 attempt ctx。未更改 waitJoined/Joined、D04 或 Budget retirement；因此“实际 owner 已终局”与“允许发布成功”不再被混同。Result/Next 的共享成功消费接缝覆盖两响应模式，不引入新预算、worker 或提前释放。

作者冻结原红/修后证据已独立核查，并非本实例重新运行：

- red-f1-f2-01 manifest 91dae500a9a2514acba36721d1c9e962488e3d5a31f40fb72d8785c4e308479f 的全部 10 份原字节逐 hash/长度匹配 red metadata；fixed metadata 的同 10 项仅两生产变化，6 个测试/来源文件均相同。两次 argv/env 完全相同：Go1.27.1、-race、-count=1、-v、-timeout=6m、精确 TestStructuredValidationAllocationBounds 与 TestStructuredSuccessfulConsumptionRechecksCancellation 选择器。
- 原 raw SHA b92c9107e330258751fbf0bda7c1a3d683c9ca2993897df20e1d05189c8e8849，exit1，2 top/6 sub 全 FAIL。修后 raw e4eb8ab13d8ba72f6f92a0026cc20d6d4df98f3f6caae83022755da5443790d6，exit0，同 2 top/6 sub 全 PASS；未改断言、限额、barrier 或等待预算。
- 4 MiB plain string / 超长 number 的 validator 额外 TotalAlloc 实测分别 20,972,048 / 20,972,128 bytes → 0 / 64 bytes。测试排除了输入与 schema 构造，使用 1 MiB workspace 上限并核合法 string 成功、超长 number 为 limit；这些是该固定 pure run 的观测值，不声称所有输入的零分配或通用性能结果。
- JSON/SSE × caller/attempt 四个取消子例使用确定消费 barrier，修前 err=nil，修后 cancelled/wire_cancelled，且本地 owner Joined、Budget active=0。已核 localExchange/finishLocal 固定 ac5 helper：它们合成已完成的本地 Exchange，未构造真实 D04 Client 或 body/server；故此证据证明消费接缝和本地 owner 退休，不取代真实网络 actual join。

仍待原卡正式验收：新词法实现全部分支、过程中取消和精确极限的最终纯测试；原 text 纯/真实回归、混合 Budget；作者 10 源和完整实际证据冻结后，独立真实增量仍为 schema mismatch+usage/失败终态与受控取消 actual join/slot/material 两类。没有为本次复审扩大测试或方法。

本实例仅静审冻结生产与已结束 pure 证据，不读活动测试，不运行 Go/Docker/npm/browser/网络，不改仓库、业务或 Git。Object/Artifact 阻断及 Resolver/consumer/root 边界不变。窄复审完成，all-stop，等正式后续任务。
