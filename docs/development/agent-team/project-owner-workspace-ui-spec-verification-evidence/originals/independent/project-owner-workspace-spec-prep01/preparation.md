# Project Owner 工作区规格：独立准备

状态 PREPARED_ONLY。推荐及原 25 输入已读；固定产品 cc850b22、归档 b91cb89f。原 22 路径清单保留，root 已决定为唯一 Cookie/CSRF/intent owner 窄加 useSession.ts 第 23 路径；不是产品实施授权。未读作者活动稿或 Audit 活动实现，不给规格/产品 PASS。适用 design、verification、agenteam-vue-development 与 Vue testing blackbox/async 技能。

## 全文 STATIC 必核接缝

1. **单一 Session owner 与当前 Owner。** 固定 useSession.ts:1267–1272 的 runAuthorized.current 对 personal 以外 action 要求 admin，不能照搬 System facade 处理普通 Project Owner。窄加 Project action/context，但维持唯一 owner、当前 User/Session/epoch/CSRF、实际 transport finally 释放、全身份失效清理。禁止额外 Session controller、通用 URL/CSRF/token 导出。回归 S2 两槽独立 intent、局部弃置、Logout 与恢复竞争，不能用 visible timeout 冒实际收尾。
2. **三种读取 shape 与完整预算。** Owner List/Get 的 5 MiB 上限与默认 client 600000 B 不同，须 endpoint-specific 原始 UTF-8 流字节预算；完整合法页不得裁断。列表 active/archived、archiving、deleting 是不同闭集；deleting 无 description。Resolve 完整 ProjectRef 允许 deleting；Owner Get Project 不允许 deleting。共享过宽或过窄 decoder 均有风险。准确校验 description UTF-8 8192、normalized_name、timestamp/archived_at 关联、MaxInt64 version string。列表 cursor 原样、filter/owner 绑定；不新增 TTL 或 Session/limit 绑定。
3. **Update 恢复不是 Model lookup。** 正式 union 为 committed/result、in_progress、not_observed。私有原命令保留 Project ID、expected_version、字段 presence、原 body/key，空 description 与省略不同。当前 GET 相同值不能确认历史写；历史 committed Project 不能覆盖当前 GET 生命周期、名称/版本。not_observed/in_progress 不自动换 key 或重放；重放由显式用户行为和当前身份门禁触发。HTTP 丢响应、坏成功 body、取消与服务端 Unknown 各保来源，不把放弃跟踪写成回滚。
4. **稳定 ID 与路由。** 列表进入、独立 Resolve、后继 current Get 分层；每个迟到结果须同时满足当前身份/目标 ID/generation。旧名复用由另一个 Project 占用不能把原草稿/历史 receipt 迁过去。rename 后只按原稳定 ID 获取当前状态再协调路径。动态登录 return 必须是明确路由语法闭集，拒外部 URL、异常编码/分隔符、额外 query/hash；保原静态 Account/Personal/System 路由优先，登录后重新 Resolve/授权。未 Resolve 不能先加载子资源。
5. **时限、取消、导航。** 后端 Read/Resolve/lookup 2s、Update 30s 发布/I/O 与最多额外 3s 原确认退休不是同一前端等待承诺。可见超时后 owner 仍由实际 fetch/body/cancel 退休持有。单次确认弃置与完整身份切换不得清除另一域草稿；native beforeunload 无可用的确认完成回调，取消刷新应保页面/草稿，不能先销毁 intent。
6. **功能边界。** 仅 Owner 列表、双导航、空 Dashboard、基本信息编辑/原命令恢复；无 Create/lifecycle/ModelUI。管理员无 Owner 豁免。archiving/archived 只读，deleting 不进 detail/子页面。展示生命周期不能声称对象 runtime/participant join。
7. **发布停止精确隔离。** 新 Vue 及 task-owned Vite dist/loopback harness 不触 scripts/build-central-web.mjs、internal/central/webassets、app hosting、agenteam_web/embed、公用发布槽。SPA concurrent-publication 原停止不能 retry、改写、委派绕过。harness direct link/refresh 不证明 Central History fallback 或生产发布；当前发布 fallback 与新增多段 Settings 路径之间的未绑定要明写。若旧浏览器依赖公共 dist，只有后继明确资产单一 custody/原样恢复授权才能交换。
8. **真实依赖。** tests/account 的 app/account.go 闭包仍与活动 Audit 交叉。不同新文件不代表图独立；后继 Go 需要 Audit 已接受且全停的新固定基线，或 root 明确批准固定 cc850b22 最小隔离闭包。准备不消费未接受 Audit 产品。

## 后继最小独立验证设计（未执行）

- 静态对 frozen 23 路径及固定源，核 typed client、Session action/owner、router guard、所有未知结果迁移；检查 Scope 足以闭合而不绕过已有公共 owner。
- 纯客户端代表：合法最大完整列表、三种生命周期 shape、Resolve/Get deleting 差别、UTF-8/escaped raw bytes、三态 lookup 闭集、presence/expected_version/MaxInt64、abort 后 reader 实际退休。严格解析真实 safe HTTP 原 bytes + actual headers/status/target/run 与同 schema；合成样本不能冒真实 HTTP。
- 组件/状态代表：原 key/body 的显式 check→retry 与实际调用计数；跨 Project/身份迟到响应；旧名复用；current observation/历史 receipt 分离；用户操作验证导航一次确认、取消与草稿隔离。nextTick/flushPromises 不是计时器或实际 request join 证据。
- 实际浏览器后授：同源正式 Account/Project 服务 fixture（创建仅 fixture），列表→双导航→基本信息 rename/description/no-op、历史恢复、身份变更、取消/owner 尾部；桌面/窄屏、浅深色、键盘/焦点、overflow/reduced motion。保现有 Authentication/Personal/System/Summary 代表回归；harness 结果不外推 Central 发布。

本轮只有文件读取/哈希与自有 scratch 记录；无 Go、Node、browser、socket、PG、Docker、业务或 Git 写入，无在飞命令/自有资源。两次 scratch 索引准备脚本因误解 sources 索引 shape 分别 KeyError/FileNotFoundError，未改产品或输入文件；不属于产品/测试失败，没有中断或遗留进程。等待作者正式 frozen rev1 后做全文 STATIC。
