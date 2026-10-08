# Project Owner UI 客户端与安全返回路径验收

2026-10-08，固定 api-v1 五路径完成限定独立 PASS，root 已采纳；产品 `7dbd42a3fb70f9cea94c21a5c3f77197d65b04b2` 已提交推送，root 已核远端一致。接受范围只有封闭客户端与纯安全返回路径，不是完整 Owner 工作区 UI、Session、真实路由页面或 D27 模块接受。[规格 rev2.1](../work-items/d27-project-owner-workspace-ui.md)其余门槛继续适用。

基线是 `ff396a4efa1c44d54078e9f5dcaf9812f514d665`。五源在独审后与作者 [api-v1 冻结 SHA](project-owner-ui-client-verification-evidence/originals/author/api-v1/freeze.json)逐项相同，并再次与产品提交核同；后继导航集成可以修改工作树，不能回填或替换这组输入。[source-map](project-owner-ui-client-verification-evidence/source-map.json)提供 Git blob、固定依赖及原件 SHA，源码直接复用产品提交，不另复制产品树。

| 交付路径 | 本次边界 |
| --- | --- |
| `web/src/api/client.ts` | 五个封闭 Project 端点、请求及成功响应 cap；旧端点与 Problem cap 保持 |
| `web/src/api/project-owner.ts` | Get/Resolve/List 三投影、当前 target/Owner、Update/lookup 历史回执与捕获输入校验 |
| `web/src/router/auth.ts` | 保留静态返回目标，增加 `/projects` 与严格动态 Project 路径解析 |
| `web/src/tests/project-owner-client.spec.ts` | 客户端契约和流尾部测试 |
| `web/src/tests/authentication.spec.ts` | 返回路径测试及旧 `/projects/123` 期待的规格兼容修正 |

独立静审覆盖完整五源差量。Get 不接受 deleting，Resolve 可以接受合法 deleting 投影，列表保留自己的精确行结构；更新回执只比较原提交字段、同 target/Owner 与合法原版本或加一版本，MaxInt64 no-op 保持合法。读取 cap 按实际字节计数，取消承诺实际完成后才结束请求。返回路径不做 URL 折叠或 decode，保留应用命名空间和公开单段入口的既定边界；本次未执行实际 Vue Router 页面安装。

## 实际验证与输入

[独立最终结论](project-owner-ui-client-verification-evidence/originals/independent/final/result.json)及 [原报告](project-owner-ui-client-verification-evidence/originals/independent/final/review.md)保存命令与限制。实际命令为 `python3 /workspace/scratch/owner-ui-verification/api-v1/run_checks.py run02`；原 argv/cwd、45s 上限、日志、输入前后 SHA、实际 wait 和进程后态均在 [run02 原结果](project-owner-ui-client-verification-evidence/originals/independent/run02/result.json)及 [Vitest 原报告](project-owner-ui-client-verification-evidence/originals/independent/run02/vitest.json)。

Node 24.19.0、Vitest 4.1.11、单 worker 的独立最终轮实际 1.725s，258/258 PASS：

| 来源 | 数量 | 实际覆盖 |
| --- | --- | --- |
| 独立客户端补集 | 58 | 三投影、Owner/ID、精确键、Unicode/字节、版本/日历、原回执、请求捕获、旧 cap 与实际取消尾部 |
| 独立返回路径补集 | 105 | 原静态目标、合法现存用户名、动态闭集及编码/命名空间/后缀反例 |
| 独立末尾字符补集 | 20 | name/version/raw return/key/address 各自的 LF、CR、U+2028、U+2029 反例 |
| 作者冻结客户端测试独立重跑 | 60 | 原 api-v1 客户端测试 |
| 既有 Account 客户端回归 | 15 | 固定 ff396a4e 的原测试，消费本次客户端 |

总计独立新增183、作者客户端60、既有Account15。早一轮 [run01](project-owner-ui-client-verification-evidence/originals/independent/run01/result.json)实际通过238；后加20个定点反例是核实静态 `$` 锚点疑虑，全部在相同产品字节上通过。该疑虑没有形成产品测试红，也没有 api-v2 返修，不能把推测记成缺陷。两轮 direct actual wait 均 exit0、adopted wait 数为0；已观察 owned PID 两次扫描均空，输入前后相同，不声称全机进程清零。

