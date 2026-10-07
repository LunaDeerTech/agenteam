# Project Owner Usage HTTP：阶段 B 验证

主线程已采纳独立 **B 限定 PASS**，两路径产品已提交推送 `e7304512e73fdddb25bdbf1c0882400ad5b5f791` 并核远端一致。本结果接受 handler 与受控无服务行为证据，native 三顶层只接受编译与发现；[工作卡 rev1](../work-items/d09-project-usage-read-http.md)的默认 root、真实 HTTP/PG/native 和整卡产品尚未验收。后续 C 七路径作者已实际 ACK，本档不读取或接受活动 C 输入。

## 固定输入与证据复用

两候选均与 `e7304512` 原字节一致：`internal/central/usage/http/handler.go` SHA `e56b1050691ef5c10b0649d4d06e94a6d4a819f832db85ed49e9faca0f0eebe9`；`handler_test.go` SHA `6cb52c4553409b5edad5a7e641d55e966eea48b2a6ef452403e9ec24553102da`。直接复用该提交，不另存产品源码。

[作者 input01](project-usage-read-http-stage-b-verification-evidence/author/input01.json)、[最终输入](project-usage-read-http-stage-b-verification-evidence/author/final-input.json)与[最终状态](project-usage-read-http-stage-b-verification-evidence/author/final-state.json)分别为 `1ac71b65f3566c07a4fb9e03250e82ac226836a134ac4a5289d4d17d6287d6b9`、`f99553429fffdb163086eadda94e28d2cbc1d78f80cff1b4955f5530faf5f3a7`、`54fba54d3a9d08f40553217346a05983a26f90f0dfc072d3c1565168adec3be0`。作者实际 race 图为379包/2349文件，沿 A 的既有输入清单加本轮差量绑定，不重造依赖树。

[独立报告](project-usage-read-http-stage-b-verification-evidence/independent/review.md) SHA `0784e21b67f4da86c04c018b1514f8628a98730fae185719c188fc031c6b4982`；[最终核对](project-usage-read-http-stage-b-verification-evidence/independent/final-check.json) SHA `99e25a4abbf125b0bcb9f1ec306cb9bcc07048af5bd48693e7d7ad080a1b928d`。独立核29个新增/变化产品图输入，最终匹配291个产品路径及2个作者 driver/binary 原件；12项受控执行输入前后相同。A 五源及 common 未变，其 query/DTO/schema 与原失败复用[已提交 A 归档](project-usage-read-http-stage-a-verification.md)，不称本轮重跑。

作者原件保留当时 Summary 依赖“候选、待接受”状态；随后四源已独立采纳并提交 `e6cb70bdfc6ef7569d740f767f7dca3211a2ef7a`，独立[绑定记录](project-usage-read-http-stage-b-verification-evidence/independent/bind-result.json)核其原字节相同，依赖门槛已闭合，无因提交而重跑。此处仅消费 Summary 纯契约，不代表其服务、迁移或生成已实现。

## 接受的 handler 行为

handler 在 HTTPBoundary/RequireHuman 之前建立并传递同一 `min(parent, now+2s)` context，正式构造只接具体 Project、Usage、Account 实例；私有受控测试接缝不成为公开替代授权口。三个精确资源支持 GET/HEAD，复用 A 的严格查询与完整投影，其余方法按原边界返回405/Allow。服务 error/Unknown 保留原 Fault/CommitState，同时返回的非空候选被丢弃；完整 Body.Close 先于成功或 Problem 发布。HEAD 同样完成查询、投影、编码，但不输出 success/Problem body。

正常及失败分支设置 ResponseController 读写 deadline，取消 callback 实际完成后才清除；Body.Close 恰一次，短写、Write/Flush/Close/清 deadline 错误及取消均安全 abort，完整 Write 后同步 Flush。实体防御最多实际读一个 byte，非 EOF、零进展或原始长度/Transfer-Encoding 不合规均拒绝。安全 route Pattern、固定 Problem instance 与既有外层 RequestID/Recover 保持 cause/query 不外露。这些受控事实不代替 net/http socket 行为证明。

## 实际检查

