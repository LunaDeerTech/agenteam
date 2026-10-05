# 前端基础验证记录

日期：2026-10-01。环境：Node 24.20.0、npm 11.19.0、Chromium / Playwright。此记录针对公共组件和 Debug，不代替业务页面验收。

以下至“保留边界”为该日期的历史记录，原结果与当时未实现范围均保留。2026-10-05 的正式 Account 认证及个人设置验收分别见文末追加记录。

## 工程检查

- TypeScript 严格类型检查通过。
- Prettier 检查通过。
- Vitest / Vue Test Utils 交互测试通过：受控输入与选择、原生 Radio 分组、禁用、按钮可访问名称、树键盘、标签页键盘、纯文本消息、按钮异步成功／错误／计时清理、默认主题、嵌套菜单 Escape、模态焦点和抽屉关闭策略。
- 样式检查对照文档的 30 项浅深色 token，检查 84 个当前使用的正文、状态、控件边界、焦点和身份点对比度组合；通过。
- 公共组件无 Debug 数据或视图导入。
- Vite 生产构建通过；产物不包含 Debug 路由组件或演示数据。

## 浏览器检查

浅色／深色 × 1440 / 1024 / 390px：系统导航保持 46px，内容区独立滚动，无整页横向溢出；人工查看桌面、窄屏、消息和详情截图。

交互检查通过：选择器方向键、Home / End、跳过禁用项、选择后恢复焦点、Tab 继续导航；菜单及模态 Escape、嵌套菜单优先关闭、模态背景 inert 与焦点限制；搜索结果跳转并聚焦分区；消息 HTML 文本不执行；窄屏抽屉不越界；减少动效；手动主题不受系统变化覆盖，跟随系统主题响应变化；刷新 Debug 深链接。

另检查复选框、Radio、开关、校验清除、本地保存、按钮成功反馈宽度保持、折叠连续切换、标签页跳过禁用项、分页、tooltip 边缘避让与 Escape、移动端目录。身份标签至进度分区实测 24px。

## 组件细节复查

本轮按反馈移除目录风格说明。834px 浅深主题下，消息输入没有可见标题、内边框、聚焦轮廓或阴影，发送和错误关联保持正常。执行详情没有外边框，长日志容器实测高度 320px、内容高度 811px，可通过 PageDown 在内部滚动。

Tab 指示线与内容滑动通过浏览器检查；过渡时只有当前面板进入可访问树，快速反向切换不会留下旧面板，减少动效偏好下立即切换。390px 无整页横向溢出。单元测试更新为 16 项，类型、格式和生产构建通过。

## 保留边界

对比度针对当前使用组合；深色 `--control-border` 不用于 `--accent-soft` 底色上的必要控件边界（该组合不足 3:1）。当前选中控件用 `--accent` 边界；未来页面若把输入放在强调底色上，须按实际组合复查。

未接入 Go 服务、认证、业务 API、真实持久化或业务页面。未进行 Firefox、Safari 或真实触屏设备验收。生产根路径只显示骨架与空内容区；生产 `/debug` 显示未找到页面。

## 2026-10-05：正式 Account 认证完整结果

[D26 认证工作项](../work-items/d26-account-authentication.md)的 21 个代码/测试/配置路径已独立验收并获主线程采纳，源码提交并推送为 `9a710f272026b41ef69852bbeb41cb7670b500a8`。固定后端基线为 `457b1979c9d6563740543b2011eedc06cce34c71`，最终作者输入 `fixture-input-05` SHA-256 为 `2e0d63e1fabeaf4ba1ba808f2aa5e4a795b903516f6501b08d25517f16b37966`。逐源指纹、实际命令/环境/退出码、原始日志、失败修复及独立结论见[持久认证验收记录](../agent-team/d26-authentication-verification.md)。

本轮环境为 Node 24.19.0、npm 11.9.0、锁定 Playwright 1.56.1 与已安装 Chromium 151、Go 1.27.1。web 唯一依赖增量为精确 `go-captcha-vue 2.0.7`，其余锁定依赖未升级；缺包按原 integrity 恢复到任务私有缓存。

