# old16 首三轮与 oldprofile01 失败独立复核

结论：原 oldprofile01 FAIL 保持；前两轮作者实际 PASS 与三轮 owned 退役证据复核通过。当前证据把最早失败定位到测试的 CDP body 取证路径，未证明后端响应内容或产品状态存在缺陷，也不足以排除全部产品因素。本人仅只读冻结原件与固定 Git 源，无浏览器实验、Go/check、资源重跑或源修改。

批原件 `/workspace/scratch/owner-ui-backend/old16-batch01-result.json` SHA-256 `adc742d93b38b0d0fe9594a331b118d286cacc0f006f9983f3faa7ba438472a1`。三个正式 handoff 及全部 72 run 文件（900775 bytes）和 3 launch 均逐 SHA/bytes 核合。oldprofile01 handoff `765738c021c959007202a2c7ce65195932962d81ca0f4b43ab28e42d1d189d95`，raw `2d8dec1e104b2c11e333f63f191e923ef0d25589d8b5c723db1f6fea5568ee69`，result `1c7defd5344b5bfc2c1cffc081f818c84457b8a5f2aeff745d43594734b13356`。原文和 FAIL 未回写或覆盖。

| 原轮次 / top | 原结果与实际范围 | top / direct 秒 |
| --- | --- | --- |
| oldauthlife01 / AuthenticationWebSessionLifecycle | PASS；1 browser case 5.0s，登录/refresh/Session CSRF/logout 原断言，Go 确认对应 Session 被 logout 撤销且成功 Audit 数为 2。 | 7.60 / 53.563 |
| oldauthrevoke01 / AuthenticationWebRevocationAndExpiry | PASS；revocation 4.4s 与 expiry 3.1s 两次 browser。真实改密撤销另一 Session；expiry 是将一个 owned Session 行的期限移到过去后由真实 GET/router 观察，非等待自然期限。 | 12.42 / 64.721 |
| oldprofile01 / PersonalSettingsWebProfileAndAvatar | FAIL；1 browser case 在 body 读取处失败，未到 final result 或 Go 最终 Profile/Avatar facts。 | 8.54 / 58.746 |

固定 `tests/account-captcha-web/e2e/personal-settings.spec.ts` 来自 Git `367156d89773660c4a671a4b73d5ea7a16e24f50`，SHA `155bba1a7bf85ac5020c97b68dd45ade920b0e5016e6dd1f97d7a0202b1699ba`。实际原栈是 **441:27 `await duplicate.json()`**，错误为 `response.json: Protocol error (Network.getResponseBody): No data found for resource with given identifier`。不能把它记为 447 的 UI 断言失败或 `field_errors` 值不符。

`responseFor` 297–312 只等待匹配 method/path 的 Playwright response 并断言 status。此次已匹配 `PATCH /api/v1/me`，400 状态断言通过；随后 CDP 获取 body 抛错，`field_errors` 包含 `/username` / `ALREADY_EXISTS` 的内容断言尚未完成。既有 24 原件没有持久 HTTP body、当时 DOM 或网络 trace，无法确认该 400 的具体 JSON、页面是否已呈现 aria-invalid、浏览器是否取消/保留了原 body，也无法由本轮锁定 CDP 丢失 resource 的底层机制。

按固定顺序与失败位置，已经过登录/Session 与初始资料检查、首次用户名/显示名提交的精确请求值断言、成功提示、reload 后 `settings-admin` canonical 和显示名回填，再提交重复用户名。不能把这段顺序证据冒称额外 DOM/网络原件。441 的内容断言、445 起 aria-invalid、449 当前 GET 保持 username、取消/清空显示名、native 最终输入、三静态头像上传/回读、三非法头像拒绝、移除、storage 安全、`result(completed)` 均未完成或未到；Go requirePersonalFacts、8 次 committed writes/Audit、3 次上传和无 active avatar reader 的最终断言也未到。无额外 Project schema/client 或 legacy 原 body 检查结果可借用。

