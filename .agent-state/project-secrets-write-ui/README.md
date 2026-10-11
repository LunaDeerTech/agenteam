# Project Secret Write UI 早期失败输入恢复

用途仅为解释和恢复本片原失败；不是新的通过记录，也不批准自动重跑或恢复 STOP。正式边界见[任务卡](../../docs/development/work-items/d10-project-secrets-write-ui.md)。

## 可精确恢复的产品源码

`early-product-inputs.patch` 以组合修后 `e1e09454f49e99c6b823352178685865c7b32e60` 为应用基线，仅把以下四个文件恢复到首轮反例时的 `8c08c570b354657c9ed539203a9d817882e7fe0d` 原字节：

- `web/src/composables/useSession.ts`
- `web/src/composables/useProjectSecretEditor.ts`
- `web/src/composables/useProjectSecrets.ts`
- `web/src/components/projects/ProjectSecretEditor.vue`

在私有恢复 checkout 中应用：

```sh
patch --batch --fuzz=0 -p1 < .agent-state/project-secrets-write-ui/early-product-inputs.patch
```

已在私有临时目录实际应用，退出 0，四文件逐字等于旧 Git 对象。该历史 Session 会同时撤下后来合入的 Knowledge 删除增量，以恢复首轮实际文件；不要将恢复结果覆盖当前开发或生产输入。基线后若这四文件变动，应先核限定差额，不使用 fuzz 强套。

只研究 checking 的发布顺序时，可仅恢复上述 `useProjectSecrets.ts`：旧文件在这处先发布 reactive scope、后写 route。其单文件恢复也已实际应用并逐字比对；搭配当前其余源码只构成定向行为复现，不能称 checking01–03 全部旧 WIP 原字节。

## 原件与无法补造的范围

原命令、日志、PID/实际 Wait 保留于 `output/ai/project-secrets-write-ui/checks/`：`review-repro01` 为 3 FAIL/1 PASS；`unit01` 为 42 PASS/1 FAIL；`checking01`–`checking03` 为恢复失败的后续观测。原 WIP 测试与部分中间产品编辑没有逐字快照或逐项 SHA；最终测试源经过格式化和诊断收敛，不能反推后宣称还原那些运行的全部输入。不要改写原 FAIL。

`build02` 还保留一个页面测试 helper 类型错误。以本文件基线的 `web/src/tests/project-secret-write-page.spec.ts` 为准，将唯一 `function button(w: Pick<VueWrapper, 'findAll'>, label: string)` 改回 `function button(w: VueWrapper, label: string)`，已在内存验证所得整个文件逐字等于 `0661f376`；这不改变产品或断言。修后 `build03` 与 `combined01` 的原结果另存，不覆盖该失败。

其余最终方法控制由本目录受跟踪的 `method-controls.mjs` 恢复；方法变更及原执行范围仍以各自 invocation/result 为准，不把后增 paste 联证或组合控制倒算成早期 68 项已覆盖。依赖、构建资产及原运行输出保持 ignored，仅保留原件；不把材料或请求摘要写入恢复文件。

## 首次浏览器定位失败

`native01-inputs.patch` 仅将 `tests/account-captcha-web/e2e/project-secret-write.spec.ts` 从 `c964e62d39cf1863395b914776feef706dd0a971` 恢复到 `e764c08155af31627159c09f2d3450d77363d9cf` 的原定位写法。已在私有目录实际 `patch --batch --fuzz=0 -p1`，退出 0，整个文件逐字等于旧 Git 对象，SHA256 `4fb15a414346505a0990778098cfd97254bacb941195ddef59937191cdb4b3dc` 与原 native01 输入记录一致。其余产品、Go、observer 和原 dist 不变；无需恢复整树。

原 native01 是完整退出的 wholeFAIL：normal 停在空目录可见性断言；lost 已过 seed 详情，停于 required 材料字段的 exact label 焦点定位。没有发生写入/Lookup，没有失败截图或 raw locator count，不补造。原件在 `output/ai/project-secrets-write-ui/native01/`，后续定向 DOM 反例和修后结果在 checks；保留原结果，不用后续成功覆盖。
