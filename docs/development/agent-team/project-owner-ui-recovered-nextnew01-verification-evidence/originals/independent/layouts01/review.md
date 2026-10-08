# layouts01：原件、视觉与退役独立复核

结论：限定 PASS；无本轮范围内必修。作者唯一真实 layouts01 已 PASS，本人只读复核冻结原件并逐张查看 8 张实际 PNG，没有重新运行浏览器、Go、schema/client、资源或探针。此结果不是独立实际 A/B、旧 16 组或完整 D27 验收。

固定交接 `layouts01-handoff.json` SHA-256 `fd7130557d2d86ec10997b81ae3b906cd54a0c852d5e9b60019f2c64f89b59d6`；59 个 run 文件共 995513 bytes 及 launch 逐一核合 SHA/bytes。原 raw `e877d1a137ccafb7770f9498adadbc07c3482411ee3bf161fa17ed79f00a5907`、result `1d2ed944a926fb70d32f58a2873d243863162adb2f18c6d305f3f7fb26f21108` 保持不变。复用 Git 367156d 的 browser-v5、Go04、已验 955 源图及 BB driver；未重扫工具闭包。

真实浏览器 1 case / 1 worker PASS 7.0s，唯一 `TestAccountProjectOwnerWebLayouts` PASS 14.31s，direct command 64.058s / exit 0。冻结源码、Go completed 握手与原 PASS 共同证明：键盘打开 Project 和 Settings，键盘保存并确认，取消修改，离开确认内获焦、Escape 关闭后原链接恢复焦点且保留草稿；实际冲突后先重读再显式采用当前值。随后完整经过 8 布局矩阵、archived readonly/保存禁用、注入下一次读取失败后的错误页、独立 Cookie context 空列表、无 Debug 路由与资产断言，以及最终 `verifyBodies()` 和 `complete()`。没有另存全 DOM 或逐动作 trace，不能把这些实现绑定证据称为额外 DOM 原件。

16 个闭合十字段安全侧录绑定本轮 source_run/input_hash、唯一 X-Request-ID 及 9 份内容寻址原 body；没有请求密码、Cookie、CSRF 或命令 key。原 schema 子进程 status 0 / stdout 16，覆盖全部 16 响应。原公共客户端同字节结果为 list 2、resolve 2、get 4、problem 0：浏览器 GET 为 005/006/007/009/013/014/015/016；001–004 是 setup，010 GET 与 011 PATCH 是 IPC 正式 update helper。008 是浏览器 PATCH 200（version 2），012 是浏览器 PATCH 409 `VERSION_CONFLICT`；这两个写响应仅计 schema 与实际 UI 行为，不计公共 GET 客户端重放。009 body 与 008 同，013 与 011 同；014/015 同一 archived Project。

客户端验证读取原 body bytes，用响应 request ID 匹配运行中的原生浏览器 URL/Owner/status，再给正式 API 的 List 参数、Resolve username/project_name/Owner、Get project ID/Owner。该 Map 未逐请求持久化，所以此处依据固定实现、原始响应与实际 checker 成功，未声称另有持久 URL/Owner 网络 transcript。注入的 read-error 属 proxy 故障，不冒充额外正式 Project Problem；archived 为真实接受后准备的终态事实，不代表生命周期参与者已运行。

8 图的 SHA、文件尺寸、PNG 尺寸均与交接相符，并逐一以原尺寸查看：

| 图片 | 可见区域结论 |
| --- | --- |
| light / 1440×900 / no-preference | PASS；身份事实、表单与动作排列清楚，名称蓝色焦点轮廓可见。 |
| light / 1440×900 / reduce | PASS；文字与控件边界可读，未见遮挡或横向裁切。 |
| light / 390×844 / no-preference | PASS；导航换行、设置栏目折叠、事实纵向堆叠、Owner ID 换行正常。 |
| light / 390×844 / reduce | PASS；名称焦点及提示文字清楚，已捕获内容无横向裁切。 |
| dark / 1440×900 / no-preference | PASS；深色背景上字段/辅助文字/焦点轮廓可读，动作可见。 |
| dark / 1440×900 / reduce | PASS；同布局可读，未见遮挡或横向裁切。 |
| dark / 390×844 / no-preference | PASS；纵向堆叠、身份值及焦点轮廓可读。 |
| dark / 390×844 / reduce | PASS；可见区域可读；与同尺寸 default PNG 同 SHA，属于静态画面一致。 |

截图实际使用 `fullPage: true, animations: "disabled"`。390×844 图只包含内部滚动容器上段，描述下部及动作在画面外；不声称截图展示全部移动端控件。窄屏每组合实际检查了 html 主题、系统/Project 导航、aria-current、基本信息、document/body/main/form 横向溢出、名称焦点和焦点样式、reduced-motion 媒体值。键盘动作主要在矩阵前执行，readonly/error/empty 在矩阵后；它们不是 8 图分别展示的状态。静图与媒体值不能证明动画过程或原生 zoom；未额外测色彩对比数值。

direct PID 219440/starttime 1203932 实际 wait exit 0；4 个已记录 adopted Chromium/crashpad 身份逐个 actual wait exit 0，watchdog complete/joined。原 4 containers + 3 networks 的 7 个精确 ID 两扫 absent，owned processes、runtime、browser runtime 两扫为空，baseline 不变，monitor/cancellation/forced actions 均零。TCP 辅助观测 39.384670239s 后两扫 active/time-wait/new-host rows 均空，末扫 06:51:09.406069Z；不当作完整短连接轨迹或所有权证明。新增 PID1 containerd-shim 219815/220298/220622/221223 单列为非 owned、未 wait，不能称全机器无进程或零僵尸。

轮内 input-before/after 同 SHA `af0a368fa712ae80c72e6edf0ac19ecaa6914b59c33ce5b1a189c2cb9b65a932`，且与前两轮相同。root 在 06:54:53.303006Z（末扫之后）恢复原 3 assets，与 exchange 原清单逐项一致；运行中固定 53 assets 的 source-after 不能因后续恢复而算漂移。restore 原件 SHA `196dd979a92efdb6fd1fa20e0e4342b0daa8dc53194c2e104e245b1e069d0827`。

STOP：只写本独立 scratch review/evidence/manifest；无活跃命令、reader 或本人创建的真实资源。详细原件路径、响应分类、每图指纹及退休身份见 evidence.json。