### 工程及纯交互证据

原完整 `npm run check --prefix web` 实际通过 62 项测试、格式、类型和生产构建；随后 UI04 的全部 22 项受影响测试替代原 17 项覆盖，合计 67 个不同的最终适用测试，采用分版本有效证据，不称为最终一次 67 项全跑。核心 29 项和未变公共组件 16 项复用既有结果；最终局部样式修复后另行通过 `format:check`、`vue-tsc`、正式构建及浏览器 spec TypeScript 检查。`Prettier --write` 的格式化执行与只读格式检查分别记录。

纯测试覆盖六 API 的完整 DTO/Problem、CSRF43 与 pass80、Unknown 同 key/完整输入、Cookie 请求单飞、30s 逻辑等待与实际 transport 尾部、忽略取消的迟到结果、路由恢复和焦点归属。两份 Go fixture 文件以离线 readonly Go 1.27.1 通过 `test -tags=integration -race -run '^$' ./tests/account` 和 `vet -tags=integration ./tests/account`；后续 Go 字节未变，复用该结果。

### 真实浏览器与后态

作者四个 Go 顶层、六个浏览器分支均有实际 verbose PASS，按以下输入复用；原先包含其他失败场景的 driver 仍保留 exit1。

| 顶层/分支 | 有效输入与结果 |
| --- | --- |
| `TestAccountAuthenticationWebSessionLifecycle` | input03 中该顶层通过；真实登录、Session CSRF、刷新恢复、注销、Cookie/保护内容和两条成功 Audit。该 driver 的 desktop 焦点失败另保留 |
| `TestAccountAuthenticationWebRotateChallenge` | input04 的完整 desktop/keyboard 顶层通过；真实错误密码阈值、公共图求解、verify80 字符 pass、同 key 完整输入登录及 DB 一次性消费 |
| `TestAccountAuthenticationWebRevocationAndExpiry` | input04 中两分支通过；正式改密撤销与精确自有 Session 期限后态均经真实 GET/router 隐藏旧内容。该 driver 的布局缩放失败另保留 |
| `TestAccountAuthenticationWebLayoutsAndProduction` | 最终 input05 完整顶层通过；浅深 × 1440/1024/834/390、实际长名、200% 缩放无页面横向溢出、键盘、真实服务端 theme、API/资产不回退及生产 Debug 排除 |

真实链使用完整 `app.Run`、全迁移、Secret 后同一启动预算内的 Model 初始化，以及自有同源正式 dist/反代服务器。rotate 两分支另核 reduced-motion 的实际 computed 样式，保留角度功能。作者查看了最终浅色 1440px、深色 390px 安全截图；截图不含敏感表单。

独立增量 `TestAccountAuthenticationWebIndependent` 为一顶层、一浏览器链，实际通过错误 proof 的 `400/CHALLENGE_INVALID`、禁用失焦及创建按钮交接、换题后成功验证、原 key/完整输入/pass、登录后 Session 一致、匿名 CSRF 不能注销及正式 Session CSRF 注销，并检查 failed/consumed 挑战、Session 撤销及 Audit 后态。成功轮 top 10.99s、driver 36.457s，原 raw SHA-256 为 `b7fa8aba0508e49c1e729890c6246570d7d5c6925defc5feda5279569aa95a6b`。

### 原失败与证据边界

核心 F1–F4、页面材料清理/迟到恢复/焦点问题、导航循环 exit143、纯测试前置错误均保留原日志及修复映射。作者首轮真实失败是必填标签精确定位不匹配，未进入登录；之后分别发现 desktop 验证后的焦点缺口和 200% 顶部布局溢出，修后维持原断言复验通过。未延长预算、删除断言或把原失败 driver 改写为绿色。