静态产品路径使用原生 Fetch Response 的 stream reader，读完后在 finally join cancel/release；它与 Playwright/CDP 的 `getResponseBody` 取证通道不同。此事实可说明失败的观察层位置，不能证明 cancel 正是本轮 CDP 失败原因，也不能单凭源码判产品成功。

必须补齐的是原重复用户名 400 body 的可靠取证，再让原断言及后段真实执行。建议的最小独立可审变更仅为这个额外 legacy spec 路径：给现有 `SettingsJSONTarget` 闭集增加 `{path:'/api/v1/me',method:'PATCH',status:400}`，将该调用点改用已有 `settingsJSONFor` 的原生 body 观察结果；保持 400、`field_errors` 两字段、aria-invalid、当前 GET、所有后段行为/facts、预算和重试不变。不得以吞掉 Protocol error、跳过 body、重新发一次请求或放宽断言替代。

既有 helper 58–184 的适用界限已独立读审：保留原 native fetch promise/Response，不等待 observer 再交产品；同源精确 method/path 的单次响应，实际 status 双核、calls==1、只 clone 小错误 JSON，UTF-8 fatal 与 600000-byte 上限，finally 恢复 fetch 并 join read/cancel/release。它原已用于 preferences409/login401/change-password400。复用它是取证修正方案，clone/tee 仍是有界观察，不可充当未插桩 stream/owner tail 证明，更不能提前声称后继 profile PASS。

该旧测试不在原 24 路径卡授权内，实施前须 root 明确窄额外路径；实施后另作正式 freeze、独立差量审查和运行输入闭包一源差量，再授新的 old-profile 真轮。当前 955 闭包/独立 A/B false freeze 不自动吸收后续修改。原失败留在 v04，不能绕过失败守卫启动余下组；本报告不自行授权修复、后继资源或预算变更。

三轮各 7 个实际 ID（4 containers + 3 networks）互不重复，共 **21**，并非原完整 16 组计划的 114。每轮 exact ID 两扫 absent，owned processes / runtime / browser runtime 双空、baseline 不变、0 monitor/cancellation/forced；3 direct 分别 actual wait exit 0/0/1，watchdog complete/joined。adopted 分布为 **4 + 8 + 4 = 16**，逐 PID/starttime 核原 observed 身份与 actual wait exit0；撤销/过期 top 含两次浏览器，不机械按每轮 4 计数。

三轮 TCP 尾分别 39.394405484s、37.384781927s、38.399376900s；最后两扫 active/time-wait/new-host rows 均空。前轮终清 07:13:02.150733Z 后第二轮才在 07:15:10.369423Z 开始；第二轮终清 07:16:53.834427Z 后第三轮才在 07:17:58.772437Z 开始。末轮终清为 07:19:37.294799Z。三份 Docker baseline 同，6 份 input-before/after 同 SHA `af0a368fa712ae80c72e6edf0ac19ecaa6914b59c33ce5b1a189c2cb9b65a932`，授权副本同 `332a391ee12bf8f4142242363ce8e725e2683b8358013814b74361f340f818c5`，driver BB 不变。

新增非 owned、未 wait 的 PID1 containerd-shim 为每轮 4、共 12，已逐身份与 owned 集和原 process baseline 分离：241473/241826/242193/242793；250910/251365/251733/252356；260608/260964/261314/262053。它们的完整 starttime 见 evidence；不称全机器无僵尸。TCP 是补充 host 增量观测，不是完整短连接追踪或所有权证明。

root 在 07:21:10.668955Z（末轮清理之后）按 reader-preflight=[] 恢复原 3 assets；restore SHA `43bf31225ededd1bb2417c73808911bddc7a777d4eeec5ede31b1ff49514dde6` 与 exchange 原清单一致。后续有意恢复不算运行时固定 53 资产漂移。第三轮 FAIL 后余 **13 组 NOT_RUN**、无后继资源，符合条件串行失败后退役再 STOP。

STOP：仅本 scratch review/evidence/manifest。前两轮只是作者实际执行加独立原件复核，未做独立实际 A/B；余 13、完整 Profile/Avatar、README22、完整 D27 均未接受。后续根因验证与修正接受须各按新正式 scope 进行。
