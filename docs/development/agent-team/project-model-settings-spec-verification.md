# Project Owner 模型设置 rev1 规格静态验证

2026-10-08，root 已接受完整整合候选的 [SPEC STATIC 独审](project-model-settings-spec-verification-evidence/originals/d411c2ce0dfe46b7-review.md)及正式安装的 [T1/T4 行政差量独审](project-model-settings-spec-verification-evidence/originals/e1abcf0ec30fa19e-review.md)，均无未关闭必修。当前结论仅为完整规格与最小共享源交接接受，不是产品实现或动态验收。正式两份规格已由 root 提交、推送并核远端同：`7b8af244fe87d43699c17c95077ee73971e18411`；本归档没有执行 Git。

| 固定规格 | 字节 | SHA-256 |
| --- | ---: | --- |
| [正式卡 rev1](../work-items/d27-project-owner-model-settings-ui.md) | 91,436 | `7cb91a5d8852f10edd0e777a508784e3d65ea73f50de9cc45e5b0c4682230f6d` |
| [17 端点附件](../work-items/d27-project-owner-model-settings-ui-endpoints.json) | 6,087 | `6c85ae889c8a86eb67695f2eb29773395b58db4f5bdfecc9c9182ff3e62fd9fa` |

两份规格按上述 `commit:path` 固定，精确引用见 [source-map.json](project-model-settings-spec-verification-evidence/source-map.json)。正式卡及其 prepared 副本不重复归档；端点附件与原 T3 `endpoints.json` 字节相同，只保存一个正式引用。

| 阶段 | 必要原件与结论 |
| --- | --- |
| rev0 草案 | [原草案](project-model-settings-spec-verification-evidence/originals/8b78072a09d2c87f-spec-draft.md)、[独审463cd09a](project-model-settings-spec-verification-evidence/originals/463cd09aecf075c4-review.md)：可形成完整结果，无新语义必修；原 Audit/T1–T4 前置仍按当时 pending 保存。 |
| rev1 工程增量 | [engineering](project-model-settings-spec-verification-evidence/originals/9197fc97facd71d1-engineering.md)、[protocol](project-model-settings-spec-verification-evidence/originals/83e3e652e9677a41-protocol.draft.json)、[selectors](project-model-settings-spec-verification-evidence/originals/c36bed42977f75b6-selectors.json)、[独审691ab122](project-model-settings-spec-verification-evidence/originals/691ab12294c0b688-review.md)：补 typed 接口、精确旧 selector 与私有协议；不是正式安装或 binary discovery。 |
| T3 精确 DTO | [T3 原稿](project-model-settings-spec-verification-evidence/originals/8c82af7cd694fa0b-t3-dto.md)、[独审0e60d1ae](project-model-settings-spec-verification-evidence/originals/0e60d1ae009d6917-review.md)：私有材料、snapshot、origin/比较与 join 边界；保留当时 backend agreement 未完成的原文。 |
| 双方同 SHA 消费 | [前端确认](project-model-settings-spec-verification-evidence/originals/931da9f62b03d866-agreement.md)、[后端确认](project-model-settings-spec-verification-evidence/originals/22754edd6ce04d72-agreement.md)：静态可消费；三个 tuple 排布例子不替代完整恢复矩阵，也不证明已经实现。 |
| 完整整合候选 | [84,300 B 原候选](project-model-settings-spec-verification-evidence/originals/c5674d4e06618f26-candidate.md)、[冻结](project-model-settings-spec-verification-evidence/originals/dedc14db178430fc-freeze.json)、[完整独审](project-model-settings-spec-verification-evidence/originals/d411c2ce0dfe46b7-review.md)：保完整恢复矩阵和 29 路径，原稿的 pending 没有回填。 |
| 正式安装 | [作者安装冻结](project-model-settings-spec-verification-evidence/originals/99a649d45341895b-freeze.json)、[完整行政 patch](project-model-settings-spec-verification-evidence/originals/ea62d6016e882322-candidate-to-formal.patch)、[最终独审](project-model-settings-spec-verification-evidence/originals/e1abcf0ec30fa19e-review.md)：21 项替换正向等于安装文件、逆向恢复 c567；T1/T4 静态闭合。 |

