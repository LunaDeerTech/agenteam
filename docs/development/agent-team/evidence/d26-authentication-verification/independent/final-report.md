# D26 正式认证前端：独立有限验收

**PASS，建议采纳本卡最终 21 源。** 独立验收输入是固定 `457b1979c9d6563740543b2011eedc06cce34c71` 加作者 input05（manifest `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`），不是任意当前主树。root 已提交推送 `9a710f272026b41ef69852bbeb41cb7670b500a8` 并确认远端一致；本审核人随后逐一读取该提交的 21 个 Git 对象，全部与已验指纹相等，记录在 evidence/accepted-commit.json。

采纳依据是版本化组合：core/UI/fixture 静审的确定缺陷均有保留原红和窄修；独立 pure 两项证明真实 typed API/controller 的晚 body/cancel 所有权及 Session 身份不匹配；独立本地 Chromium DOM 一次观察证明 disabled 原生失焦前提。后两者分别是受控 pure 和本地 DOM，不能称真实服务器或完整 Vue 浏览器验收。

作者真实覆盖为四个 Go top、六个 browser case：SessionLifecycle 复用 input03/auth-lifecycle-rotate-02，完整 RotateChallenge 复用 input04/auth-rotate-03，RevocationAndExpiry 复用 input04/auth-revocation-layouts-01，最终 LayoutsAndProduction 来自 input05/auth-layouts-02。包含失败的原 driver 仍保留，已到达的独立 PASS 按精确变化边界复用；不是最终输入一次四 top 全绿。作者 pure 67 distinct 来自全量 62 与受影响 22 替换旧 17，亦不是最终一次 67。最后 CSS 改动已真实通过 390px/200% 与剩余正式资源回退尾部。

本次新增独立 **一个 Go top、一个真实 browser 链**，使用原正常完整 root/正式 dist/隔离 PG+MinIO 与公开题图 solver。plan-v3 唯一执行：PW 7.8s，Go top 10.99s，account 12.035s，driver/observer exit=0。实际到达：错误 proof→HTTP400/CHALLENGE_INVALID 与真实 disabled blur/失败焦点；新题正确验证→80 字符 pass 原样、同 key 与完整原输入登录；login 与随后 GET Session 身份一致；旧 anonymous CSRF 正常请求被 403/CSRF_FAILED 拒绝且 Session 仍有效；页面用已确认 Session CSRF 注销 204、清 Cookie/保护内容；真实 DB 两题分别 failed/consumed、精确 Session logout 撤销、两条成功 Audit。raw SHA `b7fa8aba0508e49c1e729890c6246570d7d5c6925defc5feda5279569aa95a6b`，完整 argv/env/exit/输入见 evidence/independent-real-02。

独立首轮 plan-v2 的原 CDP 取证失败保持：401 后 response.json 报 Network.getResponseBody 无数据，未到目标链；原因仍未知，不能改记产品错误或首轮成功。v3 按 root 授权仅在私有 spec 中对阶段精确 arm 的同一 fetch response 做限长 clone，并以 X-Request-ID 与原响应绑定；保持原参数/Response/真实断言，保留原 CDP 单读与 requestfinished/failed/导航记录。最终八阶段原 CDP **本身全部成功**，copy 匹配且没有 requestfailed；没有实际走缺体替代分支。tee 改变背压、取消传播和微任务时序，因此此真实链不作为无干预传输尾部证明。原错误与这些限制详见 first-real-failure.md。

两轮独立真实资源均先实际清零后交窗：每轮 4 容器/3 网络都有活体 nonce/精确 ID，双次 absent；trusted 2 容器/4 网络字段不变，11 PID/starttime 消失、4 adopted actual waits，runtime/fixture tmp 空、owned command 最终 census=0。第二轮 567 固定输入前后及末检相等，源停止写入。最终 resource-handoff-final.json SHA `e04c5aa1634a361074e4ba2b2377e914e9d0e42b3d452fd2f7a7f7b2b2bb4f97`。无自动重试或增加预算。

准备过程也保留原件：私有计划 CHALLENGE_INVALID 403 笔误先按固定契约纠正为 400；两处创建挑战 200 笔误在运行前改为正式 201；v3 首次 TypeScript03 类型断言失败保留后仅修类型。Go/config 未变的 race compile/vet 复用，最终 spec TS05=0。原首红、各版本 probe/输入/计划和差量都进入轻量索引，不改原 raw 字节。

范围仍有限：正式 dist 由测试 fixture 同源托管，生产 Central 尚未据此获得 SPA 托管能力；实际 Vite 代理、真实服务器 COMMIT 故障/延迟 Cookie 竞争、完整 D26/D28 与既有 Object/Artifact/Project 等后继阻断均不宣称完成。没有 Provider 访问、外部账号、生产根装配或共享资源变更。

可复建方式：用 `457b1979...` 恢复运行依赖，再仅从接受提交 `9a710f27...` 覆盖清单内 21 源，并按固定锁构建 dist/核指纹；独立测试另叠三 probe。不要将接受提交整棵树误作当时实测基线。无需归档 input 符号链接树、node_modules、Go/浏览器缓存或二进制；完整准确入口见 external-locators.json 与 final-index.json。此报告及索引冻结后，本审核人 all-stop，无资源占用。
