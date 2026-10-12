# Knowledge 子树删除 UI 恢复范围

正式功能、各轮失败与有限验收边界见[任务卡](../../docs/development/work-items/d12-knowledge-subtree-delete-ui.md)。本说明仅保必要历史输入，不替代主线交付状态或原结果。

## 最小历史源码

命名技术基准为 `2dcd2ef9`，也是 native03 实际源码；已交 main `48fe9a37` 的下列两文件逐字保持该版本。补丁只在隔离恢复目录应用，禁止覆盖当前产品或主线：

- `retired-inputs.patch`：两文件恢复至 native01 的 `6e0f90b1`。
- `retired-inputs-d35143a3.patch`：同两文件恢复至 native02 的 `d35143a3`。

两文件为 `internal/central/app/knowledge_delete_web_test.go` 和 `tests/account-captcha-web/e2e/knowledge-delete.spec.ts`；分别保留原未限定关闭按钮、footer修复后但heading未修、以及Go尚无安全stage诊断的真实版本。每份补丁均从同一命名基准独立应用；实际 `patch --batch --fuzz=0 -p1` 返回0，无fuzz/offset，逐文件字节等对应Git版本及原native input-hashes中的SHA。原验证已迁至 main `output/ai/retired-20261011/knowledge-subtree-delete-ui/runtime/recovery-check01/`。这是两份本任务独有方法源的恢复，不冒称还原整个早期checkout、Directory合入前全部输入或未记录的WIP。

早期单元/页面输入有的只有命令、SHA和原日志，没有保存每次在制源码全文；该限制保持，不补造。`remaining-dom01-page.source.ts` 是由补focus前的唯一差额还原、且已与原失败记录SHA相等的页面控制输入；探索性opaque token客户端解析失败已被正式裁定不属产品要求。native01原raw定位错误/count、native02 Lookup502内部stage从未记录，不能从后续成功倒推。两原binary与各轮原映射、实际Wait和FAIL原件均继续保留。

## 已保全的原材料

Secret Web owner确认dependencies01同锁物理私有、17个包内链接均无外链；browser owner确认原tool11109物理复制并已由自身CLI使用。双方无Knowledge旧树运行依赖，本任务全部原进程已实际退出。现已从 `/workspace/agenteam-knowledge-subtree-delete-ui` 完成以下保全；旧树/分支尚未删除。三个目录使用同文件系统 `renameat2(RENAME_NOREPLACE)`，原inode、权限、uid/gid与mtime保持；小/tmp目录复制后逐文件字节、权限与mtime相等，原/tmp仍在。实际记录位于目标根 `retention.json`：

| 原路径 | 已保全内容 / main新路径 |
| --- | --- |
| `output/ai/knowledge-delete-ui/` | 整目录：两compile binary、方法控制、原01/02 FAIL、03结果、请求联证、7图、runtime映射、wrapper与恢复核验；`output/ai/retired-20261011/knowledge-subtree-delete-ui/runtime/`。 |
| `output/ai/knowledge-subtree-delete-ui/` | 整目录：Web分段基础控、全部旧FAIL、实际build/DOM控、原失败输入及dist；同目标下 `web-checks/`。 |
| `web/dist/` | 当前实际生产资产原目录，同目标下 `original-web-dist/`；原runtime已另存84资产，不覆盖任何原dist。 |
| `/tmp/kdw01/` | 复制小目录、保原：`ui-0af01d9d6bed4b2e.log`及owned/request；`original-tmp/kdw01/`。 |
| `/tmp/kdw02/` | 同上：`ui-f6fc52654d934332.log`及owned/request；`original-tmp/kdw02/`。 |
| `/tmp/kdw03/` | 同上：`ui-1eac6d76eacf4fc9.log`及owned/request；`original-tmp/kdw03/`。 |

可再生node_modules、私有Go临时目录及缓存不额外复制；不能触保护的SkillHTTP热Go cache或SkillsResume只读modules/MinIO。原JSON中的历史绝对路径保留，通过本表与retention.json映射，不改写原证据。正式临时材料中的会话/令牌按原方法已经退休，本说明和补丁不保存私密材料。


