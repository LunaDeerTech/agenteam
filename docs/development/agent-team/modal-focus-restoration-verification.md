# 共享模态关闭焦点恢复验收

2026-10-06，[共享修复 rev2](../work-items/d27-modal-focus-restoration.md)已由 `directory_backend` 实现、`recovery_verification` 独立最终 PASS，主线程采纳并提交推送 `79f922ec259d2838052a903612e2a27005618c11`，主线程已核远端一致。接受输入为作者 input02 SHA-256 `584a9de02239ca17c6bea8834c2cf262aec255cff690780bd0a9c97ffe78c4a6`；本归档逐字节核对四源与该固定 Git。邀请 UI 已恢复 rev4 实施与独立业务窗口，仍未接受；本结果只接受共享组件能力。

## 1. 输入与接受范围

基线为 `6be5321c51f24108d886a139c7a31bb0aab92db3`，范围修订为 `5266a49ed5eeaeea3ad46a6ea088cc3bf547e4e9`。纯测/类型/构建使用固定已提交 web 加授权四源，浏览器只用11项组件闭包、固定配置与两npm锁；未使用活动邀请源码或其dist。四路径分别为 `web/src/composables/useLayer.ts`、`web/src/tests/dialog-outside-focus.spec.ts`、`tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts`、`web/src/tests/components.spec.ts`。

| 输入 | 冻结事实 |
| --- | --- |
| input01，`7bb8455b359d8ee4d4d67a4df29075b43173994be49a0b49b2fe7f5c78310610` | 首版同步模态内恢复；既有组件测试仅把getClientRects spy从beforeAll移到beforeEach，适配restoreMocks:true。作者通过后仍被独立真实原生场景阻断，原件保留。 |
| input02，`584a9de02239ca17c6bea8834c2cf262aec255cff690780bd0a9c97ffe78c4a6` | 只返修useLayer及其两份测试，components.spec.ts不再变化。focus后只有ownerDocument.activeElement确为目标才结束；失败继续后续合法控件、panel或允许的下一层，不重复尝试原失败trigger。 |

恢复仍同步限定于 `restore && wasTop`，优先合法原trigger，其次最高剩余modal及其上层的合法目标。连接性、原生disabled、祖先hidden/inert/aria-hidden、样式及布局门禁保持；无modal原分支、注册/键盘/pointer、全局focusable/Tab、组件props、Teleport/Transition和CSS不变。真正编辑宿主保留精确恢复；继承isContentEditable的普通后代不被当作focus已经成功。

原件入口：[作者首版报告](modal-focus-restoration-verification-evidence/objects/e979649325759baa60133db8f946aa8a0beb96adc12d1d96b371edbcb8077e68.txt)；[作者返修报告](modal-focus-restoration-verification-evidence/objects/645ebe647572195f1397fb1d7f181dcd28de1bbbc7402a0d85c24b2149a93481.txt)；[独立中间阻断报告](modal-focus-restoration-verification-evidence/objects/87bd2d3021d03f38d6b3aa5f1970808f8339684a408880b7a1a6bc8b5831e577.txt)；[独立最终报告](modal-focus-restoration-verification-evidence/objects/f5a9d46f5d0674730aa1508f85ef5aa4d127848a160bce0082b1abb406f75d0c.txt)。所有逻辑路径与精确SHA见[归档索引](modal-focus-restoration-verification-evidence/archive-index.json)，同字节原件只存一份。

## 2. 作者实际检查与原失败

