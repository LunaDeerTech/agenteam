# Project Owner Usage 只读 HTTP 根装配验收

2026-10-07。主线程已采纳独立限定 PASS：本卡授权的 A/B/native/C/D 组合完成，覆盖当前 Session/Owner 下的三个 GET/HEAD 资源、默认 root 装配及真实 PostgreSQL 读事务。C 七技术源与最终 backend README 已提交推送 `03a4a0b87b21c9d3583c01dc3d543bb4ee31ea05`，远端一致由主线程核实；连同先前 A5/B2，完整交付为 15 路径。D08、完整 D09 和 E01 不因此完成，ready 仍为 503，生产 `Invocations` 仍为真正的 nil。

固定结论来源为 [独审最终报告](project-usage-read-http-root-verification-evidence/objects/0c34820608069f9458c84c71a96b83326855324f6e78469367cc1fa46fa0e244) 与 [独审最终结果](project-usage-read-http-root-verification-evidence/objects/2202bcfc8c4ea0c67565792bfc8ca5e04646c907976665f8a9b14b3db1d7da48)；作者版本组合见 [D-final 原件](project-usage-read-http-root-verification-evidence/objects/89258e5b24b8f57a133b4368e9fdfa2df09575bb17a320430b638e34b90fc4a6)。旧 [A](project-usage-read-http-stage-a-verification.md)、[B](project-usage-read-http-stage-b-verification.md) 和 [native01](project-usage-read-http-native-verification.md) 归档原样复用，保留各自原失败与限制。本归档没有重新运行产品或任何证据脚本。

## 固定输入与接受范围

[fourteen-v05.json](project-usage-read-http-root-verification-evidence/objects/b2316eed06adf0983e5ecd93249a5891d414638e0c495289ae0ef577c957e59b) 固定 14 个技术文件，SHA `b2316eed06adf0983e5ecd93249a5891d414638e0c495289ae0ef577c957e59b`：A 五路径、B 两源，以及 C 的 app 三源和 model 集成测试四源。A 产品提交为 `ba7ce7296b7c08d976d3fc3ce1e1d0b727a3f04f`，B 为 `e7304512e73fdddb25bdbf1c0882400ad5b5f791`；Summary 四源纯契约依赖复用 `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a`。技术源和依赖通过固定提交及指纹定位，不另复制代码树。最终 README SHA `1abf35fbdb8d4498dd886f6b16e40792ad942ba3f8e6bd23576232c2ac35680d`，其[作者自查原件](project-usage-read-http-root-verification-evidence/objects/e7d6e13638ad949e80efe085dd438f3f6cad56ea9b914b5fb5480a6ee36cd592)与14源不变检查单独保存；文档收尾没有产品重跑。

静审确认 Project/Usage 使用同一原数据库对象与 Account Authority，Project Sessions/Routes 和 Usage Sessions 均绑定正式 Account，Usage Projects 绑定新建 Project Authority；游标继续使用原配置。构造过程不做 I/O，Secret→Model→Usage 初始化共用原启动 context，Usage 只检查现有 schema；默认 `app.Run` 不注入测试 handler、Skills 或 Facts。路径分发保持既有路由链和单层外部 middleware。

HTTP 当前名 resolve 与稳定 ID list/summary 分开授权。每次读在正式事务内检查当前 Session、Owner 和项目状态；其他 Owner 或管理员没有跨 Owner 豁免。完整 Committed 且 context 有效才发布安全投影，Unknown、回滚、取消、游标及 Rows/Close 错误保留零候选边界。大整数、零、unknown、八种聚合和稳定 ID 游标沿原 Usage 契约，未新增 writer、迁移或产品初始化数据。

