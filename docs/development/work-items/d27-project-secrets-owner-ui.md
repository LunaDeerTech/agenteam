# D27 Project Secret Owner UI

状态：实施中，首真实链整体 FAIL，定向测试方法修复后待第二次；尚未验收。仅消费 main 已交付的 Secret Owner HTTP；未修改后端、公共 OpenAPI、依赖或迁移。

## 范围与固定合同

依据 [Secret HTTP](d10-secret-variables-owner-http.md)、[Secret Owner](d10-secret-variables-owner.md)、[项目设置布局](../../frontend-design/layouts/project-settings.md)、[设置壳](../../frontend-design/layouts/settings-shell.md)。普通 Human Owner 进入 `/:username/:project_name/settings/secrets`，查看安全列表与详情，创建、修改名称/描述、显式替换值及删除。默认 Project initializer 的既有未绑定边界保持；不创建项目、配置 Agent 授权或实现材料消费。

现有 HTTP 仅三个路径模板：collection GET/POST、item GET/PATCH/DELETE、commands/lookup POST；所有成功均 200。列表 5 MiB，其余输入与成功输出 1 MiB。安全 metadata 恰八字段，值 1–65536 UTF-8 字节、不修补 Unicode/不 trim/不允许 NUL；名称与描述按原 Schema 校验。未知、重复字段和返回的材料字段拒绝。归档项目可读，新增写仅当前 active Owner。

Lookup 仅原 command、target_id、update/delete 原 expected_version；沿原 key/CSRF，不含 value/request/semantic_digest。只 committed/not_observed，后者不证明回滚。无自动重试、原材料重放、轮询或换 key。历史回执不等于当前 metadata，确认后失效旧列表/详情并重读；读取失败不推翻已确认提交。版本冲突保留安全草稿，须完整新 GET 后明确采用版本，重新输入需要替换的值。

## 输入、身份与退出

值只在编辑器本地和一次执行闭包中存活；点击提交即清 DOM/响应式输入，离页、身份/项目变化或销毁也清除。不进入 Session 公开状态、长期意图、日志、storage、URL、摘要或返回展示。JS 字符串及浏览器/GC 副本不可承诺物理擦除。Session 恢复仅保存身份、目标、原版本、key/CSRF 和安全进度，不保存原材料请求。

本域 read/write/lookup 全部经过原 Session 唯一 Human Cookie owner。当前 401/CSRF 失效清身份；局部 403/404 清受保护展示，不设置 System 拒绝。真实身份变化丢弃旧意图；同 Session 临时 checking 保留不确定命令身份并禁止发布。原请求 body/cancel/release 实际 finally 前不释放 busy。取消/传输/解析或取消失败按不确定处理，不视为回滚。

页面发布绑定当前 Session 身份、Project generation/readGeneration 与当前 route。导航确认在原全局 Session restore 前，仅已注册本页才进入新 await；明确放弃不宣称服务端取消。共享 UI 组件接口、原路由拒绝及其他域逻辑保持。

## 文件与方法

新 API `project-secrets.ts`、controller `useProjectSecrets.ts`、页面与 SecretEditor；共享 client/Session/router/Settings/ProjectNav 仅本域闭合接线。普通 Variables donor `60dbcee7` 只参考严格校验与列表结构，未覆盖旧 snapshot 的 shared 文件，也不复用普通值长期缓存或其原失败验收。

定向三 spec 使用真实 API/Session/Workspace 与 Vue Router/页面，只有 Fetch/环境受控：校验合同、输入清除、Unknown identity-only 查证、same-session/真正换身份、当前拒权、原取消尾、归档只读、迟到读抑制、显式冲突采用与导航确认。它们不是实际浏览器或真实 Cookie/PG 证明。后继实际窗口由 root 分配，SPA concurrent-publication STOP 不变。

## 首轮基础结果（有限）

- 首版 source `7df16529`。pure01 原 Wait1：26 项实际执行中 15 通过/11 失败，另页面套件未加载；确定 client 新 Secret 分支后继续进入旧分派，带 target 的合法请求被旧 fallback 拒绝。仅接为同一 else-if 链，旧域规则不改。页面套件错误来自私有 configLoader runner 的已关闭 Vue module runner，改原 `.mjs` native loader。
- type01 原 Wait2：本域 delete 联合类型未收窄，另主树已锁 node_modules 缺 go-captcha-vue。前者返回固定 delete literal；后者比对旧 Skills 树与本树 package-lock 完全一致后，只读借其原 2.0.7 包，在本树私有 node_modules 链接集合补齐。未 npm install、未修改锁或共享依赖。
- 修后 pure02 三个新 spec 共 30 项全部通过，原 session24216/chunkc7943a exit0，13.82s；完整 vue-tsc type02 原 session91351/chunk78def9 exit0。运行命令及日志在本树 `output/ai/secret-owner-ui/`，源随本卡可恢复；不将可重建 ignored 输出称 main 固定材料。
- 原 client/Session 受影响边界仅选择六项：Knowledge 四 GET、Project 五请求、Credential 明确五操作，以及普通 Owner active/archived 与当前401；compat01 四文件选中6项通过/165未选，session50229/chunk391505 exit0。没有重跑旧全 UI 矩阵。
- work_ui 非作者实际审查 14 web 源有限接受，无确认 must-fix；引用作者纯控/type 结果，没有重复运行，也不将它们升级为真实浏览器或 PG 证据。
- 当前没有实际浏览器、真实 Cookie/PG 或视觉验收。首次正常 CRUD 与一次真实提交后受控响应丢失查证方案待 root 分配 fixture/shared writer；生产 SPA publication STOP 继续保留。