| 轮次 | 实际结果与含义 |
| --- | --- |
| old01 | exit1/1.278s，锚定选择器没有匹配测试；是runner选择错误，不是产品红。 |
| old02 → new01 | 固定旧useLayer真实焦点红：exit1/6.281s；候选同名用例exit0/5.808s。关闭顶层后仍有下层modal，旧实现未把焦点留在其中。更早new01的运行输入绑定限制见§4。 |
| unit01 | exit1/2.385s，新夹具仅await nextTick，未等既有组件聚焦续段；改为flushPromises。 |
| unit02 / unit03 | 分别exit1/2.807s、exit1/2.688s；保留六例inert属性/重复spy布局装配红及两例inert=false属性反射红。修正测试前提，不放宽生产门禁。 |
| pure01 | exit1/9.321s，保留当时六例新夹具失败、旧beforeAll布局spy被restoreMocks清除、隔离树缺已提交颜色文档。 |
| unit04 / build01 | 分别exit0/2.652s（53项）、exit0/6.235s（类型与构建）；完整最终首版检查由下行覆盖。 |
| browser-types01 → browser-types02 | 独立tsc命令漏DOM.Iterable，exit2/1.159s；只纠正命令后exit0/1.033s。 |
| pure02，input01 | 完整npm check实际exit0/13.537s：16文件、287项、格式、vue-tsc与Vite构建通过。 |
| new02，input01 | 实际exit0/93.376s，30项Chromium通过：原11项、16项初始/共同重挂矩阵、3项Popover。 |
| repair-old01，input01产品 | 新的继承editable原生断言真红，exit1/7.096s；原pointer捕获成功，顶层打开后撤tabindex并使祖先editable，Escape关闭后focus失败。 |
| repair-new01，input02 | 实际exit0/30.344s，10项通过；Dialog/Drawer继承editable回退与真正编辑宿主精确恢复、选定初始/重挂尺寸动效、Popover合法/禁用anchor。 |
| repair-unit01 / repair-components01 | 分别exit0/2.692s（52项恢复测试）、exit0/2.291s（原nested-menu精确焦点1项）。其余10项outside及11项旧组件测试未在这次筛选重跑。 |
| repair-format01 / repair-build01 / repair-browser-types01 | 分别exit0/0.510s、5.779s、1.176s；格式、vue-tsc/构建、浏览器spec严格类型检查通过。 |

input02接受采用受影响复验及未变部分的显式复用，不能称重新执行了完整287项或整个新增浏览器矩阵。原11项浏览器和原10项outside单测断言保留，components.spec.ts原断言、CSS/matchMedia setup及清理不变。首次Floating UI依赖记录误以为vue-demi在顶层node_modules，定位实际嵌套依赖后修正；该准备异常没有独立raw，只保留作者报告陈述，不补造。

## 3. 独立产品红与最终复验

| 独立轮次 | 原输入、实际结果与边界 |
| --- | --- |
| pure01 | input01，exit0/2.025s，3项通过：fieldset禁用/首legend例外、无可用panel不尝试背景焦点。 |
| browser01 | input01，exit1/14.516s；Dialog1440 normal与Drawer390 reduced两完整真实组已PASS。继承editable用例先把祖先可编辑，pointer聚焦宿主导致capture=false，是私有壳前提装配红。 |
| browser02 | 只修壳时序，input01真实产品红，exit1/10.241s。正常tabindex触发并确认capture=true后才撤tabindex、开启祖先editable；目标连接/有布局、isContentEditable=true，但focus无效，activeElement=BODY、lowerContains=false。原raw SHA `08d0d655a17ace483fa5265c2009426167d6847df10742d654cf6ec6ca9ef00e`。首错停止，后续Popover组未跑，不能计PASS。 |
| pure02 | input02，exit0/2.002s，3项通过；输入变化后的原纯门禁复验。 |
| browser03 | input02，exit0/13.750s，3项通过；完整probe与browser02逐字节相同，仅替换消费的useLayer。覆盖共同重挂、真实Escape/action/可命中遮罩、双向Tab/无延迟抢焦点、原生合法性，以及Popover、最高允许层、非顶层移除和无modal兼容。继承editable原反例现落到合法BUTTON且lowerContains=true。 |
| browser04 | input02，exit0/5.019s，1项通过；真正编辑宿主无tabindex、tabIndex=-1仍可原生精确focus，Dialog1440/Drawer390后续Tab双向留在模态。 |

