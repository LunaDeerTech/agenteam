# Project Usage HTTP Stage A independent result

结论：**A 限定独立 PASS**。冻结五源的实质静审、必要 Go 纯行为与标准 schema 检查已闭合，未发现需返修产品缺陷。这个结论只接受 RequireHuman、strict query、安全 DTO/完整编码与正式 schema 五路径，不是整卡 HTTP/root/真实资源接受；原执行失败及 a04 未 join 编译子进程继续明确保留。

固定输入：作者 a02-current-five.json SHA256 `8d78d3b4de0fb8fcb8f0fb8af6ea4ff92fe03c40554d24014b0be422b0e8b7a9`。候选五源加固定 common.json 逐字节保存在本目录相同相对路径，精确 SHA/字节见 `inputs.json`。对作者既有 a03 清单中288个仓库输入作实际复核：五候选+common匹配本输入，其余282个匹配固定 `6fa2ee721a75ea34a1ccd6523b8b25d68328c5b9`。后续文档 HEAD 变更没有改变产品输入。没有复制源码树或重造大型执行清单。

末件 `stage-a-final-input.json` SHA `ba247577940319f250b2a5d409848db6c569e38aa4e5d8bdaa5f82ab2ee4a3e0`、`stage-a-final-state.json` SHA `199222a69c75431bf9c596942f2e6c985fb9ca9fe009baed9c7c8aa0feedf7f1` 已核；独立实际哈希检查追加 race11文件及7个driver/binary/schema/tool原件全部一致。作者原 nonrace 2348输入与后补race图不混称单次完整图；scratch driver的明确subreaper修补保留原件。

## 已通过的实质边界

- RequireHuman 复用原 check/csrfSession 和 safe/unsafe分类，只返回合法 Human，不调用 System授权或接受caller identity。RequireSystem及后续原方法与固定 main 字节完全一致；作者真实Account Service/Authority配合受控SQL行的36子例验证了边界调用组合，不冒称持久Session/Owner证明。
- strict query 在分配通用解析前限 raw32768B；一次解码、解码键去重、空值/未知键/非法UTF-8/NUL/分号/空段拒绝；limit canonical1..100，原typed Filter/Instant/UUID及八group保持。独立probe另核一次cursor解码、8192 decoded UTF-8 bytes含界，以及newline/Unicode后缀与invalid calendar输入。
- 投影调用完整Page/Invocation/ProjectRef/Aggregate校验，包括隐藏字段；精确Project/Owner、页大小、group及AsOf保持。字段required/nullable/closed，历史名字与live IDs分开，六nullable TokenCount/六计数原义及大整数JSON字符串保留。独立probe核合法agent.meeting_id/tool.execution_id不被删去、大统计数、末行隐藏损坏零候选且Committed状态、编码后取消和默认HTML/U+2028/U+2029转义。
- 闭集保留Protocol5、ConsumerKind5、Purpose12、Dispatch5、FinalStatus4、ErrorCategory14、GroupBy8及条件。ProjectRef四lifecycle schema只表达正式值；不证明deleting项目通过真实read gate。独立probe保留正式Instant/GroupKey允许year0000的行为，未用额外format检查收窄历史。
- 最终完整1MiB编码门保留。已核作者实际最大合法默认转义测量：Invocation4564B、List100464720B、Aggregate10093858B、Project49672B，均满足卡片保守界；不是对任意伪造service cursor/其他内部对象的新增容量承诺。

## 实际行为与证据

独立命令均无监听/数据库/容器，离线readonly module，45s外层预算，自有scratch overlay及证据。复用root已允许的热build cache；不改候选源码。各 `independent-*/command.json` 保存 argv/cwd/env、实际exit、前后源hash、direct PID/实际wait、raw hash；raw.log保存原始输出。

| 检查 | 实际结果 | 证据目录 |
| --- | --- | --- |
| 四个独立Go风险顶层，`go test -race -count=1 -p=1 -timeout=35s -overlay=... -run ^TestUsageIndependent -v ./internal/central/usage/http` | exit0；外层2.082s，Go1.019s；四顶层PASS | independent-go01 |
| 独立手造382个合法/非法DTO、闭集/关联、所有required和额外字段、精确整数与Unicode例；Python jsonschema4.26/referencing0.37 Draft202012 | exit0；382全部PASS | independent-schema-final |
| Node24实际ECMA Unicode regex：34 pattern编译、147 pattern边界、128/129 astral字符计数 | exit0；全部PASS | independent-node-final |
| 三路径六操作、精确parameter集合/required、安全Session、HEAD无content、closed DTO、原RequireSystem字节 | exit0 | independent-structure-final |
| 全250个schema引用和实际from参数standard validator路径 | exit0；250全解析 | independent-ref-final |
| 上述288仓库依赖字节 | exit0；无漂移 | independent-dependency-final |
| 四Go源gofmt -l | exit0；无输出 | independent-format-final |

schema的format为annotation；字节限、日历/先后关系、ID相等及计数加法等继续由正式Go Validate执行。Python标准schema行为与Node实际ECMA pattern证据分列；没有宣称使用不可用的Ajv或完成OpenAPI通用元规范工具验收。

复用并核了作者同a02的 a05/a07 race compile、a06 Account5顶层36子例、a08 Wire11顶层99子例（含129标准schema例、Node34pattern/40scalar/2Unicode）、a11/a12分包race vet及a10 race graph。各实际exit0、前后源hash相等。必要原件路径/hash和独立数出的测试数见 `author-evidence-references.json`，不把复用结果称为独立重新执行。

## 原失败、实际尾部与未验证

保留作者a01离线依赖缺失setup失败；a04两包cold race命令及a09 nonrace vet均45s外层TERM、实际wait-15，原raw为空，无测试assertion开始证据。后续按root明确许可分包、同45s、使用已热race配置通过；不声称nonrace vet通过或原失败同轮全绿。a09 subreaper实际wait adopted编译子进程后零成员。

独立schema首轮runner错误地用$id覆盖已注册resource，382例均PointerToNowhere；原script/raw/result分别保留为 `../a02-schema-independent-run01.py`、`schema-run01.raw`、`schema-result-run01.json`。修的是独立runner URI定位，候选不变；同382例再跑及带实际wait元数据的最终运行均通过。这是验证工具错误，不是产品RED。

末尾实际 `/proc` 扫描见 `tail-result.json`：独立7个runner组全空、全部direct waits0且adopted0；作者a05–a12组全空。作者a04编译PID22765/starttime81512仍Z/PPID1/pgrp20527，**没有被作者或本worker实际wait，不能称原轮进程清零或已join**。没有操纵PID1或把它计入后继cleanup。该事实限制原执行终局证明，不改变同输入后续纯行为结果。

Stage B的handler/HEAD实体实际执行、预认证自然2s/parent deadline/Body.Close/写Flush/callback终局，Stage C/D的当前PG Session/Owner/锁/游标/名称变化、默认app.Run与native TCP/keepalive，三新PG组/六旧真实回归和产品README均未在A实现或验收。生产Invocations真正nil、Runtime Facts/Skills/lifecycle不新增绑定；Object/tools/SPA停止项及ready503边界不变；用户已在175694bf明确由系统管理员统一选择会议Summary模型，当前正式设计待审、实现未交付。

实际写入仅本worker `/workspace/scratch/usage-http-verification` 计划、六个必要冻结原件、独立脚本/overlay/raw/证据与本报告；没有业务/Git写入。所有自有命令与源读取已停止，无自有监听/DB/容器或活进程。可由root采纳限定A结果并另授B；不需要产品返修。
