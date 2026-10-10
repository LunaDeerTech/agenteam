# Project Secret Owner UI

本候选实现普通当前 Owner 的 Secret 创建、列表/详情、替换值、结果查证及删除页面，复用正式 Secret HTTP 和唯一 Session owner。输入值提交后清除；提交结果不确定时保留原命令身份，通过用户显式 Lookup 查证，不能自动重发或把历史回执当成当前 metadata。权限、CSRF、幂等及版本规则由正式服务保持。

当前为可恢复候选WIP，尚未正式接受，不合入main。前端产品、四个组件/API/Session测试和真实链源码来自 `27aa30b8`。交付方法在 main `18a27db5` 上只加入 Secret UI profile，保留 metadata、Human Install、Owner HTTP 及其组合入口；没有带入尚未交付的 Agent schema profile。默认 Project initializer、完整 Agent/F1 与生产 SPA publication 的原未绑定/STOP 不因本页面改变。

## 真实链与结论边界

唯一 Go 选择器为 `^TestProjectSecretOwnerWeb$`，app 包、1 top/0 sub；Playwright 只选 `project-secret-owner.spec.ts` 的 `[owner] Project Secret lifecycle`，1 worker、retries0、45s。Go 原120s包含cleanup，原Go6m、driver540s + TERM60/KILL3、TCP75及七资源双退役门保持。

fixture 通过真实 bootstrap/invitation/redeem/login 准备普通 Owner，浏览器登录取得正式 Cookie。默认 Project Create 明确 Unbound/NotCommitted；已有 Project 由显式 test-only 同 Store/真实 ports 创建，服务实际 Stop/Drain/Joined 后才启动浏览器。页面操作原默认根 Secret 服务，不替换领域授权。

真实链为 create→当前 list/detail→PATCH 提交→受控丢回执→Unknown→原 key Lookup→当前v2→delete。代理只在原 backend200完整安全回执、EOF及Close之后，将这一次 PATCH 表示替换为零正文的502 `application/problem+json`；它不是合法 Problem，也不证明回滚。消费者必须真实读到EOF、完成reader cancel/release与outer cancel后进入 uncertain。Lookup 不包含Secret值，成功后重读当前metadata。最终Go另核三次命令的history/Audit/event/commands/持久回执各恰3及无重复提交。

所有十请求均保 normal `requestfinished` 恰1、failed0及同原Response.finished(null)。原Request/XID、fetch Promise、EOF/Content-Length/bytes/hash、cancel/release/outer实际尾、Session原Promise typed结果/当前identity/notbusy及DOM必须同时成立。首次退休先固定Node资格并立即发起两个既有observer的seal，再join原尾；迟到不能升级首失败。借用 `knowledge-owner-read.native.ts` 仅定位同dist既有Session单例，不继承GET取消例外。标准Draft202012校验绑定原backend安全响应；OpenAPI本地Response `$ref` 通过原registry解析，不外部获取。

native05 原wholeFAIL且原进程和全部资源尾已关闭、窗口已释放：DELETE出现原requestfailed，另九请求正常，原UPDATE/GET本轮正常；消费者分组通过仍不替代原Node pending1及normal-only门，候选继续阻断、未正式接受。最终结论见 [D27](../../docs/development/work-items/d27-project-secrets-owner-ui.md)；原失败记录及唯一7KB安全首失败材料可从保留的 `ai/secret-owner-ui` topic `fd426edf` 恢复，不复制原件到本候选。不补推取消来源或放宽门，root已停止第六次全PG重跑。此前native01–04均保持原wholeFAIL：01为页面定位未到POST，02/03为完成证明失败，04十请求与首退休门通过但Schema helper未解析本地Response `$ref`。修后本轮十个原安全200响应离线校验通过；这不追认旧轮、也不替代最后Go提交事实断言。原详细失败与恢复材料保留在作者topic `27aa30b8` 的本路径历史及ignored原输出，不复制到正式交付。

## 复现

准备同锁前端/PW依赖、Go1.27.1、固定MinIO及原PG/Chromium/Python环境。前端使用正式 `web/vite.config.ts`，任务私有Vite配置只覆盖cacheDir；产物为 `output/ai/secret-owner-ui/web-dist-01`。编译app integration/race候选到 `output/ai/secret-owner-ui/candidate-02/secret-owner-ui-race.test` 并精确列举上述唯一top。首次使用该目录时先创建任务output父目录；每轮candidate/dist与源码保持冻结。

入口的独立闭包包括全部app同包Go/helper、所选Go/PW四源、前端生产文件、完整私有dist、锁定Playwright1.56.1实际运行包、Node/TS/Python/Chromium及 `common.json`/`secret-variables.json`。工具依赖可只读借用同锁实际文件；产物和Go/生产源码仍须普通文件。driver实际枚举和首尾重枚举判定输入，不以README或历史哈希替代本次输入。

以下仅记录获新明确资源授权时的恢复方式；当前不授权第六轮。使用全新两位attempt：

```sh
python3 -B .agent-state/secret-owner-ui/run.py --attempt 06
```

该入口固定上述candidate02/dist01与Secret selector；创建fresh `/tmp/psu06`、本域 `evidence-owner-06`/`native-06-control`，同进程检查至少5GiB，使用空Docker配置、私有telemetry off且清三旁路。它只启动原root-chain supervisor，不增加资源监督层或延长原预算。必须读取原Go/Node/driver/supervisor/outer实际Wait及资源、private/runtime/desc/TCP双尾后判断整轮结果。

仅方法控制可离线执行：

```sh
python3 -B .agent-state/secret-owner-ui/entry-controls.py
node .agent-state/secret-owner-ui/native-controls.cjs
```

交付组合只运行受影响的Secret入口6项、metadata6项、Human Install4项及Owner HTTP4项，共20项实际通过；这些是OS边受控的入口/闭包/逆投影检查，不代表真实浏览器。Secret增量严格剥离后两shared整字节等于main18a，未知selector/预算改变/缺或重复RUN-PASS-NodeWait以及缺任何原退出尾仍拒绝。前端产品测试、TS/build和native方法既有证据按相同输入复用，交付树未重跑产品Go或浏览器。
