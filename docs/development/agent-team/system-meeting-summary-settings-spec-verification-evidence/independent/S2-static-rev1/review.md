# S2 rev1 完整独立 STATIC：需要小范围修订后冻结复核

固定卡 SHA `1734a1a7e0074db41cc1e3171f805e7107a35efc789d6c2b78437cfb54ae9772`，产品基线 c210d249。已完整读 §§1–6，并对照固定HTTP/boundary/json/native输出、根初始化链、selection API/Session/页面/导航以及旧browser fixture；未读取活动实现。31候选路径无重复，11本地链接/fragment有效。作者manifest58条来源中57条当前hash相符，唯一S1卡页首归档变化已由root授权且技术§1后逐字保持，不构成产品漂移。

结论：rev1有两个需明确修订的工程契约点，以及两处文档精确化；除此之外未发现阻断。保持原rev1冻结，交root协调作者单次修订，新freeze后仅差量复核。没有测试/资源/产品/Git写入。

## 集中必要修订

| ID | 冻结位置 | 实际问题与精确修订方向 |
| --- | --- | --- |
| B1 | §3.3，第91行，beforeunload并列“一次聚合确认” | 浏览器beforeunload不能等待自定义Dialog Promise，不能自定义实际提示文案，也没有用户确认/取消结果回调可据此原子discard。应用内route/logout/主动离页使用一次聚合自定义确认及捕获代次校验；beforeunload应独立规定仅依据两区dirty/未确认并集触发原生保护，不新建确认Promise、不退休任一槽、不声称收到确认结果；取消离开时草稿/intent保持。页面仍存活时不能预先清除状态。 |
| B2 | §2.1，第39–45行，有界新handler与清deadline | 固定 `http.go:dispatch` 在 route.serve返回后统一WriteJSON；`httpapi.WriteJSON`只写且可能仍在net/http缓冲，未Flush。必须明确新route early-return进入完整专用handler，helper拥有序列化、成功/Problem写入及HEAD headers的实际Flush，不允许它退休deadline后由旧dispatch再写。完整Write+Flush并检查ctx后才能停止/join callback、清deadline，再外层cancel；partial/short/flush失败或panic沿ErrAbortHandler，不能由外层Recover补第二份无预算响应。参考已接受Usage handler的Flush-before-reset语义即可，31路径已有足够接缝，不需改共享middleware/Usage helper。 |
| D1 | 页首第3行，S1改动计数 | “35技术路径中29实际改动”把README算进技术数；实际28技术改动+1 README=29总改动。分别表述候选35、实际28技术、README末件，总29，避免改变已接受证据。 |
| D2 | §5末，第141行，“Usage产品…只读” | #7/#8明确授权project_usage.go/test仅原初始化helper适配，与笼统只读冲突。明确这两文件只允许列出的helper顺序/参数/对应测试适配；Usage业务服务/HTTP/DTO/schema/端口/授权/预算语义和其他产品源只读，不允许借接线修改业务行为。 |

B1/B2均是已定目标的可执行工程修正，无新增用户决定；root已明确认可方向。原始卡及本报告保留，不让并发作者写入污染当前审查输入。

## 完整审查中已通过的部分

- HTTP契约与S1独立state/request吻合：GET三required闭合字段、显式null仅未配置、PUT不能clear、无Project/reasoning；独立ID/version，旧四用途字段/命令闭集与原lookup保持。3s/30s在RequireSystem前、严格body/framing/16KiB、HEAD/405/真实错误分类合理；新handler与旧dispatcher已存在的子path接缝可实现，B2把输出终局落实。
- 同kind历史语义完整：两receipt均按原selector ID/expected+1/kind/count0匹配，同User新旧共享key仍异义冲突，found/missing/failed不确认body或writer回滚；保留不可变原材料和仅显式重放。当前GET与历史receipt/草稿分离，保存后GET失败不撤销确认。
- API保持原第六依赖和12参数次序。原Selection类型不扩，原工厂组合新3方法可兼容既有调用；当前测试依赖通过工厂构造，无漏出的必改typed mock路径。单owner/14个selection action明确分类，两槽未决intent可共存、局部abandon隔离、全身份失效清两槽；原Session/CSRF、迟回/取消尾部边界覆盖。
- 同页独立form保留旧Dialog/四项PUT，plain chat选择不错误继承memory json_schema。初始读取有界顺序且普通失败不跳过另区；参考Model→Provider同signal/owner，有限5项，不目录预取。App/router只读仍可由原页面owner内部组合与聚合guard实现；检查中卸载的Promise/焦点约束与已接受模式一致，B1单独修正原生离页限制。
- 默认root明确Secret→Model→Summary→Usage原ctx，构造无IO、无新预算/隐式initializer；失败/Unknown不执行后继，不预置行冒成功。S1服务/所有迁移/Resolver不改，31候选路径可闭合HTTP/根/UI/测试，D2只澄清Usage helper例外。
- 真实验收含HTTP恢复/default app.Run/native keepalive、正式生产dist浏览器双intent/owner尾部、一次确认/身份/layout及旧六组；不把httptest/native/browser互相冒充。2m/6m/45s/workers1重试0及超预算先报拆分，完整TestMain/dynamic链与七资源唯一窗口要求可执行，不需在STATIC时启动资源。
- S3/D24实际生成、Resolver、生产Invocations/Runtime Facts、生产SPA及原三停止明确排除，ready503与整体模块未完成保持。已向S3独审按目录对齐：S2不写其contract/resolver/policy/plan或meeting_summary_resolution新测试；README最终归root串行交接。

## 证据与停止

固定16项实现准备见 `../S2-prep/inputs.json`；本轮卡/路径/链接/来源检查见 `inputs-check.json`。实际操作只有读取冻结/固定Git文件、hash、路径/链接/fragment与文本规则核对；不声称S2实施、compile、pure、native或真实PASS。只写本目录；当前无活动命令/资源/业务写入，报告冻结后停写等root新freeze派工。