首次独立tsc的typeRoots指向缺@types/node的harness依赖，exit2；改为固定web依赖后exit0/1.019s，产品未改。只读查旧证据曾误找old02/input-before.json，实际文件是input.json；这是定位错误。两次浏览器原红的截图、错误上下文、trace和精确probe均保留。截图/trace是原始失败记录，不据本次归档声称完成额外视觉验收。

## 4. 输入绑定、清理与限制

旧 `runs/old02/input/` 与 `runs/new01/input/` 下保存的browser spec均为21,619字节、SHA `a2b951e928d83f31871b1f3ab8ebd493a2a3f32570a037b0a5bf95d9e3475631`；old02/input.json另有该指纹。new01没有运行前后spec哈希，原runner未保存这种绑定，command/terminal/server/results也不提供它。可证明保存副本相同、命令选择器相同、server组件闭包仅useLayer不同；不能提升为独立证明当轮运行字节完全一致。原报告“同字节”措辞以此限制及作者第二报告澄清为准，不倒填历史清单。

repair-old01/repair-new01的原runner在Popen前复制并哈希实际spec/config/四源/11组件/锁，结束再次核对。两轮spec均为 `ab6f5b4e33d8d554a42ed64a9b874accc9c94bc67f2dc63626db94e96b089d1a`；各自前后不变，浏览器组件闭包只改useLayer。纯测试源另有对应版本，不把所有输入都称单文件差量。

作者六轮浏览器均实际wait、server.close、subreaper waitpid；终局170个所属PID/starttime两扫空、私有Chromium TMP消失、监听基线不变。独立六轮动态命令实际wait，68个所属PID/starttime两扫空，18次adopted wait实际完成，运行壳/TMP消失、监听基线不变，窗口交回。历史PPID1 Z Chromium `180182/180185`及独立记录中的既有crashpad不属于本次资源，未触碰或声称回收；后轮清理不覆盖这一历史限制。

共同卸载/重挂是受控组件场景，不是邀请真实App/Session/pageshow。业务确认宿主与App期Promise的修复仍由[邀请UI rev4](../work-items/d27-system-invitation-ui.md)实施并独立验收；共享组件PASS不证明该组合通过。jsdom布局测量不冒充原生focus，非顶层合成事件只证明门禁；restore=false仅静态保留。环境未提供Playwright技能，沿主线程授权使用锁定仓库harness。没有后端、PG/MinIO/SMTP/Docker或生产托管验证，也不覆盖其他浏览器。Summary待决、Object/tools原停止、Artifact/Project阻塞、ready503、完整D08–D28/E01未完成与E01未开始均保持。

## 5. 持久证据与文档交付

[最小证据包](modal-focus-restoration-verification-evidence/README.md)保存374项逻辑映射、204份去重原件，共4,698,647字节；另61项固定Git来源映射不重复复制。原作者20轮命令、独立六轮动态/两份编译记录、两版源、失败截图/trace和终局均保留；两份独立失败trace占主要体积，不复制其展开缓存、完整web、node_modules、dist或可执行文件。

离线核验仅读取原字节和固定本地Git，核四交付源、103项隔离输入、两版组件闭包、运行指纹/原结果与清理记录，不启动产品、浏览器或归档driver：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/modal-focus-restoration-verification-evidence/verify_archive.py
```

本次授权范围只有本报告/证据包、[台账](tasks.md)页首、[恢复接续§11](recovery-2026-10-06-continuation.md#11-共享模态关闭焦点恢复接受)及共享卡接受页首。旧文档原时态保留，共享卡技术§1–4 SHA `e998a47db068618edf67cb8cc624ac8c8f16e09a5765a2b9ece0ef437585716a`保持；不修改邀请卡、AGENTS、指南或产品。文档检查与原件核验不等于重跑产品。