作者最终闭包 gate 校验 3127 个文件、799 个已接受本地输入，摘要 `6d17f60670bcbe54414bbe000bc3687f9dca3b3c27ebc0e966ff191209fabe21`；i01 加入独立 probe/生成 test main 后为 3135 个固定输入、同样 799 个本地输入，前后摘要 `aae65cf6e22e4bedd7a2cc7f785575bc158ed9fabbfeb41fe29fa554fd2833cf`。[closure-v05.json](project-usage-read-http-root-verification-evidence/objects/dc74e9acfb271ce4cbf40db9c5af3db29b6c3236b13f85ebfe6d31768609599f) 保存小差量；完整依赖 manifest、Go list 大图、工具和 binary 仅记 SHA，不纳入本归档。

## 作者结果与原失败

作者离线原 [HANDOFF](project-usage-read-http-root-verification-evidence/objects/180b8b4343d55df256fcd26c8f5b92554294f7a967bfa11ff0876f1b2b17b76a) 和 [case matrix](project-usage-read-http-root-verification-evidence/objects/e1c559eef6a8d1a51e43d53212b07a2154aff9d85305d6083bb684c784baee31) 保留当时“真实资源尚未运行”的状态，后续以 [D-final 原件](project-usage-read-http-root-verification-evidence/objects/89258e5b24b8f57a133b4368e9fdfa2df09575bb17a320430b638e34b90fc4a6) 为准。新 app pure 三顶层通过；原 root 回归、完整 fixture 20 包编译及 process TestMain 的两个命令构建、CGO0 outbound helper 和 Python schema 闭包均有固定输入。最终受影响 compile/vet 与 driver gate 通过；编译、发现与离线 gate 本身不作为真实产品验收。

| 轮次 / 输入 | 实际退出 / 秒 | 本轮通过与保留失败 | owned PID/starttime |
| --- | --- | --- | --- |
| d01 / driver v02、源 v03 | 1，fixture spawn 前 | PATH 缺 Docker；无 fixture command/raw，不算真实轮 | 未启动 fixture |
| d02 / driver v03、源 v03 | 1 / 64.796 | ProjectionAndPath 通过、标准 schema 11；Root 和 Terminal 预期错误保留 | 106 |
| d03 / driver v04、源 v04 | 1 / 59.825 | AuthorityAndTerminal 通过；Root 诊断响应误用 Account helper 失败 | 92 |
| d04 / driver v05、源 v05 | 0 / 55.309 | RootBinding 通过 | 88 |
| d05 / driver v05、源 v05 | 0 / 65.681 | 原 System 两顶层和 Usage 两顶层通过 | 90 |
| d06 / driver v05、源 v05 | 0 / 48.192 | 原 Project 两顶层通过 | 85 |

这形成九个不同顶层的版本组合。Projection 源、fixture、生产与共用 root helper 均与 d02 相同；Terminal 与 d03 相同，后续仅修正无关 Root 诊断断言。独审核验复用关系，未把它写成最终版本九顶层全量重跑。

保留的首红与修订如下：

- c02 app 编译的 test double 签名错误、closure01 生成器假设错误；初始 fixture Account 清理复用已过期 Drain context，且未实际等待 forceDone/Joined。两版旧 fixture、finding 与差量保存，最终用新 Force context、重复 Force(background) 等待原 forceDone 并显式 Joined。
- 原 top 末尾 defer 的耗时检查不包含 `t.Cleanup`，driver 后改为从精确 RUN 到最终 PASS/FAIL 的完整 120 秒 watchdog，并保留真实 TERM-ignoring 自有尾进程的 TERM/KILL、adopted wait 原 gate；真实验收各轮 forced actions 均为 0。
- D01 的 Docker PATH 首红发生在 Popen 之前，[launch.raw](project-usage-read-http-root-verification-evidence/objects/1695e59daa6d2fb7fe468ad37ff3653c9b0ac630ebd3588005e414d5d479c996) 与 [pre-spawn-failure.json](project-usage-read-http-root-verification-evidence/objects/af3be3c4049203e8ac470eda163d594cc7742cfbf94cffe016ff9e467438af7a) 保存原 traceback 和未启动说明；不伪造不存在的 command.json/raw.log，后继 v03 精确绑定 Docker 路径和 SHA。
- d02 把原 `/readyz` 的 503 Problem 错当 diagnostics 的 `ready:false`，并把真实 PG 锁超时错预期为 503。修正保留既有 `InternalError/NotCommitted/DATABASE_LOCK_FAILED/55P03`，对应 HTTP 500。
- d03 的 Account helper 要求三个安全头，但原诊断路由没有该契约。仅修正新测试为显式 503/200、原 Problem 与 diagnostics false 断言。C/D 真实轮返修没有改变生产字节。

