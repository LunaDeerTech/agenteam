# Dialog 关闭后的显式页面焦点后备：验收记录

2026-10-06，共享修复 [rev1](../work-items/d27-dialog-fallback-focus.md)已通过作者检查和独立验收，主线程采纳五条交付路径，提交推送 `fd32120eba4c76f67b649248377f4825a78d5d79` 并核远端一致。规格为 `3c79fd4069837c4cee0b1ed37e00480ea8f1909f`，技术 §1–4 SHA `e1e5606cbb493f5079aa51c72eaa46462ae80d6794059ca4db9904903e4cef75` 保持不变。本次接受共享组件能力；账号安全 UI 按 rev2 继续组合验收，尚未接受。

## 1. 固定输入与接受范围

作者 input01 manifest SHA `604a4738761e428b0bff3f8ea64319ae07f6e8e072b97dead925c309c09f6860`，组件基线 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`。固定五源、11 文件 browser 闭包、锁与 127 项隔离 web 指纹；没有消费 Account 活动候选或 dist。[作者报告原件](dialog-fallback-focus-verification-evidence/objects/76b13f73adf3c4c41457a4fb69f0b35ccae17430a809a7890443346f75791e71)及[独立报告原件](dialog-fallback-focus-verification-evidence/objects/53f5b198962a689f94ccf213ec2c6e8aae73768c191ce0f3611ef22d38deac1e)保存各自实际范围。

| 交付路径 | SHA-256 |
| --- | --- |
| `web/src/composables/useLayer.ts` | `aa3e2624666350d289c571c99516776423ef79d58f095308478d2593a5dcd7f3` |
| `web/src/components/ui/UiDialog.vue` | `2009c74dbb2f2cd5ae351b435eb3d801f9dce3ea0a9c91c73a055371fed211b0` |
| `web/src/tests/dialog-outside-focus.spec.ts` | `50e1e4da805ad3229322cc6429fab9fc476859df2f70f51f8b024bba3ff8ec59` |
| `tests/account-captcha-web/e2e/dialog-outside-focus.spec.ts` | `f2db7a6e2f0123f1628a6c7c9a8d32c257373b4867c8710be53a9dd3030e57bc` |
| `docs/development/frontend/components.md` | `0a20dc50008737e56d094e641dc2354816596553785fa4c3bf5de2d3d3784e96` |

UiDialog 的可选 `fallbackFocus` 以响应式 ref 传入 useLayer，关闭时读取最新值。仅正常 `open=false`、原层为顶层、无剩余 modal 且显式目标非空时进入新恢复分支；合法原 trigger 实际聚焦成功优先，否则同步尝试当前页面目标。同 document、非 BODY／HTML、布局、祖先 hidden／inert、disabled 和实际 activeElement 均有门禁。无 prop／null、直接卸载、非顶层及剩余 modal 保持旧规则；没有新增延迟抢焦点、全局目标扫描、CSS 或开层次序变化。UiDrawer／Popover 封装器未新增 typed API。

## 2. 原始失败、复验与复用

以下耗时均为原 driver 的实际终局，不把 discovery／类型准备当浏览器行为通过。

| 原轮 | 实际 exit／耗时 | 结果与解释 |
| --- | --- | --- |
| 作者 old01 | 1 / 2.008s | grep 前置 `^` 不匹配完整标题，No tests found；没有 browser／server。原 clean=false 保留。 |
| 作者 old02 | 1 / 7.608s | 原固定组件真实 RED：重挂后的单层确认正常关闭，overlay／inert／滚动锁已清，当前标题 `toBeFocused` 失败。 |
| 作者 pure01 | 1 / 4.055s | 112/114；两处新增测试重复 spy inherited 布局方法，误使后备目标也无布局。属于测试前提，原 source／raw 保留。 |
| 作者 pure02 | 0 / 4.238s | 仅测试节点自有 getClientRects 装配调整，生产未变；Dialog 102＋旧 components 12，共 114/114。 |
| 作者 new01 | 0 / 123.627s | 原 34＋新增 11，共 45 项真实 Chromium PASS。 |
| 独立 pure01 | 1 / 1.759s | 七项 PASS；一项错将下层首可聚焦元素预期为内容 input，实际为 UiDialog 内建关闭按钮。 |
| 独立 pure02 | 0 / 1.421s | 仅改精确目标断言并复验受影响一项，七项 skipped；背景 fallback 零调用门槛不变，与原七项组成 8 项通过。 |
| 独立 type01 | 2 / 0.791s | 私有配置未显式定位已锁 Node 声明；不是产品错误。旧 tsconfig 和 raw 保留。 |
| 独立 type02 | 0 / 0.918s | 私有 tsconfig 接入已有 web/@types，未改浏览器断言。 |
| 独立 browser01 | 0 / 8.292s | 三项真实 Chromium PASS；没有重复作者完整矩阵。 |

作者 browser type 2.517s、隔离 web build 9.432s、format 5.924s、list01 1.893s、list02 1.897s 均实际 exit0；list01 精确发现旧红案例一项、list02 全 45 项。old02 在“先 list”指令到达前已启动，不追填先发现事实。独立 list01 exit0/1.024s，精确三例。各原 argv、cwd、记录的 env、input、raw、result 均在[证据索引](dialog-fallback-focus-verification-evidence/index.json)，不将选定环境字段扩称为完整环境快照。

old02 原运行 probe SHA `1b3b4e000aee606440e3a18fc8d24f4a7f616434d7e5894b572de096d3e15047` 与最终 `f2db7a6e…30e57bc` 不同。保留两份精确字节及[项目格式规范化等价原记录](dialog-fallback-focus-verification-evidence/objects/3072ee646ef7328902042f47b9dc905b072f4b46755440cfb24b2592f020d548)，不称为 byte-identical；本次归档没有重新运行 formatter。作者 pure01→02 逐输入指纹仅 pure test 改变，独立 pure01→02 则保留原 probe 和单一断言差量。

## 3. 真实覆盖与资源终局

作者 45 项涵盖旧红原断言、Escape／action／真实遮罩、390／1440 与正常／reduced motion、最新目标更换、合法 trigger 优先、无效后备、直接卸载、剩余 modal／非顶层及后续用户焦点；预算保持 45 秒/test、expect 3 秒、worker1／retry0。

独立三例使用绝对锁定 Playwright CLI、45 秒/test、worker1／retry0：整页面与单层 Dialog 共同卸载后 initial-null 重挂，关闭精确落到后来替换的新标题再原生 Tab；真实 inherited contenteditable 后代 focus no-op 的前提成立时转到标题，同时核合法 trigger 优先、直接卸载无新 fallback 与用户后续焦点；剩余 modal 首控件优先、Tab 不逃逸，null 目标保持原 trigger。原生 no-op 的安全附件保存在独立 results.json，不把 jsdom 布局桩当真实几何。

| 真实轮 | 所属 PID/starttime | adopted 实际 wait | 终局 |
| --- | --- | --- | --- |
| 作者 old02 | 13 | 5 | Node 已 wait、server.closed=true、所属两扫空、端口基线不变、短 TMP 删除、输入不变。 |
| 作者 new01 | 145 | 4 | 同上；45 项通过后窗口交还。 |
| 独立 browser01 | 19 | 4 | 同上；无清理信号，实际 exit0 后窗口释放。 |

old01 的唯一所属进程两扫空、端口不变、TMP 已删，但因未进入 server lifecycle 而保留原 clean=false／server_all_closed=false，不能改称该原轮 all-clean。其余准备／纯测轮的实际 wait 与所属终局分别保留。历史 PPID1 Z 未触碰，不纳入这些清理或 wait 声明。输入前后核对包含五候选、组件闭包、原运行工具与私有 probe／config／runner；原报告记录 runtime 7094 文件／28 symlink 一致，安装依赖实体未归档。

## 4. 归档、离线检查与未完成边界

[最小证据入口](dialog-fallback-focus-verification-evidence/README.md)保留 219 个逻辑原件、139 个 SHA 对象，共 1,660,403 字节；固定 Git 140 个引用替代完整树副本。全部失败候选版本、原始命令／日志／退出／清理、旧红 screenshot／trace 和独立安全附件保持原字节。离线脚本只核对象、固定 Git 的五交付路径、127 项隔离 web 指纹及 16 轮原记录，不执行产品，也不读取 Account 活动源。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 \
  python3 docs/development/agent-team/dialog-fallback-focus-verification-evidence/verify_archive.py
```

Account new01 原组合焦点断言 false、new02 诊断 BODY 与未直接采样 captured trigger 的区别保留在[工作卡 §1](../work-items/d27-dialog-fallback-focus.md#1-责任边界与固定证据)及原固定交叉引用；本包不重复复制该业务证据，也不把受控组件重挂等同于真实 Session／Navigation 通过。账号安全按 rev2 继续，真实 BFCache、其它浏览器及所有调用方业务组合不在本结果内。完整 D26/D27、D08–D28/E01 未完成，E01 未开始；Summary 待决、Object/tools 原停止、Artifact/Project 与生产未绑定、ready503、Runtime／生产 SPA／Vite 等限制保持。
