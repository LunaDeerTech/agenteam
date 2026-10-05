# D09 System Model HTTP 独立验收

**库级 System Model HTTP 与被动 credential 命令查证已独立 PASS，主线程采纳并精确提交推送 `ac5b4c65e88ed0ec1813fbf341035c88ca9a38c7`，远端一致已核。** 本卡交付 17 路径：9 生产/契约（含 OpenAPI）、8 测试。生产 root 尚未挂载；根装配规格已授权制定，候选卡已冻结、独立静审中，尚无业务实施权，不表示 Provider/Invocation 执行、Project/Summary 或完整 D09 完成。

固定测试输入为 `9110686a507716d8b4e042a5562a22507658adcb` + 已验 Secret20 `8ad6759` + HTTP 最终 17 源。[最终清单](evidence/d09-system-model-http-verification/author/fixture-input-02/manifest.json) SHA 为 `caa5b3d485aaee6d07cec51a54915106d93c652f36ae5aea71bc7d8b4ee21a7a`，生产 9 源与静审 prod02 相同。实施者 `d08_registry_backend` 与独立验收者 `skill_verification` 分离；[V 原报告](evidence/d09-system-model-http-verification/verification/final-report.md.txt) SHA 为 `67c55b9cab5c285cd22cb55da8baf74972383632ae6943d43c5a4344218da20b`。本报告只归档已完成的验证，不新增测试声明。

## 作者有效覆盖与复用

| 原组与输入 | 实际覆盖 | 边界 |
| --- | --- | --- |
| new-configuration-01，input01 | 5 顶层/26 子例，model 13.477s | 生产、配置测试及 Model 分支未变，限定复用；未在 input02 重跑 |
| new-credentials-02，input02 | 5 顶层/17 子例，model 10.849s | 首红所在完整组复验，含实际 Secret Committed→Store Unknown→HTTP503 与删后历史材料重放 |
| compat-model-01，input02 + Secret20 | 5 顶层/5 子例，model 8.596s | 原 Model 权限、引用与 rollback 回归 |
| compat-account-secret-01，input02 + Secret20 | 5 顶层/0 子例 | 原 Account 3 项（9.530s）及 Secret 2 项（4.253s） |

共 **20 个不同顶层/48 子例显式 PASS**，新旧各 10 顶层，无 Skip。原 driver `sh scripts/test-objects.sh -run <exact selector>` 的 `-race -count=1 -timeout=6m` 不变，GOFLAGS 为 `-mod=readonly -v`。V 独立逐 SHA 核对[作者矩阵](evidence/d09-system-model-http-verification/author/coverage-matrix.json)的 79 个引用，连同原 argv/env/exit、日志和清零记录全保留；[机器核对](evidence/d09-system-model-http-verification/verification/evidence/author-final-checked.json)记录每组版本，不能写成最终 17 一次全量通过。

纯检查同样按输入限定：race02 覆盖最终 Secret/contract/Model，Account/httpapi 复用 race01 未变部分；最终生产 9 源与 vet01 和两 cmd 实际 build 输入一致；最后两个测试经 compile-integration02/vet-integration02 通过，compile-only 不计动态覆盖。早期 unit/compile 不扩称最终版通过。OpenAPI JSON/引用、22 operations/schema 检查保留原结果，没有用空日志代替 exit 元数据。

## 原发现与修复

- unit01 的时间 helper 拼写及含 opaque 函数 LockKey 的比较使新测试失败；修正为真实 Canonical + mode 比较。unit02 的 Model OpenAPI 相对路径错误在 unit03 修正。原 raw、输入和精确历史差量保留。
- F1 是[production01 独立静审](evidence/d09-system-model-http-verification/static/production-review01/review.md.txt)发现：读 Tx 已 Committed 后 caller 取消可能仍交 observation。prod02 仅加新 lookup 成功尾部的 context 检查，返回零 observation 并保留 Committed 安全 Fault；[修后静审及窄 delta](evidence/d09-system-model-http-verification/static/production-review02/review.md.txt)和后续 present/absent 真实通过均保留。**F1 没有修前动态红记录。**
- new-credentials-01 真实首轮为 4 顶层/17 子例通过、1 顶层失败：Secret 预期 503 实得 200，后续删后历史段未执行。测试 observer 对 raw Canonical 求 SHA，而正式 Secret 使用 CanonicalJSON 后 Digest，未命中最终 receipt/Unknown；没有据此认定生产吞 Unknown。
- 修复只改 fixture 和 credential 两测试，使用正式 `cursor.Digest` 并断言语义命中及一次 ExecuteWrite，原 503、材料和历史强断言不变。修后真实日志确认 final receipt Tx Committed→Store Unknown→HTTP503，随后已删 create/update/delete 的完整原材料重放通过，错材料仍 409。原首红不改写为成功。

