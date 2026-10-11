# System Runner UI 历史失败输入恢复

本片已在 main `f86f4215` 正式交付并推送。这里只保存必要的历史源码差额；不包含完整树、依赖、运行时材料或 token。最终技术来源为功能分支 `3137f0b9`，以下三个文件在交付 main 中逐字相同：

| 文件 | 最终 SHA256 |
| --- | --- |
| `web/src/api/system-runners.ts` | `d3e232a8b80923d93203c107f2ca023ed246ab7031aa7635d8297e739954ce71` |
| `web/src/composables/useSession.ts` | `7751bfb1bc41c33ea916ce1f25ce72a10ace97431e673df3127a5ef6d8bdb422` |
| `tests/projectvariable/system_runner_management_web_test.go` | `0f88533dfe9ab4371526fa96785254a3f6bba123c02aee30b78c20a87b60edf4` |

补丁由实际 Git 对象直接生成，采用零上下文以避免补丁内 Go 缩进被当作文档空白错误；恢复时先核基准哈希，再按零上下文补丁处理（如 `git apply --unidiff-zero`，Git 写操作仍由 root 执行）。静态在内存应用后已逐字比对目标对象；没有改动活动产品、运行测试或重放旧命令。恢复旧失败输入时使用隔离副本，不应用到当前产品。这里只恢复补丁列出的文件，不宣称恢复完整原运行环境。

## 前端失败及缺失范围

- [frontend-c882-products.patch](frontend-c882-products.patch)：从最终文件恢复 `c8824052` 的 API/Session，分别得到 SHA256 `090b95d108affed993652b61b4d9082541dae220f656209cb15c4b37f92dfa4e` / `f177f89246ee36110ffbd8640eb0df35dc82de2d3f02d3ea2d03505858a8f8ca`。原 `unit01` 记录输入为该固定产品加新专属测试，合法短页失败；`publish-repro01` 明确记录原 Session 未变，暴露 confirmed 同步消费者退役后仍发布材料。补丁包含该固定版本的实际差额，不等于两轮原未提交测试文件的逐字快照。
- [frontend-17d-api.patch](frontend-17d-api.patch)：独立从最终 API 恢复 `17d0fc32` 的固定 API，SHA256 `1e169291a9e7f9a526ff356844188665f334338710ddaa51f90f673e8871b45e`，保留最后可选 signal fallback 修正前源码。不要叠在上一补丁后使用。原 `build01` 只记录 `d86d0878 plus own 13path frozen completion`，没有逐文件哈希，故此固定源码不证明完整 build01 WIP 输入逐字相同。
- `type01` 只记录 `562d3538 + own API/Session working diff`，原日志是 transport overload 类型错误。早期 API 泛型/重载签名的原 WIP 字节未保存，不能用上述较晚 API 冒充。
- `unit03` 原记录包含在制 publication guard 与页面/Session 控；修复前 Editor 首焦点 WIP 未保存。后继采用原 UiDialog 的 `data-autofocus`，但不倒推生成修前“原件”。Web owner 已确认未另存上述未提交 WIP；原调用与 FAIL 日志仍保留。
- `retirement-repro01` 实际退出 0，不归入失败。修后各次 client/session/state/page 与 build02 结果沿原记录，不合并重复计数。

## Go 夹具的精确历史文件

从最终 `system_runner_management_web_test.go` 开始，按表中顺序应用；每一步都恢复该轮原记录绑定的完整单文件。

| 顺序及补丁 | 目标固定源与原失败 | 应用后文件 SHA256 |
| --- | --- | --- |
| 1. [go-final-to-native02.patch](go-final-to-native02.patch) | `251c3be2`（技术 `72e0257e`），恢复 Audit `resource_id::text` 与同参数 UUID 用法 | `cc98ffed0344912e1b326d6948098f3dc94b941b5cfc9872662f13431d92b337` |
| 2. [go-native02-to-native01.patch](go-native02-to-native01.patch) | `ef3e4502`，恢复对 token wire 文本求摘要 | `9e1a581421cc17b78f441ca1d43be9f339dbcac414758af6389900c1b11f470d` |
| 3. [go-native01-to-compile01.patch](go-native01-to-compile01.patch) | `6d2367ea`，恢复缺少 `updates` 字段的首次编译输入 | `4443edc92f09cc82b3c21c701171520801a134e72fe5c469a7bb2714c33252ae` |

`compile-web01` 原编译退出 1，五处错误均指向未定义计数字段。`native01` 两场景首次 Create 观察失败，`native02` 仅正常浏览器场景通过、Go 计数失败，lost 未完成恢复；两轮 wholeFAIL 保持。错误日志未包含实际内部 SQLSTATE，摘要/UUID 源码契约错误不能被追认为原轮已经动态证明的唯一首因。修后 `native03` 的有限接受范围见[正式卡](../../../docs/development/work-items/d15-system-runner-management-ui.md)。

## 原方法与输出保留

原输出根为 `output/ai/system-runner-management-ui/`，清理时应完整保全既有内容，迁移位置由实际保全记录说明。本次没有移动或改写 output。特别保留：

- `checks/` 中 type01/build01/unit01/publish-repro01/unit03 的原调用、结果与日志，以及修后结果。
- `compile-web01/` 至 `compile-web04/`、`native01/` 至 `native03/` 的原配方、源码绑定、结果、原安全证据及编译产物；未添加任何 token 或原 mutation body。
- `compile-web04/launcher01-result.json`：首次 wrapper 在启动 Go 前误用旧目录，被原防覆盖断言拒绝。失败脚本后来被修正，旧脚本文字没有单独快照；只保留原错误记录，不从新脚本反造原件。
- `methods/list02-source/` 的原 Playwright 转译 JS/map 及来源记录；原 JS 307/310 行对应固定 TS 124/125 行，sourcesContent 逐字一致。其余方法控原结果不变，没有新增失败方法版本。
- `integration01/`、`integration-paths.json`、`retirement-scope.json` 及三轮原外部日志 `/tmp/srmw01/ui-9a211ff1ad4e4743.log`、`/tmp/srmw02/ui-6f9c917637894493.log`、`/tmp/srmw03/ui-161f3afca6fa4d71.log`。退役范围文件是在主线交付前准备的范围记录，不替代现已交付事实。

本 owner 已停写产品/方法，没有存活的 Go、Node、native 或后台任务；不再依赖本树作为后继输入。私有依赖尚未迁移或删除，其他 owner 的下游使用仍由 root 在实际清理前确认。热 SkillHTTP Go cache 与 SkillsMilestoneResume 只读模块缓存不属于本树清理范围。