最终安装独审核对 7 个契约 fence、§2/4/5/7 全文与 29 路径表原字节不变。规格仍为 26 条前端技术路径＋2 条新 Go＋README 第29项；17业务操作是6 GET、9 mutation、2 POST lookup，私有IPC为9项，新6与旧14精确top分列，两项条件Runtime不预排。45s case、120s top含cleanup、6m包、75s TCP、5GiB和7ID等原预算保留；这些是未来验收约束，本档没有执行它们。

T1 的 [14 输入指纹](project-model-settings-spec-verification-evidence/originals/f5d7a82484eb793a-t1-current-sources.json)与[必要签名摘录](project-model-settings-spec-verification-evidence/originals/9f55c8ac0db36105-t1-signature-excerpts.json)固定当时13技术＋README。最终独审实际逐行对应1,236行摘录，接受链为Audit当前表9源、Owner24表3源与rev1 account.ts 1源；没有用旧Owner表里已变更的其他行冒充当前源。共享Session默认依赖的第15位置、WriteOptions、普通Owner显式分派/actual finally、workspace只读currentReadContext、App九项确认与原路由接口保持独立边界。归档只复制这些停止的元数据，没有读取正在实施的业务源或重建完整import图。

[Audit完整22验收](project-owner-audit-ui-verification.md)与[其最终原独审](project-owner-audit-ui-verification-evidence/originals/442ad7809fee233c-review.md)原位复用。安装观察的 `75411e27` 和后继文档提交 `3d9eeeae` 均保原时点；最终规格锚为上述 `7b8af244`。原candidate、原review与pending文字全部原字节保存，由后继接受记录说明状态变化。

原错误均保留：[完整候选独审证据](project-model-settings-spec-verification-evidence/originals/3bd930dc22dfd1df-evidence.json)记载一次只读误用 `protocol.json` 文件名的 exit 2；[安装失败记录](project-model-settings-spec-verification-evidence/originals/557d389d39353778-failures.json)保存误用 `project.ts` 的 FileNotFoundError及工具包装 JavaScript 解析失败。更正路径/包装后的只读完成不抹除原失败，也不将其解释为产品缺陷。没有补造缺失的独立 raw 文件。

本次归档另有一次[生成顺序错误](project-model-settings-spec-verification-evidence/originals/571084d08b366273-installation-attempt01-failure.json)：工具 exit 1，链接检查在manifest生成前读取其路径。修正本生成器的检查顺序后完成；已复制的53个原件未变，原工具stderr与当时3个生成件SHA保留。此错误不属于产品或规格缺陷。

新增 54 个原字节实体，共 564,180 B，其中53个规格接受链原件和1个归档工具失败记录；6个已有永久实体共59,087 B原位复用，2份正式规格共97,523 B用固定Git引用。来源映射与完整SHA见 [source-map](project-model-settings-spec-verification-evidence/source-map.json)，生成件及原件清单见 [manifest](project-model-settings-spec-verification-evidence/manifest.json)，本次仅文档检查见 [checks](project-model-settings-spec-verification-evidence/checks.json)。没有复制业务源码树、依赖图、cache、binary或Audit证据全集。

原行政patch的36处尾空格逐行记录在 [format.json](project-model-settings-spec-verification-evidence/format.json)，原bytes不规范化。生成报告/元数据的UTF-8、LF、末尾换行、尾白、JSON和本地链接已自查；原始证据中的历史scratch链接保持原文，不冒称全部旧链接已迁移或重验。Git暂存空白检查由root另行执行。

实施授权和作者实际启动分开记录：root已将前端26路径与后端2新Go分别移交，本档不把followup当作ACK或执行结果。实现、完整工具/源码/schema/dist闭包、编译与发现、逐轮资源/资产许可、实际验证及不同构造独验仍须按正式卡取得自身证据；README末件待28技术接受后另授。生产consumer/serving/Invocation/root绑定、三停止、Jina以及完整D27/项目的未完成边界不因本SPEC接受解除。

归档已停止写入；未执行Go、Node、浏览器、网络、Git或资源操作。