## 历史编译二进制归档补充（archive03）

2026-10-12，本片下列裸二进制已无损收入共同 [archive03](../../output/ai/retired-compile-binaries/archive03/README.md)，随后经独立授权从原 retained 路径精确移除。完整字节现保存在 [history-binaries.tar.gz](../../output/ai/retired-compile-binaries/archive03/history-binaries.tar.gz)：298,117,374 B，SHA-256 `e72c0e3c62242e4ef0fa552bdfdfb634b97290d6c58de90026c7997a77b9922a`。表中成员名相对原 `/workspace/agenteam/output/ai`，并保留原 compile 路径关系。

| 归档成员 | 字节 | SHA-256 |
| --- | ---: | --- |
| `retired-20261011/knowledge-subtree-delete-ui/runtime/compile-app01/knowledge-delete-app-race.test` | 77,645,511 | `df47e82db6caf768cc867c20f0bcf7119fa4b49e4d95f4e4082f13ec8bb4d475` |
| `retired-20261011/knowledge-subtree-delete-ui/runtime/compile-app02/knowledge-delete-app-race.test` | 77,648,417 | `2f98e5d29ebbdfb11f2db3cba84113ee69a1282b90b40c27bd5500ae5fb592a6` |

原 phase1 工具 28190 实际 0（`662de4`），outer 988070 / child 988073 实际 Wait 0 / 50.927s；当轮完整解流验证九成员并保留全部裸件。后续 phase2 工具 59243 实际 0（`e2e194`），outer 990023 / child 990026 实际 Wait 0 / 8.168s：先重新验证整包、九成员内容与元数据及原件身份，再只 unlink 这九个获授权 ELF，目录和其它文件删除数均为 0。实际清单见 [removal03](../../output/ai/retired-compile-binaries/removal03/README.md)、[result.json](../../output/ai/retired-compile-binaries/removal03/result.json) 和 [deleted.jsonl](../../output/ai/retired-compile-binaries/removal03/deleted.jsonl)。原 139 份关联记录、FAIL、输入 maps 和归档 full stat 均未改变；当前 Command 2,007 输入与四身份保持。整批相对裸件基线净减少 394,043,392 B 已分配空间（小证明文件另计），不据此宣称 ENOSPC 已解决或新运行必定成功。

恢复时先核整个归档 SHA 和 [manifest](../../output/ai/retired-compile-binaries/archive03/manifest.json)、[before.json](../../output/ai/retired-compile-binaries/archive03/before.json)、[specs.json](../../output/ai/retired-compile-binaries/archive03/specs.json) 中的精确成员关系，再仅向新的隔离目录排他提取所需成员。例如使用 `tar --extract --gzip --file <归档绝对路径> --directory <新隔离目录> --keep-old-files --same-permissions --same-owner --numeric-owner -- <上表成员名>`；不得覆盖现有文件或写入当前 source/cache。提取后逐件核 SHA/size、mode、uid/gid 和纳秒 mtime；权限不足以恢复元数据或任一核验不符即停止，不能记为恢复完成。完整边界见共同归档恢复说明。

前文的文件数量、裸件保留和迁移 inode 相同是原保全时的历史事实。重新提取会分配新 inode 并产生新的 ctime，dev 取决于恢复目标文件系统，旧身份只保留在原 manifest/PAX 记录中，不能称新文件重建了原 inode；原日志、结果和 maps 不改写。恢复不是重跑授权或新的 PASS，热 cache、只读 modules、工具链与当前二进制不在本次删除范围。

两件原 binary 的 SHA 依据仍是对应 native01/native03 输入图；原 compile result 没有 binary size 或完整文件元数据，本次批准身份是归档前的实物观测，不回填为旧编译证据。早期未提交输入、native01 原 raw 定位错误/count 和 native02 Lookup502 内部 stage 等原缺口保持。
