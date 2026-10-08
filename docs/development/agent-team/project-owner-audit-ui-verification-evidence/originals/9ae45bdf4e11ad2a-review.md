# Project Owner Audit UI state-v1 独立验收

结论：**状态层受控 20 项组合 PASS；六源整体接受因 App 集成必修暂停，STOP**。对应卡 #4/#5/#6/#7/#13/#15，freeze SHA `c730a2e83a59f35971d4bfd81d74fa4c6cfcd5f3132d6c7aa510cfe7c9039f0c`。本轮运行／输入核验时六个安装源与冻结副本核同，四个既有文件的差量对原接受副本逐字节生成；最小运行闭包 24 模块／445116 B 与作者固定输入一致。没有读取活动 Views/router-index/ProjectNav/browserJS。

后续集成发现：root 在本报告封存期间转交作者真实 App/component 首轮 43/46 的失败并核对源链：非法 `/owner/demo/settings/audit?cursor=x` 经冻结 auth.ts 的 named not-found redirect 丢失 query，App.onMounted 再用变为合法的 fullPath 调用 projects.afterNavigation，实际发出 Resolve/Get（未发 Audit）。此为 auth 与 App 的集成必修，现六源不可整体接受，也不能据本轮纯状态 PASS 宣称其无缺陷。原 state-v1 冻结字节／20 项结果保留；root 已授权作者仅 auth.ts 的 to.fullPath 窄修，五个其余 state 源仍冻结。下一步仅审 state-v2 差量与固定真实 App 拒绝路由回归；不重跑这 20 项或读取活动页面。作者该失败原件待正式移交独核，不混成本轮本人实跑结果。

[最终输入核验](final-input-check.json) 与 [结构化结果](result.json) 已冻结。

独立接受 20 个有界用例，按 **run01 未变 18 PASS＋run02 两项定点 PASS** 组合；没有声称一次全 20 通过。首轮 exit 1、18 PASS／2 FAIL，命令 1.326 秒；次轮 exit 0、2 PASS／18 skipped，1.479 秒。两轮均有实际 direct wait，无 adopted wait 或强制清理，观察到的 owned 两次扫描为空，各 69 输入前后同；实际 Session owners 在每例结束也已释放。原 [run01 raw](run01/raw.log)、[原探针](run01/independent.spec.ts)、[run02 raw](run02/raw.log)、[最终探针](run02/independent.spec.ts) 保留。

两个独立首轮失败是探针前提问题：既有 run 在 microtask 中开始 restore，调用返回处尚未进入 checking；CSRF 换代后 Workspace 已自动完成新 Resolve/Get，不能再期待空 context。仅修改 scratch：第一项扣留实际 Session fetch 并在 checking 入口核同步清理，第二项扣留新 Get，验证完成前不授权及完成后一次初读。原失败未覆盖，[差量](probe-correction.patch) 与 [归因](probe-failures.json) 保留；六份产品源码无修改。先前一个 Python 闭包准备 helper 的正则字符串 SyntaxError 发生于执行前，修正引号后才建闭包，没有因此启动 Node 或改产品。

独立用例经过正式 Session、Workspace、Audit controller 和公开 API/transport。普通 Human 的局部 403/404/unknown 保留原 Owner Update body/key，Audit abandon 不丢意图且不自动 lookup；当前合法 401 才清身份，迟到 success/401 不发布。现有 System denied 不阻止当前 Project 读取。list/detail 原生 cancel Promise 拒绝前仍占 Cookie owner，旧域请求为 busy；尾部结束只解锁，同页失败需显式 retry，新页等待旧尾部后仅一次默认初读。完整 Get 前、失败 Get、deleting／异主 Project 不给上下文；同身份 profile 更新不重载 Audit，checking 同步清列表/详情/筛选且保 General 草稿，CSRF 换代需要新 Get；参数复用清旧作用域，cursor 失败仅显式首页恢复。直接调用 route helper 的 audit 后缀 raw 拒绝用例通过；这不证明 Router 重定向及 App 挂载后的实际调用链。

静审确认第 14 构造依赖追加而未改变原 13 个位置；新域使用独立 revision 和既有唯一 owner/actual finally，普通 Human 分支不借 System admin 门禁。currentReadContext 仅完整当前 Get、稳定 ID、完整身份及 generation/readGeneration 可用，Owner 既有写状态机无语义差量。页面 controller 独立清私密页态，旧 catch/finally 不发布新代次，返回列表不自动补读，未知读取不进入写恢复。尚未把这些状态层事实扩成 App、RouterView 或原生浏览器接入结论。

复用作者固定结果：state-unit03 五文件 285 PASS（新 52＋旧 233，5.678 秒），state-types03 原 187 文件时点 PASS（11.514 秒），最终六源 format-check02 PASS（1.232 秒）。unit/type 后仅一个测试链排版差量，已与作者原 patch 精确比对，四个生产源同；没有重跑活动全图。state-atomic-regression01 原 1 PASS／50 skipped 表明初读 Get 的 General 草稿发布疑虑在修产品前已通过，不称修复过产品缺陷。[76 件作者证据指纹](input-binding.json) 绑定原 state-unit01 audit 后缀未接入时的 50 FAIL＋1 cleanup rejection、state-unit02 的 49 PASS／1 FORBIDDEN 测试前提失败，以及格式检查失败／纯格式修正，原字节均保持。

API 阶段原独审 71 与作者 API86/旧102 仅复用，未重跑。此 PASS 不含 Views/ProjectNav/router-index/App pageshow、真实 focus/guard、browser Cookie/CSS、Go/数据库、真实 HTTP/schema-client、当前全 app type/unit/build、完整 Audit UI22 或 D27/生产。后继 UI 集成必须证明实际 checking 卸载、新页建立、导航守卫与参数复用接入；本轮没有资源、网络、安装、Git 或仓库写入。
