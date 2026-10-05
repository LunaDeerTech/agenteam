# 公开账号入口 rev1 独立规格审查

结论：**PASS（仅规格可实施性）**。对冻结 rev1 未发现须返修的确定阻断，建议主线程采纳精确 23 路径范围后另行授权实施。本结论不是页面、纯测试、浏览器或完整 D26 验收。

固定基线 `c54f73f3324caa11608d84e5d207141985eb6074`；卡 `rev1.md` SHA-256 `3f3e3a9624af7b73ed8a5082d74e85c6a7f3631286892352e209c2e0b05008d2`。独立逐字核验 inputs.json 中 31 个 Git 对象，全部 SHA/长度相同；23 路径为 12 生产、4 旧 mock 适配、7 新测试，现存 11/新增 12 与卡一致。固定 Account/app、Account/common OpenAPI、accountenv 相对认证提交 `9a710f2` 零差量。本结果不读取活动业务、Artifact 或台账作依据。

1. 五个正式 POST 均已存在，`http.go:65–85,141–173` 和 `csrf.go:133–148` 明确使用 Browser Cookie + 匿名 CSRF；两个 inspect 无幂等 key。`http_auth.go:221–399` 的响应与卡 §3 一致：邀请 201 没有 User/Session，申请 202 不披露账号存在/投递结果，reset 204 无 JSON 且不设置 Session Cookie。无需后端、数据库或迁移增量。
2. 卡 §2/5 正确识别真实接缝：`useSession.ts:317–410` 原 run 有 void/no-op，publish 清匿名状态，bootstrap 会改变 phase；不能直接把这些当 typed entry 结果或共存能力。新增门面仍归已有 owner，忙时在新 key/fetch 前拒绝，保留可见 30s 与实际尾部、稳定 Session epoch/私有匿名代际。四份完整 AccountAPI mock 的位置与实际代码匹配；其余 client 测试使用真实工厂，不需伪造第五份完整 mock 适配。
3. 原身份与原命令重放要求成立：`browser.go:39–100` 每次 bootstrap 新建匿名上下文；`link_commands.go:125–148`、`reset_complete.go:84–110` 按 Browser/key/完整值查询已有命令，早于活链接检查。故 Unknown 不能换上下文/key、先 inspect 或把 consumed 410 当 receipt。`http_auth.go:376–380` 在存在当前 Session 时另作邀请 inspect，卡 §5 已保留可能拒绝原重放且仍 uncertain 的边界，没有绕过 Cookie/当前授权的捷径。
4. Fragment 的入口、代际和释放责任可在允许的 account-link/router/App/page owner 中实现：history 构造前清除 URL，私有有界 current/pending 材料，旧 dirty/actual owner 先收束，迟到结果与 unmount 不清后继上下文。卡没有把 token 放入公共 reactive/DOM/history/storage，也没有承诺抹除 JavaScript 堆。`login?switch=1` 只显示身份选择；原 `performLogout` 的严格成功状态可供判断，void Promise 本身不是确认。普通登录四个安全返回目标仍闭合。
5. reset 204 已确认与随后的当前 Session 观察分离符合服务实现：`reset_complete.go:267` 只撤销目标用户 Session，浏览器可能仍带另一用户的有效 Session；同 owner 后续 GET 的 200/401/错误须分别处理。卡明确先清密码/token并记录确认，GET 失败不降成 Unknown、不重发 reset；实际尾部和安全全局身份更新不因页面放弃被跳过。
6. 验收范围与现有 fixture 可衔接：`authentication_web_fixture_test.go:124–158,178–188` 已提供正式同源 dist/API、真实 app 和绑定 record。新同包 helper 可沿随机 origin 正式邀请/兑换/登录，再用受限精确日志及只读 PG 核后态，不需修改旧 launcher 或 SQL 造身份。四个新真实顶层分别覆盖兑换、重置、多身份/显式切换、公开反馈/布局；原认证及设置各四顶层保留。纯 Unknown/过期/迟到、SMTP UI 分支与真实 backend_log/消费后 410 明确分证，不冒充真实 COMMIT 故障。原 race/count1/6m、top 2m、PW 45s/单 worker/零 retry 及资源双清门槛保持。

实施后的重点独立核验限实际风险：同一 owner 的忙时/actual-tail 与两个 CSRF 共存；Unknown 原输入保持与 context 失效；fragment/旧页清理及 switch 的零自动 logout；reset 204 后 GET 失败和另一身份保留；正式 fixture 不向产品注入身份/材料，不以纯替身宣称真实后端通过。当前不需要为上述风险扩路径或降低门槛，若实现揭示范围差量应先报告。

只执行固定 Git/本地文件读取、SHA/清单校验并写本私有报告。未运行 Go、npm、browser、Docker、网络或测试；未修改仓库/卡/业务，未执行 Git 写操作。生产 SPA 托管、完整 D26/D27、Object/Artifact 已知阻断均未改变。**all-stop**。
