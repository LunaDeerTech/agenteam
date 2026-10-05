# D26 最小独立真实浏览器增量（准备，未执行）

本计划基于固定 fixture02 `75e43c7d…`、已审 core02/UI03 与规格 rev1.2；作者首轮三 browser 目前停在 selector 前置失败，待固定窄修和两组真实结果后去重。这里只计划一个新增 Go 顶层、一个浏览器测试链；没有写 probe、申请新依赖或运行资源。

已覆盖可复用的边界：两项独立 pure 已证明真实 typed API/controller 下晚到 body/代际及 Session 身份不匹配；一次独立原生 DOM 已证 Chromium disabled 同步失焦。作者固定四顶层包含正常生命周期/真实 CSRF 拒绝与注销 DB/Audit、官方拖拽与键盘成功挑战同 key/80 pass/DB consumed、撤销/期限、布局与生产隔离；这些只在作者实际有效 raw 通过且输入相符后复用，不能因静态准备齐全而提前记为通过。

拟保留的增量是一条真实失败→恢复→安全退出链：

1. 用正式 dist、完整 app.Run、正常同源 fixture 与合法测试前置进入真实 ChallengeRequired。公共题图 solver 只计算公开图像角度，偏移 180° 后通过原生 range 键盘提交真实错误 proof，预期真实 CHALLENGE_INVALID，不读取私有答案、不 route.fulfill。观察验证期间 disabled/原生 blur，响应后题卸载且创建动作真正聚焦，登录仍被挑战门禁阻止，未出现首页；原失败 challenge 的 DB phase=failed。
2. 原页面正常创建新题，以公开图像正确求解、原生输入完成 verify，观察成功回登录焦点，80 字符 pass 经产品 typed client 原样送出。跨失败/新题/登录在内存中比较原意图 key 和完整输入是否保留；只落安全布尔，不输出密码/pass/token。成功后核 login 身份与产品随后 GET Session 身份一致。
3. 在已认证同一浏览器，用先前真实 anonymous bootstrap CSRF 向正式 logout 发一次正常无效 CSRF 请求，要求403、当前 Session 仍有效；再操作页面真实注销，内存比较其 CSRF 与已确认 Session 的 token，要求204，Cookie 消失、受保护内容/路由关闭，DB 精确该 Session revoked_reason=logout，相关安全 Audit 成立。此步在作者正常生命周期通过后只补明确 token 归属信号，不机械重跑所有布局/会话组。

该链不增加 COMMIT/网络故障、延迟代理或假 API。30s 忽略取消、旧 finally/Unknown 等责任沿已有效 pure 复用；正式超时异常不能在本卡临时扩故障注入。若后续作者真实证据已覆盖其中某项同样的新增信号，删除重复部分；若作者出现实际产品失败，先交其修复，不修改产品来让独立 probe 通过。

未来需另授私有 probe 与唯一浏览器/Go/PG/MinIO窗口。在自有固定 `457b197+最终21+同源dist` 小闭包追加独立 Go test / Playwright config / spec，不改候选21、原 fixture 或 scripts。复用正常 newAuthenticationWebFixture 与原公开 solver；原 Go 顶层2分钟、PW45秒、workers1/retries0、原脚本 race/count1/6m预算保持。精确源码/锁/dist、命令/安全环境/exit、首红、资源活体ID+nonce/PIDstarttime+实际wait和双清零都需原件；结束先归窗再排版。不宣称完整 D26、D28 正式托管、Provider 或上游 Object/Artifact 阻断关闭。
