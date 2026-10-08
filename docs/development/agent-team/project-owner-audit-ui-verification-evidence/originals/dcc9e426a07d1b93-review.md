auditread01 原 FAIL 保留；task-owned 退休原件复核 PASS；焦点提案有限 STATIC PASS，STOP。

原 manifest `039dc5f8fbe2d81ba2864fd1e2f035749ecf918e0ffaeb07c7e02441668a988f` / handoff `c6285cce…` 的 55 个原件、463493B 均核 SHA/bytes，连必要固定源码与提案共 66 指纹。原 raw `3f2886ac7f75d0693c5d3c37d8b0b2c0582795f5fc2e0e7d94f500fa2c16bfb5`、result `174d380d6f8255da16f8414832c0a4c949763db08c9cdf3e21324227981bd22f` 不变。

唯一选定 top 实际 FAIL 22.90s；完整 fixture command exit1 / 83.363s。spec:1153 的 actor_id 焦点断言在 5000ms 内观察到 input inactive，原 raw 同时显示 aria-invalid=true，之前对应 invalid 断言已通过。没有旧轮 activeElement、完整 DOM、error-context 或截图；不能回填“实际聚焦 kind”，也不能仅凭源码推导排除其它未观察到的页面状态因素。

提案 `96707fcc…` 与 patch `fea0465a…` 静态合理。固定 API 对 service+actor_id 同时标记 actor_kind / actor_id，controller 逐项发布错误；View 把 kind 排在 id 前，并在当前 generation 的 nextTick 后定位首个 connected/enabled invalid 控件。UiField 把 error 映射为 invalid。工作卡要求保输入、定位错误字段及零请求，没有要求这个组合必须定位第二个控件 id。提案保持 id invalid 断言，新增 kind invalid 断言并严格要求 kind focus，与既定首错策略一致，没有放宽成任选焦点、删除错误断言或改变请求/预算。以固定旧 spec `f6482404…` 在内存应用唯一替换得到 `16a3f734f9ac236c8ca7d60640643e42daa2407dd308cd7ec068cc1010fac06f` / 67892B，与提案相同；没有写入产品或运行它。

退休原件支持：command 的直接 actual wait 1、4 个不同 PID/starttime 的 adopted actual waits，均与 observed-processes 对应；watchdog 完结并 join，无 monitor error、cancel 或强制 tail。固定 7 IDs（4 containers / 3 networks）在两次 cleanup 均 absent，baseline 保持、owned processes、Go runtime 与 browser runtime 均双空。TCP 是补充 host 轮询，尾部 37.371494747s 后于 14:01:58.969437Z / 14:01:59.173219Z 两次 active/TIME_WAIT/all delta 为空，没有据 TCP 发信号，也不是完整短连接或所有权证明。4 个新 PID1 containerd-shim zombie 为非 owned，未 wait，不能声称整机进程清零。Go raw 另有 Node child actual wait、proxy handlers join、preparation service join 和 root actual join 原日志。

input-before/after 原 bytes 相同，1176 输入摘要同 `811d6f98…`；driver `06cf03c3…`、原 grant `87c71a94…` 与运行记录相符。该新 Audit 组 legacy_dist_required=false，不把本轮作为 global59 动态扫描。未读取当前主机、正在运行的 Model 资源或当前资产。

已保留 19 browser GET sidecar / 9 原上游 body（16 list / 3 detail，18×200 / 1×400），逐 body SHA/bytes 和 grant 绑定一致；producer seed 不充 browser 请求。它们不等于最终 browser EOF/request-id 对照或 schema/client 重放完成。失败之后的零额外请求断言、finish 协议结果发布、实际 schema subprocess / public client 同 bytes 重放未到，0 截图；其它新轮、旧轮和独立四轮也不因本次 prefix 接受。后继需独立新实际 read 证明修正和后段，原 FAIL 永久保留。

详见 [result.json](result.json)，SHA `d06a323b5ef1726c0d81e98b13050984f1bb24f410fe2cd77d6795c75c08b365`。本次仅固定字节/JSON/源码复核，无 Node、Go、filegate、schema-client、browser、资源、网络或 Git 执行。
