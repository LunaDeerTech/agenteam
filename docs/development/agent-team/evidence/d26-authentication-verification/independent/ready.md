# D26 独立真实增量准备

**READY，未执行浏览器、Docker 或真实 API。** 只新增一个 Go 顶层 `TestAccountAuthenticationWebIndependent`、专属 Playwright config 与一个浏览器链；原 21 候选、fixture、harness 和 scripts 均未改。

基线 `457b1979c9d6563740543b2011eedc06cce34c71` 加最终 input05（manifest `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`）。私有稀疏闭包只链接已冻结作者输入，逐 SHA 核对最终 21 源及 8 项锁/依赖/正式 dist；Go 实际编译闭包和原 driver 入口按固定 Git blob 核对。567 个必要本地输入见 input.json，无完整仓库、node_modules 或缓存复制。

增量链使用正常完整 app.Run、正式 dist、真实 Account 接口与原公开题图 solver：

1. 原生 range 键盘提交偏离公开求解角度 180° 的 proof；要求 HTTP 400/CHALLENGE_INVALID，实际 disabled blur、题卸载及创建按钮焦点，登录仍受门禁限制。新题用真实焦点 Enter 创建和正确 proof，通过后回到登录焦点。
2. 内存核对失败/新题/verify/最终登录的同 key、完整邮箱/密码输入及原样 80 字符 pass；正式 login 返回与产品随后 GET Session 的身份一致。
3. 用先前真实 anonymous bootstrap token 发正常 logout 请求，要求 403/CSRF_FAILED 且同 Session 仍有效；随后页面真实注销必须携带已确认 Session 的 CSRF，返回 204、清 Cookie/表单/保护内容。Go 查询同一真实 DB 的 failed/consumed 挑战、精确 Session logout 撤销与两条成功 Audit。

敏感输入只在私有 fixture 文件和浏览器内存使用；断言输出安全状态或布尔，result 仅 ID/布尔。无 route.fulfill、延迟、COMMIT/网络故障、私有挑战答案或 Provider 访问。未重复作者已通过的生命周期/成功旋转/撤销期限/布局矩阵，亦未把分版本组合误称最终一次全绿。

原私有计划 `7a65c30c…` 将 CHALLENGE_INVALID 写为 403。实现前按固定 `internal/central/httpapi/problem.go:40` 与 `account_test.go:15` 更正为 400；root 明确采纳。旧计划原字节保留，这不是运行失败后放宽断言。

实际准备检查：Go1.27.1、GOTOOLCHAIN=local、GOENV/GOWORK=off、GOPROXY=off、-mod=readonly，复用既有模块与构建缓存；race compile 25.958s、vet 14.518s、spec TypeScript 3.772s、config syntax 0.208s，均 exit=0。完整 argv/env/exit/raw 在 logs/，未启动测试二进制。私有格式工具仅写三新 probe。未安装或升级依赖。

正式运行计划见 plan.json：原 scripts/test-objects.sh 与精确 selector，driver race/count1/6m、Go 2m、PW 45s/workers1/retries0。observe.py 复用已审 v2，仅改证据根和固定私有输入核对；原 live nonce/labels、PID/starttime、subreaper 实际 wait、双次 exact absence、原 baseline 与 runtime 检查保留，差量在 evidence/observer.delta.patch。实际运行及资源结论必须另取得，当前只编译和静态检查。root 授唯一窗口后方可执行。