## 首次真实链源码准备（未运行）

首四个测试入口已保存 `3e406c0e`，方法见 [本域 recipe](../../../.agent-state/secret-owner-ui/README.md)。同一真实 Owner 的 create→list/detail→已提交 PATCH 原回执完整读取并关闭后受控502→identity-only原key Lookup→current GET v2→delete；只有测试代理丢响应，不模拟回滚或补第二次 PATCH。默认根仍未绑定 Project initializer，已有数据由显式真实 ports fixture 实际 Drain 后提供。新 normal-only Secret observer 不借 Knowledge requestfailed 例外；提交值清空、材料不回显、same Request/XID/reader/Session Promise/DOM 和完整资源尾均为必要条件。

离线准备：strictTS01 原推断错误 exit2，单类型注解后 type02 exit0；PW list01 exit0 恰1case。native控01 因误写 Session readonly identity exit1，改用正式 leave；修后6模式/30检查 exit0、0 unhandled。所有反控明确使用受控 Fetch/PW，无 Go 编译或实际浏览器/PG；它们不改变上文未验收边界。源码将由未参与实现者进行实际方法审，真实窗口由root另授。


私有正式前端 build02 已 exit0（全type+Vite），69文件/981952B；首次 build01 因私有配置相对路径错误退出，保原日志。共享入口基于 root 转入19a donor，原 Knowledge 默认与新增 Secret 两个固定profile复用原 root 监督尾；metadata/Skill数据组保持。新入口5方法按原失败修后分项均通过，受影响旧2source控制通过；这些都是无OS资源的离线方法证明。尚未Go候选/真实窗口，当前不能宣称前后端联调或全资源链成功。

## 首次真实结果与定向修复

source56904deb 上 candidate01 原19161→9da97c Wait0（40.946s），exact list原Wait0恰1top；binary61,465,686B，SHA835f2f40adbc79c667eeb68655316917c3f1bd82c7d7439f02a4bdb6218bb56d。Go临时目录双空，固定MinIO普通私有副本已核。生产/dist/Go源码随后没有变化。

native01 原1840→d44246整体FAIL。outer788736/sup788795，启动fresh5,619,044,352B；1491输入hash1a65dba7b3f873cd515790703153f3d3ffb64a4199ff7d9593626416475a14ff。Go790967原Wait1/top55.50s、Node791128原Waitfalse、driver788797原Wait1。firstfailure只有create/page_closed=true，代理仅首个空list200，无POST；不得把后继诊断回填为原首次失败的具体await。原sup148.164s terminal1，7资源14absent/private/runtime/desc/TCP各双尾齐、inputs unchanged=True。外层在原sup Wait和退役以后，将set写JSON触发TypeError；外层原Wait1、缺result保持，不补造原外层观测。原log在/tmp/psu01/ui-6c2e643f587644ee.log，安全首阶段和tail在本域evidence-owner-01，supervisor原摘要在native-01-control。

后继无浏览器诊断确认一项定位器缺陷：锁定PW1.56.1实际injected selector在受控JSDOM、原UiField必填标记结构上，exact label“名称”/“新的 Secret 值”均0匹配（其elementText包含aria-hidden星号）；相同accessible textbox role各1匹配，1a445c实际0。首诊断误用生成exports构造方式失败，改用原工厂后得到上述结果。spec只把这些必填控件改为exact textbox角色，并细分create安全stage，原断言/45s/expect5s不变；产品、前端dist、Go候选均复用。

新增唯一可恢复外层入口 `python3 .agent-state/secret-owner-ui/run.py --attempt 02`，固定原Secret selector/candidate01/dist01，fresh /tmp/psu02、evidence-owner-02、native-02-control；只调用原sup/原helper，结果在原采样时sorted，不后验补采。新run自身追加原Secret collector必需输入，known逆变对应更新；metadata/Installer条目、sup预算/资源门没有变化。离线actual main的OS边明确doubles，空set经原Wait后真实写JSON，低容量零spawn；与受影响inverse/collector共3方法cc6293实际0。strictTS03原13625→c8a72b0、PWlist02原13246→b073c9恰1/0，未重复旧30组件或6native模式。第二实际尚未运行，不追认首FAIL。
