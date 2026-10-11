# Outbox 原正常 PG 验收输入

当前安全修后实现已由 main `60e02927cab53b53f19a42ca75624f77c1ee61ea` 正式交付。正常 PG native01 的既有通过输入是 `0f49aced1085e38080874ea9f6efb4fea8194112`，其后 opaque panic 边界以 unit04/vet02 单独验证并复用正常 PG；不能称安全修后源码重跑过 native01。

`retired-inputs.patch` 仅用于历史 PG 分段输入恢复，不是当前产品补丁，也不可应用到生产或最新工作树。由协调者准备上述 main 历史的隔离 checkout，然后在其根目录执行：

```sh
patch --batch --forward --fuzz=0 -p1 < /path/to/retired-inputs.patch
```

补丁只恢复以下四份原文件（其中 product/test 恢复的是 panic 修补前状态，仅为准确重现旧 PG 输入）：

- `.agent-state/task-planning-recovery/pg_only_supervisor.py`
- `.agent-state/work-owner-http/root_chain_driver.py`
- `internal/central/project/outbox_stop_participant.go`
- `internal/central/project/outbox_stop_participant_test.go`

四份恢复结果已在私有临时目录实际应用（exit0、fuzz0、无offset），逐字等保全原件，并逐项匹配原 `output/ai/outbox-stop-adapter/native01/input-hashes.json`；其余1660个受控tracked输入可直接从上述main历史恢复。临时目录已退，没有重跑产品或native。

完整原FAIL、normal PG、后继panic反例及修后unit04/vet02、binary和所有输入清单在main的 `output/ai/outbox-stop-adapter/`；本机材料位置见其中PRESERVATION.md。热cache/ROmodule/MinIO依赖仍由既有保留位置持有，不在补丁中。原绝对路径和分段事实不改写，历史通过不保证新环境。
