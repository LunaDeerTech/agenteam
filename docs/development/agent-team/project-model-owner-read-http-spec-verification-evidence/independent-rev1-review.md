# Project 配置与安全 chat 目录 Owner 只读 HTTP：独立 STATIC rev1

结论：**NEEDS_REVISION，仅 B1；其余限定契约可接受。** 本结论审查 scratch 工程草稿的完整性与可实施性，不是正式卡接受、产品 PASS 或实施授权。

固定输入：draft01 `9c0062bee024794f85b00f3581aa3782e4f0040ada3110989313c591b563fafe`；产品 `901eb54605d293d4308caadd278c2c3a7ae1b824`；Update 仅接受规格 `03d1c107`。46 个 mapped 输入逐 SHA 相同；补核 cursor 两源码与已接受 Owner-read closure 一致。Update 活动 18 技术源未读，共享 account.go/security.go 只消费固定 baseline 副本。没有 Git、Go、测试、网络或资源运行，仅自有 scratch 写入。

## 必修 B1：没有时间到期语义

位置：draft01 §3，第54行“篡改/过期 token 都拒绝”。固定 `cursor/cursor.go:57–77,132–200` 的 header/payload 无 TTL/issued_at/expiry，Verify 无时钟检查；`cursor/keyring.go:15–19,28–60` 捕获已加载 current + key map；Model 的 instant 是 water/after 分页位置。

应写明：篡改/签名错误及 token kid 不在当前加载 keyring 时拒绝；仅改变 current_kid 而保留旧 kid/key 不使旧 token 失效，仍须逐页通过当前 Owner/Session 和原绑定校验。不新增 TTL、在线热轮换或库写权。作者未应用的 cursor-clarification01/proposed.patch（SHA80f56ce48a1eba08056298f2088d561a6fd95eb55f0744cd7fc46991c3acbbc0）方向正确；rev1 原问题仍未修复，需冻结 rev2 后差量审。

## 完整审查结果

- 五个 GET/HEAD 与现有五查询精确对应；不引入 provider filter、二次查询、HTTP SQL、CRUD/credential/lookup。新精确段分派可作为最外层路由组合，保留旧 Owner read/Update/Usage/System 路径；13技术路径足够，12新文件 + account.go，另1 README末件。
- RequireHuman 仅认证。Model readScope 在同 Store/Tx 获得锁，Authority 当前核 Session 并委托同一 Project Authority；后者核 Owner/initialized/lifecycle。非 Owner（含管理员）/跨 scope 隐匿404，未初始化/deleting409，archiving/archived读通过，与原正式 gate 相符。当前权限不能由 cursor 或早期认证代替。
- 原 cursor 的 User/Project/query kind/chat filter、水位/keyset/无 generation、同 User 新 Session 与变更 limit、每页权限均保持；配置列表含 disabled，目录单 JOIN 同 statement 的两层 enabled 与安全七字段不改。七个旧回归 top 名称均在固定接受源实际存在。
- Project 完整 Provider/Model DTO 的 scope、credential ID/null、动态配置对象和时间/version 合法约束与原 View 一致；目录精确七字段且不补 System 完整配置。能力十字段、非null数组、nullable正 int64 字符串、原枚举/关系/unique和无 efforts 业务数量上限均一致。新独立 schema可闭合固定对象并保留合法动态配置，标准schema与 Go跨字段校验责任区分可行。
- 8 MiB admission 在新增 Validate/DTO复制/Marshal 之前，len下界/checked余量/context/Go默认escape及RawMessage保守上界有完整要求，最终编码长再次核；合法大 efforts 反例与DB仅object约束成立。保守拒绝、无裁剪/重查、503 NotStarted只指表示尚未发布、前序库/DB/RSS不担保均明确。服务 error（尤其原 Read Unknown）先于输出 admission；超HTTP期限 abort，不能转迟到503。
- 新 handler 自持完整2s预算，覆盖 CheckRequest/RequireHuman、严格EOF、同步服务、编码、Close、Write/Flush、取消回调join及deadline reset。可复用同package helper并在新文件补私有实现；没有必须改公共框架或旧Summary的接缝。HEAD仍完整验证/编码与真实header Flush。受控writer和native证据、取消与实际join区分明确。
- root在 Update 产品完整接受后，利用已规定提前纯构造的唯一 projectUsage.projects 注入既有 Model Authority，并复用原 Service/Store/Account boundary。无第二 Authority、setter、新initializer/worker；原初始化顺序与 Update work/HTTP/Outbox退出门禁保持。未来 Update接受产物与规格有差异须先重核，不以旧overlay代最终联合根。
- native三top、真实PG四新top/当前事实变更/真实COMMIT ACK丢失/默认app.Run、原bytes+实际header/schema、必要旧回归，以及真正实际graph/TestMain/动态helper纳入都有可执行接缝。尚未执行，故不预判预算达标、资源清尾或行为PASS；45s离线、native自然2s、120s新PG top含cleanup等须按最终图分别授权。

## 后继实现的重点门槛（非新增规格阻断）

1. `meetingSummaryRequestIO.start` 现只设置读写deadline；草稿“缺 Flush 能力业务前 abort”仍须新 handler 的能力检查，不能只引用 helper 名称。预算/Callback 实际join保留，不能通过提前Flush探测而先提交响应。
2. 新成功输出须使用预编码的同一完整 bytes再写 Content-Length/状态/主体，不经旧无cap WriteJSON重复无界投影。所有晚期失败禁止第二Problem或部分成功承诺。
3. 8 MiB反例要证明前置估计顺序、checked溢出与真实escape，不能只看最终len(body)；PG前序库开销超期只可如实abort，不扩大成RSS保证。Read Unknown与超大候选组合须保原error优先。
4. 真实撤权须有目标Tx/锁屏障位置；辅助SQL只称受控持久事实。默认root验证必须使用Update最终接受产物和真实默认app.Run；不推称Project创建/Secret/Resolver已开放。

建议 root 仅授权作者做 B1 文字修订并新冻，其余技术体和13路径不变；修订差量通过后再决定正式卡。实施权限仍等待 Update 接受与独占共享文件交接。
