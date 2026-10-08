# 独立实际 A/B 计划（PREPARED ONLY，STOP）

依据 Git `f09f311256a88dd535a4315b65e378d3b3682465` 的 D27 Owner UI 卡 §9.2–9.4、五个 Go top、browser config/spec，以及已停止的 driver `bb666f55…` / 原 `groups.json`。本次只读这些固定资料并写本计划，没有执行检查、读取活动 run 或启动资源。root 已通报 edit02 失败；本计划不对尚未正式移交的该轮作归因，也不把作者 recovery/identity/layouts 当作已接受。

独立 A/B 必须由本独立负责人亲自启动下表实际轮次、取得自己的原件和退役记录。复核作者日志不是独立动态。执行前，root 须完成当前失败的正式处置与必要修复冻结，交接作者相应结果及唯一资源所有权；整卡仍要求作者五新/十六旧结果齐全。本计划不授执行，也不据此跳过待验作者用例。

优先保留每个原 group 的单 top，不增加组合 selector、不修改共享 groups。A/B 是结果组合而非一次扩大后的命令；每轮单独 root 授权，前轮实际 PASS、退役、输入一致后才申请下一轮。以下运行名均短于20字符，仅为待授权名称：

| 轮次/独立运行名 | 原 group | 精确 top | 计划重点 | 资源数 |
| --- | --- | --- | --- | ---: |
| A1 `ind-a-read01` | new-read | `TestAccountProjectOwnerWebReadAndNavigation` | 登录返回、分页/过滤、dotted直链与Resolve→Get稳定ID、非Owner/admin、删除中、旧名复用；真实同body读校验 | 7 |
| A2 `ind-a-edit01` | new-edit | `TestAccountProjectOwnerWebEditAndRename` | name/description显式保存、空值/no-op、重名/版本冲突、改名canonical、已确认后仅重读；必须到最终断言/complete | 7 |
| A3 `ind-a-identity01` | new-identity | `TestAccountProjectOwnerWebIdentityAndOwnership` | 正式Logout/身份变化、checking/新Session、route及迟到尾部、Cookie owner、Project/System/Summary聚合确认 | 7 |
| B1 `ind-b-recovery01` | new-recovery | `TestAccountProjectOwnerWebOriginalRecovery` | 真实提交后HTTP响应丢失、显式lookup/replay、原body/key私有比较、唯一命令/Audit/Outbox事实、历史receipt与当前值、本地放弃 | 7 |
| B2 `ind-b-summary01` | old-summary-recovery | `TestAccountSystemMeetingSummaryWebRecovery` | 原Summary恢复与独立草稿域回归 | 7 |
| B3 `ind-b-sumauth01` | old-summary-authority | `TestAccountSystemMeetingSummaryWebAuthorityNavigation` | 原Summary双draft、权限/导航回归，与A3新域聚合组成跨域证据 | 7 |
| B4 `ind-b-auth01` | old-auth-lifecycle | `TestAccountAuthenticationWebSessionLifecycle` | 原Account Session/Logout生命周期路径，补充新Project身份用例 | 7 |
| B5 `ind-b-smtp01` | old-smtp-delivery | `TestAccountSystemSMTPDeliveryWebTestAndRetry` | 原SMTP delivery写/恢复/重试域；唯一额外SMTP fixture代表 | 9 |
| B6 `ind-b-layout01` | new-layouts | `TestAccountProjectOwnerWebLayouts` | 键盘/焦点/冲突采用、空/错/只读、8布局及逐张视觉核对 | 7 |

B选择四个旧top作独立恢复/授权/会话/邮件写域代表，不能替代卡§9.3作者全部16旧top。未选旧域按适用的固定作者结果及既有独立受控证据分别记载；如果后续失败暴露共同影响，再由root明确增加受影响的原group，不机械复制全部旧域，也不静默扩大本表。

每轮沿原 `sh scripts/test-objects.sh -run '^(<exact-top>)$'` 的实际20包、TestMain两cmd、CGO0 fixture构建和正式root/API/同库服务。逻辑组合不复用上一轮数据库或会话：每轮自己的nonce数据库、端口、容器/网络、IPC及短browser runtime。使用一个独立目录中的原driver精确字节副本；此副本/准备freeze需另授建立，不能写作者正在使用的v03目录。原失败阻断和版本后继机制保持。

工具和实际Go/TS/Python/Playwright闭包复用最终接受的固定后继：955仓库源/66集合/1165显式输入，以及171 Python模块和20 schema资源；只绑定后续正式修复差量，不重跑graph或复制全树。Go仍绝对 `/workspace/toolchains/go1.27.1/bin/go`、offline/readonly/p1；PG两local digest、MinIO精确SHA、53-file ui-v1 dist不变。root独占global `web/dist`交换与最终恢复，原3-file目录完整保留；任何浏览器reader存在时不交换。此时的f09只供计划读取，不能提前认定为未来最终执行输入。

新case仍45s、workers1/retries0；原driver每top120s含注册Cleanup、原package6m、额外TCP尾观察75s。旧case内部预算原样保留，外层既有top/package约束不扩大；setup/build继续记实际耗时。每轮fresh≥5GiB并重建PID/starttime、Docker/daemon、TCP基线；7=4容器+3网络，SMTP delivery为9，SMTPSettings/Invitation outcome不另加SMTP。运行中记录实际精确ID而非预填ID；不把所有轮次拼成一个更大预算或并发启动。

成功与失败均须实际direct/adopted wait、watchdog join、Node/Chromium登记、proxy Serve/body、Project准备服务/ProcessGuard、正式root join；精确资源ID逐个两扫absent，owned PID、私有runtime及新资源双空，原Docker baseline不变，TCP/tcp6含TIME_WAIT差量双清。0monitor/cancel/forced错误、source/tool/filename-set/dist输入前后同才可PASS。daemon/PID1非owned shim逐身份单列，不声称已wait或全机清零。失败保原件、完成尾部后STOP，由root处置，不自动重试或继续后继。

A1实际运行固定schema checker，并按浏览器原生X-Request-ID将同一原body交公开client，核真实list参数、resolve地址和当前owner；fixture准备与IPC辅助请求排除浏览器计数。各新top的safe响应bytes/closed sidecar、status/media/endpoint/ProjectID/requestID/source/input/SHA原样保存，最终schema/client文件和complete必须真正产生。GET public-client回放、页面原更新/lookup解码、proxy保留完整body而页面收到截断三者分别说明，不把setup或全部sidecar冒充浏览器读。不得保存Cookie/CSRF/key/密码/请求payload。

B1当前固定实现只把真实lookup `committed` 计作真实状态；`in_progress/not_observed`沿已独立受控结果分列，不能声称真实PG三态或PG COMMIT ACKdrop。Skills prepared及辅助archived/deleting事实不接受D10/lifecycle runtime。无需新增proxy/probe或改正式协议来凑状态。

B6实际生成并逐张查看8张full-page图：light/dark × 1440×900/390×844 × no-preference/reduce，结合原键盘、焦点、溢出及只读/错误断言；无截图或未看完不得写视觉PASS。samebody与视觉均以独立轮原件为准，不能复用作者图片冒充独立执行。

后续每轮交付短冻结manifest、实际命令/raw/exit、精确输入、资源/进程退役、safe原body及限制。当前仅计划完成，STOP；无代码、driver、groups、全局脚本或资源改动。