身份经正式 Bootstrap、invitation、mail worker、redeem、login/Logout；测试准备的 Skills 与 Runtime Facts/wire/ledger 仅用于 fixture。真实 wire Result/Close/Joined 后产生记录，不用直接 SQL 身份或账本捷径。作者 Terminal 的受控 writer 证明事务终局发布与取消尾部，不外推为 TCP commit 可见性。

## 独立验证

[独立 pure 报告](project-usage-read-http-root-verification-evidence/objects/1d79f2191fc8cc8dadc87a411c089803de38e78b7a9fa07a00efa6fa3316dc72) 固定 app race probe 在修订后通过 1 顶层/3 子例、actual 0 / 1.121 秒：实际 Usage schema 查询保留同一启动 context 与原错误，Model 错误或取消阻止 Usage 查询。原私有 fake 错把 `pgx.Rows` 用作 `*postgres.Rows` 的编译失败、原 probe 和修订输入保留。成功 schema 检查由后面的真实 PG/root 轮证明。

独立真实准备的私有初稿也重复过 diagnostics body/Account helper 假设，两个已编译版本与修订后的最终 probe 都保存；这些只在真正运行前改动私有断言。[独立 preparation 报告](project-usage-read-http-root-verification-evidence/objects/00bc5010b5e69ac33c477644cf73a04c6d9a0b7bf20f74db314e567580144255) 当时只证明编译/发现，不倒填为真实执行。

[i01 command](project-usage-read-http-root-verification-evidence/objects/a015c3fb1570b8a16cc5c5b6cd4bd646af1fa61155fe633dddbda74a826101aa) 对应唯一独立窗口 actual 0 / 61.122 秒，原始输出 [i01 raw](project-usage-read-http-root-verification-evidence/objects/998797039b1c49b4f1f2e601e5bd9f5573709a582100a2e8d4b08dd36610d311) 中两顶层均通过：

- `TestIndependentProjectUsageHTTPAuthorityProjection`，5.48 秒：精确大数/零/unknown 聚合，包含超过 2^53 的 count；标准 schema 3 例；正式同用户新 Session 复用稳定游标；HTTP 认证后、真实 Usage 事务前正式 Logout 提交，后续事务重验 Session，返回 401 且无候选。
- `TestIndependentProjectUsageHTTPDefaultRootIdentity`，5.65 秒：默认 `app.Run`，正式 Project/username 改名与旧名复用、稳定 ID/跨项目游标、Owner/外人/管理员权限、GET/HEAD、旧路由与诊断；在一条真实 keepalive 连接上完成响应并实际关闭 root。

固定 Go 1.27.1、`GOMODCACHE=/workspace/go/pkg/mod`，真实调用原 `scripts/test-objects.sh -run` 精确选择器，保持 integration/race、count=1 与包 6 分钟预算，`GOFLAGS=-mod=readonly -buildvcs=false -p=1 -v`，独立轮增加固定 overlay。新顶层 120 秒 RUN→结果 watchdog 覆盖 Cleanup；main 与 watchdog thread 均实际等待。该轮开始空闲空间为 23.6 GB，固定源、工具与 driver/overlay 输入前后均匹配。

## 实际等待、清理与 daemon 限制