独立首轮在已见 HTTP401 后，CDP 读取 body 报 `No data found for resource with given identifier`，原因未知；未取得该 JSON，也未进入后续目标。第二轮仅在私有探针加入按阶段、有界且同 X-Request-ID 绑定的响应 tee 观测，八次原 CDP 读取均成功。tee 会改变流背压、取消传播及微任务时序，因此该结果不称为无干预 transport 尾部证明，也不证明首轮根因已确定或消失；取消责任沿独立审查及纯测试证据判断。两轮原件与观测限制均保留。

作者五轮、独立两轮各自实际捕获四个容器和三个网络的 nonce/label/exact ID，均两次确认 absent，原两个容器/四个网络基线不变。非空浏览器 PID/starttime 及收养后实际 wait 记录、两次进程终局检查、runtime/私有凭据目录清理和固定输入前后指纹见持久记录；不以单个 cleanup 布尔代替证据。

本结果只关闭本卡认证完整结果。Unknown、畸形响应和迟到 transport 的纯模拟不代表真实服务端 COMMIT/网络故障或 Cookie 竞争验收。未单独运行真实 Vite 开发代理浏览器场景；未验 Firefox、Safari 或真实触屏设备。自有测试服务器不是生产 hosting，Central 仍未托管 SPA；完整 D26/D28、Provider 调用及既有 Object/Artifact/Project 未完成边界保持。

## 2026-10-05：本人资料、主题与修改密码完整结果

[个人设置工作项](../work-items/d26-personal-settings.md)的精确 24 个代码/测试路径已独立验收并获主线程采纳，源码提交推送为 `c54f73f3324caa11608d84e5d207141985eb6074`。固定业务基线为已验认证 `9a710f272026b41ef69852bbeb41cb7670b500a8`，最终 `fixture-input-01` SHA-256 为 `20d8fc5e3ec3ad3f5eb477345fd76d002eec057b4a0a2d0fad875e354c1a99fd`。24 源、11 项固定依赖及 9 项正式 dist 产物在各轮前后逐 SHA 相同；未消费活动 Resolver/Artifact，未改 Go 生产、API、迁移或包锁。精确输入、实际 argv/env/exit、原日志及独立报告见[个人设置验收记录](../agent-team/personal-settings-verification.md)。

本轮沿 Node 24.19.0、npm 11.9.0、锁定 Playwright 1.56.1、系统 Chromium 151 和 Go 1.27.1，无新依赖。`npm run check --prefix web` 在私有固定输入实际通过格式、9 文件 110 个纯测试、类型检查和正式构建；新增 Go fixture 的 integration race compile、适用 vet、浏览器 spec TypeScript 和配置语法检查均通过。Unknown、忙碌、迟到请求及实际尾部责任由纯测试覆盖，未注入真实 COMMIT、ROLLBACK 或网络故障。

### 四个新增真实顶层

四个 Go 顶层分别对应一个真实浏览器 case，均通过，使用正式 dist、自有同源服务器和完整真实 Account/PG/MinIO。普通用户通过正式邀请、inspect、兑换建立，没有 SQL 造用户或权限。

| 顶层 | 通过的实际行为与后态 |
| --- | --- |
| `TestAccountPersonalSettingsWebProfileAndAvatar` | 用户名规范及重复拒绝、空显示名、最终输入快照；JPG/PNG/WebP 上传、服务端重编码后的元数据/字节读回、替换与删除；SVG/动画/超限拒绝保留旧头像；精确 8 条成功命令及 8 条 Audit |
| `TestAccountPersonalSettingsWebThemeAndNavigation` | 预览/取消的持久 version、theme、命令和 Audit 不变，3 次明确主题写入；两窗口真实版本冲突保留草稿；三叶子登录返回、dirty 菜单/back/logout、同 Session 重验与 system 变化 |
| `TestAccountPersonalSettingsWebPasswordRotation` | 确认不一致零 POST、当前密码与弱密码反馈；严格 200 后密码清空，新 Session/CSRF 后再写资料；两个旧 Session 因 `password_changed` 撤销，新密码登录、旧密码拒绝及初始建议清除 |
| `TestAccountPersonalSettingsWebAuthorityAndProduction` | 普通用户/管理员各操作本人；body 越权、Origin、CSRF 拒绝；失效隐藏；浅深/system × 1440/1024/834/390、长文本、CSS zoom=2（非浏览器原生缩放）、键盘/焦点/reduced-motion、无溢出及生产 Debug/API/资产边界 |

