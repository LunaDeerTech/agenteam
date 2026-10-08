# nextnew01：三轮条件串行终局独立复核

结论：限定 PASS。批原件 `nextnew01-result.json` SHA-256 `5a6bc16bbb0b6fedec32f6b942d52b34a189cb15d98eb0d30a49c21ecb645d43`；复用已经停止的 recovery01、identity01 独立原件 PASS，并完成 layouts01 原件及 8 图可见区域视觉 PASS。没有运行独立实际 A/B、旧 16 组或新的资源/检查，不据此宣称完整 D27。

| 原 group / top | browser / top / direct 秒 | 最后 TCP 双清时间 UTC | 后轮开始 UTC |
| --- | --- | --- | --- |
| new-recovery / TestAccountProjectOwnerWebOriginalRecovery | 5.1 / 11.63 / 56.354 | 06:44:29.985196 | identity 06:45:59.468637 |
| new-identity / TestAccountProjectOwnerWebIdentityAndOwnership | 11.0 / 18.31 / 67.830 | 06:47:47.055041 | layouts 06:49:24.604784 |
| new-layouts / TestAccountProjectOwnerWebLayouts | 7.0 / 14.31 / 64.058 | 06:51:09.406069 | 无后继 |

逐一核定三个 handoff 引用、原 command/cleanup/observed-resources/watchdog/TCP/input 记录。每轮唯一 exact top、4 containers + 3 networks，原 case 45s、top 120s 含 cleanup、包 6m、TCP 尾预算 75s 保持。三轮不同 nonce，共 21 个互不重复的精确 ID，每轮两扫逐 ID absent；3 direct 和 12 adopted 均有实际 wait，所有 watchdog 已 join、owned/runtime 双空、TCP 双清、monitor/cancel/forced 均零。每后轮确实晚于前轮完成全部退役，符合 PASS + retirement + same input 才进入后轮的条件。

三轮 Docker baseline 元数据一致；6 份 input-before/after 全部同 SHA `af0a368fa712ae80c72e6edf0ac19ecaa6914b59c33ce5b1a189c2cb9b65a932`。三个冻结授权副本同 SHA `43a01651a84d3bde69f6b5be732056e13d4925df0924c1f443257580a8a62e2e`，true 仅准 recovery/identity/layouts；三份 driver 同 BB SHA，未改源码、工具或闭包。root 在末轮最后 TCP 清理之后恢复原 3 assets，restore 原件 SHA `196dd979a92efdb6fd1fa20e0e4342b0daa8dc53194c2e104e245b1e069d0827`，与原 exchange 清单一致。保留作者视觉 pending 的历史交接，不回写原件。

三轮共 52 个安全 schema 响应、27 次浏览器 GET 同字节客户端检查（其中 identity 的 1 个 Problem）；setup/helper 与 mutation/lookup 的计数范围沿各独立报告，不冒称全部是浏览器 GET。12 个新增 PID1 containerd-shim 与 observed owned 身份不重合、未由 driver wait，按 4/轮单列；原有主机进程与基础服务不属于本批退役承诺。TCP 只是记录到的 host 增量双清，不是完整短连接追踪。

8 布局矩阵的 `fullPage: true` 是真实截图选项；390×844 的内部滚动内容未全部入图。逐图视觉只接受可见区域，键盘/readonly/错误/空态采用实际冻结断言证据，媒体值与静图不当作原生 zoom 或动画效果证明。recovery 的真实 committed 与受控其它恢复态、identity 的 Project→System 及两 Selection draft 聚合边界保持各原报告限定。

STOP：本批复核仅消耗固定原件，不修改作者历史、产品、仓库文档、全局脚本或资产。独立实际 A/B、旧域 16 组和完整 D27 仍待后续授权与执行。