五次作者真实窗口及 i01 各使用同一正式七资源拓扑：精确 PG17/PG16 digest、outbound 与 object 原 fixture，以及固定 MinIO `RELEASE.2025-10-15T17-29-55Z`，binary SHA `dc5298474f0bc87a068f0b1135c583bb1278c17c11c512212ed7644a238c89c8`。恢复前置复用已有记录，不改 helper 或以同 tag 替代镜像。每轮保存命令/env、driver、frozen/input 前后、完整 labels/canonical Mount、精确资源 ID、PID/starttime、实际退出、watchdog 与双清。

六个真实轮的 owned 链全部终止，七个自有资源在两次检查中不存在、原 baseline 不变，runtime 文件为空；adopted waits、forced tail actions、monitor errors 均为 0。i01 观察 90 个 owned PID/starttime；外层 driver PID 125121 和 main PID 125201 亦在实际退出后确认不存在。[i01 cleanup](project-usage-read-http-root-verification-evidence/objects/478232ee51935632d711b62981624f7e23ae2a04b00911142ce9ef7277cc14f0) 与 [i01 result](project-usage-read-http-root-verification-evidence/objects/cd9978f66c2df12b11b8d608a69ca6d27b2421258b71843ee91fa1b023579c44) 原样保存该限定结论。

**owned 链双清不等于全机零残留。** 作者 [daemon 差量](project-usage-read-http-root-verification-evidence/objects/e70cb8ec0daff2d389583cd04add37331cf95167bc8f94eb3a86baaf68a0e678) 和独立 [独审最终结果](project-usage-read-http-root-verification-evidence/objects/2202bcfc8c4ea0c67565792bfc8ca5e04646c907976665f8a9b14b3db1d7da48) 明确记录 PID1 所属 `containerd-shim` zombie：d02 前为 0，d02/d03/d04/d05/d06 后依次为 4/8/12/16/20，i01 后为 24。每个真实窗口新增 4 个，这是时间窗口差量，不能精确归因到某个 container。i01 新增 PID/starttime 为 `125634/510024`、`126098/510444`、`126464/510632`、`126961/510810`；均非任务后代，没有实际 task wait，未操作 PID1 或 daemon。既有 A 未 join compile zombie 与其他历史 zombie 同样不被倒填清理。

继承 fixture 的内部组件 join 超出其既有契约的部分未获新证明。原生 B 的 3 顶层/9 子例 actual 0 与 wrapper exit 1、空端口双清无效、后续只读恢复先 TIME_WAIT 再两次空仍完全沿原档；本轮没有 native 重跑。

## 原件与归档检查

[source-map.json](project-usage-read-http-root-verification-evidence/source-map.json) 按原绝对路径、SHA 与对象位置映射，内容相同的原件只保存一次。保留小输入、原失败源码/日志、实际命令与清理原件、driver 和独立 probe；这些脚本仅为证据数据。完整依赖 manifest、大型 Go list 原文、binary、缓存、工具链、fixture 私有 recovery 凭据与运行目录未复制；指纹条目不能代替缺失原件的可携带复现。A/B/纯契约源码与旧归档通过原提交复用，本文不另建依赖树。

最终[归档自查](project-usage-read-http-root-verification-evidence/archive-checks.json)通过：296 个来源路径按 SHA 去重为 228 个原件对象（1078950 字节），186 个原 JSON 来源解析、105 项仅指纹来源核对、14 技术源及 README 固定输入匹配、相对链接和作者文档格式检查完成。卡片只更新页首，技术 §1–7 SHA 仍为 `36a7fa5c64cbb8d6716f87851b2b5eea895d344319944f6d558c37c55f53a11d`。本次禁止 Git 操作，格式检查使用只读字节检查，不声称运行 git diff --check；未执行产品、旧脚本或任何资源动作。

产品边界保持：仅当前 Owner 的只读 Usage HTTP 与默认 root；无生产 Runtime/Invocation Facts、Project Skills/初始化/生命周期、UI、Execution Summary、全模块或 E01 通过。ready503，Object join、OpenAI tools 与 SPA publication 三项停止边界保持。