| 范围 | 原始证据及结果 | 适用边界 |
| --- | --- | --- |
| 作者输入发现与编译 | b01 race `go list`、b02 race compile 均 actual0，分别0.215s、11.690s | race 图与二进制可构建，不代表 native 运行 |
| 作者受控行为 | [b03 command](project-usage-read-http-stage-b-verification-evidence/author/b03-pure/command.json) / [raw](project-usage-read-http-stage-b-verification-evidence/author/b03-pure/raw.log)：9顶层/61子例，race binary，actual0，外层5.187s | 包含自然2s、较早 parent、同步认证/服务/Body.Close/Write/Flush 与取消尾部 |
| 作者 native 库存 | [b04 command](project-usage-read-http-stage-b-verification-evidence/author/b04-native-list/command.json) / [raw](project-usage-read-http-stage-b-verification-evidence/author/b04-native-list/raw.log)：仅发现 SlowBody、WriteAndClose、KeepAlive 三顶层，actual0 | 没有执行，未把默认 skip 计为通过 |
| 作者静态检查 | b05 `go vet -race -p=1` actual0，0.668s | 无产品返修或 B 产品首红记录；A 原失败另存 |
| 独立受控 race | [command](project-usage-read-http-stage-b-verification-evidence/independent/independent-go01/command.json) / [raw](project-usage-read-http-stage-b-verification-evidence/independent/independent-go01/raw.log)：4顶层/5子例，actual0，外层5.141s、Go3.251s | 自有 overlay、35s Go预算/45s外层预算，无 listener、native、DB 或容器 |
| 独立格式 | 两源 `gofmt -l` actual0、无输出 | 只读格式核验，未重跑 A schema 或全部作者测试 |

独立 [probe 原件](project-usage-read-http-stage-b-verification-evidence/independent/independent_overlay_test.go.txt)重点验证自然2s及较早70ms parent 的实际到期；故意同时扣住取消 callback 与 Body.Close，先放 Close 仍不返回、不清 deadline，双尾部释放后才 join。自然组实际2.041988s、parent组110.966ms：到期后的受控尾部等待证明实际完成，不保证任意不合作适配器都在2s内返回。Body.Read 已进入后取消也须等实际读返回，不提前 Close、不调用 Usage 或发布候选。

独立另核 GET 部分短写、HEAD 成功 Flush、HEAD Unknown-Problem Flush 的实际尾部：取消后等待释放，GET 已写2byte不能撤回且不追加 Problem/Flush；HEAD 无 body，Unknown仍503。同一受控 writer 顺序 GET/HEAD/GET 完成三对 deadline 设置/清零，旧 callback 不改下一请求。**这不是 native keepalive 或 TCP 提交前零字节验收。**

## 实际 wait 与未完成边界

作者 b01–b05 每条都有实际 direct wait0、前后14项输入一致、adopted waits0；独立 Go 与 format 也实际 wait0，direct PID消失、adopted waits0。原最终核对中五作者进程组及独立 Go 组均无成员，format另有实际终局记录。历史 A 的 PID `22765` / starttime `81512` 未 join 限制继续保留，不能倒填为 B 已回收或原 A 轮清零。

后续仍须证明默认 root 同实例构造/初始化/dispatcher、真实 Session/Owner、PG 锁及事务终局、名称变化/稳定 ID/cursor、三新 PG 组与六旧回归、native TCP/EOF/写 deadline/keepalive，以及每轮独占资源窗口、实际 wait 与双 cleanup。PG 的1s lock_timeout不能替代 HTTP 自然2s证明。后端 README 末件待产品接受；生产 Invocations 真正 nil，无 Runtime Facts/Skills/lifecycle 新绑定，ready503、Object/tools/SPA publication 三停止及完整 D08–D28/E01 未完成边界保持。

归档收尾时的调度事实与 B 原证据分开：主线程已确认 native 三测试只消费已提交 A/B 与 Summary 纯契约、无 app/C import依赖，可与 C 离线实施并行准备；须另冻实际源/binary/driver闭包、取得唯一 loopback 窗口，保持原45s包预算、真实 wait 与双清。独立负责人仅已 ACK 准备门禁，尚未运行；C/PG 的完整14源冻结门槛保持，不能把本调度或 B 的受控 writer 结果记成 native 通过。

## 保存范围

[来源映射](project-usage-read-http-stage-b-verification-evidence/source-map.json)登记26份原件、111015 bytes：作者输入/driver/五命令与小 raw，独立输入、overlay/probe/driver、原报告、命令/raw及实际终局。probe仅将扩展名改为 `.go.txt`，原字节不变，避免进入 Go 包发现。约1.08MB的 race dependency-list raw、重复 list summary 和21119045-byte race binary仅保存SHA/字节，不复制；两候选复用 `e7304512`，A原件及源码/工具/cache不重复归档。原图清单列出的对象不等于本档全部保存。

本次文档归档只核原件 SHA、JSON、固定 Git、raw中的测试计数、相对链接与格式；[归档自查](project-usage-read-http-stage-b-verification-evidence/archive-checks.json)记录工作卡仅页首更新、技术 §1–7 SHA仍为 `36a7fa5c64cbb8d6716f87851b2b5eea895d344319944f6d558c37c55f53a11d`。未执行产品、旧脚本或资源，也未读取活动 C 代码；提交绑定不声称提交后动态重跑。
