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