同一个 [合成页结果](project-owner-ui-client-verification-evidence/originals/independent/final/large-page-result.json)绑定 4,934,510 bytes、100 个不同项目、每行8192-byte描述的 Go JSON 等价逃逸及8192-byte cursor。该原始合成 body 先经固定正式 ProjectPage schema，再由公开客户端读取，超过旧600000-byte cap且低于5MiB；缺失/谎报长度、cap+1和实际 cancel 尾部分别受控验证。这不是来自真实后端的响应。归档保留 [精确生成器](project-owner-ui-client-verification-evidence/probes/prepare_large_page.py)、SHA与结果，不重复保存4.9MiB body。

独立测试只消费冻结候选、固定 `account.ts`/`system-account.ts` 及必要工具。`auth.ts` 的 Session 导入在测试内替换为一旦被调用即抛错的 [隔离依赖](project-owner-ui-client-verification-evidence/probes/session-test-replacement.ts)，因此只证明纯返回函数，不证明 Session 行为。包锁、工具入口和元数据有指纹，没有新建完整 node_modules 字节清单；[工具记录](project-owner-ui-client-verification-evidence/originals/independent/final/tool-inventory.json)明确此界限。

## 作者原失败与结果复用

作者 api-unit03 的112测试和 format-check02均通过；[版本对应](project-owner-ui-client-verification-evidence/originals/independent/final/author-version-binding.json)逐项核同最终五SHA。types01通过保留为当时作者全类型图结果，不称本次独立最终全图重跑。

| 原轮 | 原结果与后继 |
| --- | --- |
| [api-format01](project-owner-ui-client-verification-evidence/originals/author/api-format01/raw.log) | formatter exit2，Project客户端原稿缺闭括号；原日志/命令/输入指纹保留，后继完成源码并格式化 |
| [api-unit01](project-owner-ui-client-verification-evidence/originals/author/api-unit01/raw.log) | exit1，111通过/1失败；原测试把合法 `/projects/123` 当作无效返回，修正期待后 unit02/03各112通过 |
| [api-format-check01](project-owner-ui-client-verification-evidence/originals/author/api-format-check01/raw.log) | exit1，auth和其测试格式不符；format03后check02通过 |

原失败不改写为绿色。较早未格式化或未完成源码只有输入指纹，归档不据此重建原字节；各轮原命令、结果和 before/after 指纹保存于source-map，重复指纹文件按SHA复用。`--write` 格式轮的输入变化与只读检查前后同分别保留。

## 未验证范围

本块没有独立全 Vue 类型检查、全部旧 mutator、Session/controller、真实 Router/App、页面状态、PG/HTTP/browser、八布局或发布验收。新页面、唯一 Cookie owner 与原意图恢复仍须后继冻结组合；作者完整页面测试也不能由本次纯返回函数替代。前端 README、完整24路径Owner UI、完整D27、创建HTTP与生产SPA均不在本次交付内，三个历史停止项没有被解除或重试。

本次只归位本报告与证据目录。68个逻辑原件复用为56个归档文件，原始证据432672 bytes；产品源码、cache、node_modules、二进制、dist、大依赖图和私密运行材料不复制。[归档检查](project-owner-ui-client-verification-evidence/archive-checks.json)只核字节、Git定位、引用与格式，没有重跑业务。两份原Vitest JSON没有末尾换行，见[格式例外](project-owner-ui-client-verification-evidence/format-exceptions.json)，原字节保持。

root 后继实际 Git 核对追加：`20d45a72..0ed093f1` 检查另观察作者 `api-types01/raw.log:4`、`api-unit01/raw.log:34`、`api-unit02/raw.log:13`、`api-unit03/raw.log:13` 的末尾空行，逐项与原件 SHA／字节核同，已加入格式登记，保留不改。此前作者归档检查及两原JSON无末换行结论保持其原时点；本追加不重跑业务或更改产品。
