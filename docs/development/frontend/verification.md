# 前端基础验证记录

日期：2026-10-01。环境：Node 24.20.0、npm 11.19.0、Chromium / Playwright。此记录针对公共组件和 Debug，不代替业务页面验收。

以下至“保留边界”为该日期的历史记录，原结果与当时未实现范围均保留。2026-10-05 的正式 Account 认证验收见文末追加记录。

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
