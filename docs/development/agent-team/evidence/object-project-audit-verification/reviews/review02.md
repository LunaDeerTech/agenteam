# Object Audit review02 delta 独立静审

结论：本次 delta 静态通过，review01 的 GET 历史完成兼容阻断已在代码层消除；未发现新增静态阻断。尚未动态复验，不等于整个 Object Audit 已验收。

固定输入为 `/tmp/agenteam-object-audit-author-t97qa_1s/production-review-02/input.json`，SHA256 `94647366e02f3bd544ed5a50ecc681061bf85a73b5d61a03c7ef6291124e0aeb`，base `6658a6cb1f29299521773bc8dc86b2f607b8c809`。8 源指纹均匹配，与 review01 只有 `internal/central/object/project_audit.go` 改变（新 SHA256 `ad3db6970c5806c199976b08b137f5ab5d143a705e90afac67c0a1f7973839ed`）；提供的 delta.patch 与两份固定源重新生成的 diff 逐字相同，SHA256 `11726d0c15b7490c9fd5506c2349328d3fb4f245c35bdb432b31ea55bc30b1be`。其余七源结论复用，不重做整组静审。

- GET complete 现在于 `project_audit.go:252` 允许检查原 active 或合法 released lease。`:314–321` 仍从原 Tx 读取 exact lease ID 的 object/owner_kind/owner_id/attempt/state；released 必须真实 `released_at IS NOT NULL`，且同一持久 transfer 有有效 retirement ID、32 字节 digest 与 Completed/Stopped kind。没有把单个 Completed proof、过期或 revoked 当作 retirement；这些持久 retirement 字段仍只能由原正式 authority/lease 路径形成，本 delta 未新增 mint 或绕过路径。
- GET complete 在 `:332–339` 仍要求对象 metadata 实际存在、owner partition 与 manifest 的媒体类型/长度/SHA256 精确匹配；仅不再用 Available/cleaning/deleted 判定历史 I/O 是否完成，因此覆盖已退休仍 available、active lease 下 cleaning、retired 后逻辑 deleted 三个原阻断状态。全 Project purge 后不存在 transfer/对象仍不能制造历史完成。
- 原 `:240–247` 的当前完整 stage、canonical complete 且非 revoked、真实 Completed evidence ID/kind/digest、manifest length/SHA256、GET Capture/PUT Publish 与 candidate 对应保持不变；前段全 Entry/Key、Store/Tx、generation/version/execution 检查及 `TransferService.within` 的当前 Runner/Owner/Project 授权完全未改。修复没有让历史 evidence 替代当前授权。
- Issue 仍调用 active=true，并在 GET `completed=false` 时要求 Available 且非 cleaning。PUT issue/complete 仍要求 active lease；`:341` 原 cleaning/Deleted 拒绝移到 GET 分支之后，PUT 的 upload/staging/private candidate/committed/Available 条件未放宽。其他 action、Audit 次序、事务边界和 cancel 路径没有 delta。

后续动态边界：主线程告知作者已保留三个真实 red，但本轮不读取活动测试或结果、不冒称独立确认这些运行。最终冻结后核其 red→fix 证据，独立关注合法 GET 三态至少一个代表、released 缺/错 retirement 与当前撤权否定；其余双 Audit/批撤销/真实 Unknown 等继续按原风险计划选择，不重复无关旧套件。

本轮只用固定文件读取、SHA256 与 Python diff/复制，所有写入仅本自有 tmp。没有运行 Go、Docker、数据库或后台进程，没有仓库/Git 写入，不拥有 fixture 窗口。交付末 input/8 源再核匹配；报告冻结后 all-stop。
