# 独立 A/B 九轮真实验收

限定 PASS / STOP。本人在 root 九份单轮授权下实际执行 A3＋B6 九轮；每轮 fresh fixture、原输入门禁、真实浏览器/API、actual wait 和完整退役均通过。8 张本轮自产布局 PNG 已逐张查看，可见区域通过。没有用作者日志代替独立执行。

| 轮次 | group | top / browser 秒 | schema / native client | 实际资源 |
| --- | --- | ---: | ---: | ---: |
| A1 | new-read | 17.41 / 8.6 | 46 / 16 | 7 |
| A2 | new-edit | 12.74 / 6.3 | 19 / 7 | 7 |
| A3 | new-identity | 18.08 / 10.2 | 17 / 11 | 7 |
| B1 | new-recovery | 13.11 / 5.3 | 19 / 8 | 7 |
| B2 | old-summary-recovery | 12.71 / 8.6 | — / — | 7 |
| B3 | old-summary-authority | 16.05 / 10.9 | — / — | 7 |
| B4 | old-auth-lifecycle | 5.88 / 3.3 | — / — | 7 |
| B5 | old-smtp-delivery | 20.84 / 14.2 | — / — | 9 |
| B6 | new-layouts | 14.70 / 7.5 | 16 / 8 | 7 |

五个 Project 轮共 117 份 schema 校验与 50 次原生浏览器同字节公共客户端校验（list10 / resolve12 / get23 / problem5）。四个 legacy 轮没有这些 Project checker，表中留空。Setup、IPC helper、PATCH/其他写响应不冒充浏览器 GET client；原 body 与 request ID、实际 URL/Owner 的绑定由冻结实现与当轮成功 checker 共同证明，运行中 Map 没有另存为完整网络 transcript。

A1 实际覆盖读、身份/路由、dotted Resolve→Get；A2 覆盖显式编辑/改名、确认后当前读取失败及重读，原两处 harness 定位器修复已越过完整后段；A3 覆盖 Logout/Owner、checking/新 Session、迟到尾部与共享 Cookie owner、Project/System/Summary 草稿聚合。B1 覆盖实际提交后 HTTP 响应丢失、unknown 后显式 original lookup/replay、历史 archived receipt 与本地放弃；in_progress/not_observed 为受控状态，不是 PostgreSQL COMMIT-ACK 丢失。B2/B3 分别实际复跑 Summary 恢复与权限/导航代表，B4 是登录/刷新/Session CSRF/退出，B5 是自有 SMTP 端点结果与显式重试。

B6 矩阵前实际经过键盘打开/保存/取消、离开确认焦点与 Escape 恢复、冲突重读后显式采用；矩阵中断言主题/媒体、导航、焦点和 document/body/main/form 横向溢出，矩阵后经过 archived readonly、读取错误、空列表及 verifyBodies/complete。八图为 light/dark × 1440×900/390×844 × no-preference/reduce，8 个文件、8 个 SHA；字段与焦点可读，移动端纵向堆叠且身份值正常换行，未见可见区域的重叠或横向裁切。实际 fullPage:true、animations:disabled 不捕获移动内部滚动下部全部控件；键盘/readonly/error 不是每张图各自展示的状态，不证明动画过程、原生 zoom 或数值对比度。

每轮首 1165 文件门禁接受且 before/after 一致；九轮实际 digest 都是 6bb347c9ea8c32d19dd0176302019b64ddeaec778ad14dbd8011a1bc88436f55。复用原 final05 的 955 repo paths/66 sets/210 runtime files，以及 UI19、Go04、browser-v5、单 legacy personal 修正和当时53资产。未重扫源/Git/工具闭包；根 README 的 picture 与窗口关闭后的品牌改动不能借这些轮次声称已经验收。

实际 65 个互异资源 ID（37 containers＋28 networks；仅 SMTP9，其余各7），每个 ID 两次 absent；9 direct＋36 adopted 均实际 wait，9 watchdog join，owned process/runtime/browser-runtime 两扫为空，原 baseline 保持。每轮在前轮最终 TCP 双清后才开始；monitor/forced/retry 均0。37 个新增 daemon-owned PID1 shims 单列且未 wait，不称整机无进程或无僵尸；TCP 只是补充轮询，不是完整短连接轨迹。

末 B6 TCP 双清 2026-10-08T09:34:35.005724+00:00。root 恢复原3记录时间 2026-10-08T09:35:36.635591+00:00，晚于末尾清理；restore SHA d22c8a582143a991e05b831ab20dab4000636c4ebe32208042e3b4288021aae4，原3精确恢复，53留 stage。未再读取 current dist；窗口后恢复不构成运行中漂移。

封存索引仅摘要已有 429 个 run 原件，共 4556881 bytes，另9份 launch；未重跑 schema/client 或业务。每轮授权、outer 实际终局、raw/result、schema/client、退役与截图均由 evidence.json 和 original-index.json 精确引用。原作者新5、旧2＋14只作已接受结果的版本组合引用，不重新复审或冒充本独立轮次。旧 read/edit/profile 与技术失败全部保留。

本结论完成冻结 A3＋B6 计划的独立实际代表；不扩为 D10/生命周期参与者、Summary 生成/Invocation、生产 SPA、外部邮箱、停止任务、完整 D27 或后续品牌接受。资源窗口已关闭，无活跃 owned reader；STOP。