作者及主线程实际查看了安全的深色窄屏设置截图，不含密码等敏感输入。

### 认证回归与独立增量

旧认证四个顶层名称、六个浏览器 case 采用分项组合覆盖：lifecycle 与 desktop 先通过，同轮 keyboard 在旧 spec 的 `Response.json()` 遇到 CDP `Network.getResponseBody: No data found for resource with given identifier`，原 RotateChallenge 父组及 driver 仍记 FAIL。该原日志没有记录这次响应的 status/body，原因未知，不能补推 401 或 ChallengeRequired。独立限定归因后，主线程仅授权一次原样 `-run '^TestAccountAuthenticationWebRotateChallenge$/^keyboard$'`，实际通过；源码、旧断言、预算和观察方式不变，没有 clone/tee 或 CDP 干预。revocation、expiry、layouts 随后原组通过。不能据此写成原认证父组一次全绿或首红根因已解决。

独立真实增量 `TestIndependentPersonalSettingsLiveOwnerRevocation` 为一顶层、一浏览器 case，通过同 Session `pageshow` 后保留未保存资料和 File/Blob、另一窗口真实改密并以新 CSRF 写资料、原窗口真实 401 后清除旧草稿和候选头像且不被 dirty 确认阻挡、同文档重新登录不恢复旧稿。只读 PG 核对 profile/password 各一条、avatar 零条、Audit 两条及新旧 Session 后态；透明 URL 计数核原 `revokeObjectURL` 实际调用，不外推 JavaScript GC 或 Object guard。通过轮 account 11.591s、driver 0，raw SHA-256 为 `7a90444fc7aa7e8cf745d4b8332a2d2be4fc68a616c083a3ed4e8a37b1102d05`；独立最终报告 SHA-256 为 `ada59561329eb0c5d8b9b28720e6a548601a09b27966e04df4ea40897ae92b9c`。

### 原失败、清理与限制

Core F1/F2 的原红与修复保留：改密 Unknown 的实际尾部结束后仍须先确认 Session 才能注销；后继普通 `not_started` 拒绝不消解首次 Unknown，也不丢原 key/body/File。UI-F1 独立 pure 原红及同 probe 修后通过、UI-F2 后继真实 identity epoch 不得重建旧改密反馈的作者原红/修后检查和独立静审均有版本对应。

原 TypeScript 分支类型、测试选择器/清理时点和 TS 检查命令缺现有类型路径的准备失败另行保留。Core F2 中间源和首 fixture TS 源是事后按原 SHA 精确重建，不能称为同期备份。独立真实首轮在运行目录长度检查失败，虽启动了后端前置，但 Node/browser 未启动，不能计行为验证；实际双清后仅将私有 runtime 改为短路径，同一探针和预算一次通过，未作产品修复。

所有真实执行沿原 race/count1/每包 6m、Go 顶层 2m、Playwright 单例 45s/worker1/retry0。作者五轮、独立两轮均有自有 4 容器/3 网络的 live nonce/label/exact ID 和两次实际 absent，原 2 容器/4 网络不变。首作者组未采到 Node PID/starttime，仅有 Go launcher 实际 `cmd.Wait` 返回 0；后四轮和独立通过轮的非空 PID/starttime、收养后 wait 不能反填首组。每轮进程/runtime、私有 IPC 材料清零及输入末核见持久记录；trace/video/request-body 日志关闭。

本结果关闭本卡本人设置范围，不代表完整 D26/D27。测试自有同源正式 dist 服务不等于 Central 生产 SPA 托管或真实 Vite 开发代理验收；正常头像读写和退出不证明既有 Object Runtime/guard 缺陷已修。Artifact、其它未绑定能力及未验浏览器/设备边界保持。