作者原报告 SHA `a5284098af5707420f0a61a8010f82e5a3f6f10ab69b28fbeb4d46480f6d9562`；矩阵 SHA `d5693830cc61c88f4d36ea8b99d252d751589371c3c4b4451b98c1965ecefe21`。V 的启动包装器第一次被 Python 语法拒绝，发生在全部 Go/Docker 操作之前；[原脚本](evidence/d09-system-model-http-verification/verification/evidence/startup01-script.py.txt)与[错误记录](evidence/d09-system-model-http-verification/verification/evidence/startup01-result.json)保留。修后先作 Python 内存语法检查再启动 driver，同源 py_compile 通过；该拒绝不计产品测试失败，未改变 probe 或 17 源。

## 独立实际结果

独立命令为 `sh scripts/test-objects.sh -run '^(TestIndependentSystemHTTPPassiveCurrentAuthorityAndReadFailure|TestIndependentSystemHTTPLateReceiptStillRequiresOriginalMaterial)$'`。Go 1.27.1、离线 readonly 依赖、自有 cache/runtime、本机 socket 和空自有 Docker config、原 race/count/timeout 均见[实际结果/env](evidence/d09-system-model-http-verification/verification/evidence/dynamic01/result.json)。两候选版本的离线 race compile 各自保留元数据；唯一真实一轮 **2 顶层/4 子例全部显式 PASS**，tests/model `5.516s`、driver exit 0，无 Skip。

实际增量覆盖：HTTP 已核 admin 后的真实 User EX 撤权，被 Secret 读事务中的当前 Account Authority 拒绝，receipt SELECT 为 0；只读 Tx 实际 Committed 后装饰 Unknown 的 absent、及取消 caller 的 present 均返回 HTTP503、零 observation，保留原状态/取消 cause，两案精确 Command SH/User SH、零材料/摘要 SELECT、业务行与 nonce 不变。迟到 receipt 场景则真实首读 absent，随后同 identity update v2 与独立 delete v3 提交、Metadata NotFound，第二次被动查到 v2 历史 receipt，仍调用一次原 Execute 验完整材料；同身份错材料返回 409，不新增业务事实。

probe SHA 为 `4b61aaa880ef15caf99f7b710a75972d0dac62eb7f970cc796bd1d760229688c`；[实际 raw](evidence/d09-system-model-http-verification/verification/evidence/dynamic01/raw.log) SHA 为 `703f9f714ff51dfc7a5ce3e9dc2f444be9e4027ce38089ff45d691c9d5028b47`。运行前后最终 17、依赖 20 和 probe 均匹配，不消费活动 Object/Artifact 源。

## 资源、证据与后继

作者各实际组均记录自有 4 容器/3 网络存活 ID/name/nonce-label，执行后两次 exact ID absent，原 2 容器/4 网络不变，自有进程/runtime 为 0，源末检匹配。独立同样实际核 4 容器/3 网络存活身份并双清理；原 2 容器/4 网络的 ID/name/labels/状态一致，driver PID 已消失、自有进程/runtime 空，窗口已交回。原资源记录只描述实际检查时刻，归档阶段没有再次启动或查询 Docker。

[持久入口](evidence/d09-system-model-http-verification/README.md)保存原 79 引用、F1 静态链、两测试原 patch、独立启动拒绝/compile/probe/真实运行与资源原件。最终 17 从 accepted commit 读取，Secret20 从已验提交读取；unit01/unit02、production01、fixture01 仅保存小型差量。unit02 的旧 Secret 测试副本曾缺失，V 逆转 F1 追加测试后精确命中原 SHA，恢复出处明示，不冒称旧物理副本仍在。原日志/patch 字节不改，[空白例外](evidence/d09-system-model-http-verification/whitespace-exceptions.json)只列实际逐文件检查的精确原件；[归档检查](evidence/d09-system-model-http-verification/archive-checks.json)记录原 SHA、源码恢复、链接与格式结果。本阶段无 Go/Docker/网络或 Git 写操作。

Unknown 只证明实际 Committed 与正式 Store 合法返回 Unknown 的处理：Model 原 replayScope 确认成功可返回 200/原 receipt，仍 Unknown 才 503；Secret 直接 Unknown 返回 503。没有网络 COMMIT 丢 ACK、存活 writer 或 ROLLBACK 干预证明。生产 root 未挂、根装配规格已授权制定、候选已冻结、独立静审中且无业务实施权；Object/Artifact/Project 生命周期阻断及完整 D08/D09/D28/E01 未完成保持，Provider/Invocation、Project/Summary、实际执行链均不由本卡替代。
