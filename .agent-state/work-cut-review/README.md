# Work 四条预声明截断方法独立复核

2026-10-09，Skills。只接受 Work 固定 `152eb964` 相对 `ae101b00` 的四条预声明截断观察方法；未修改 Work 产品或 helper。普通 native 诊断当时仍 WIP，不在本结论内。原 recovery05 `63642` 整体 FAIL 不变，不是新真实 UI 或整包验收。

依据 D11 Owner planning UI 卡 §8.2 与先前独立方法核定：固定 Playwright 1.56.1 的 `_onRequestFailed` 不结束 `Response._finishedPromise`，四条故意截断流本来不可能取得完整 EOF。因此仅 `unforwarded-milestone-update`、`lost-milestone-update`、`lost-task-update`、`lost-blocker-add` 不创建无必达的 finished 操作，以同一原 Request 在 page-close 前的真实失败事件结束本地观察。该事件必须联合唯一产品 owner 实际尾后可用的公开 Lookup、Go 原 Write/Flush/Hijack/Close、零转发或真实 completed SQL、精确原历史/唯一事实与资源收尾，不能独自宣告业务通过。普通五条 aborted 的原证据不足不变；held-read 原方法不扩大。

静核固定 helper diff：原预声明、精确材料与 Request identity、cap4、503 固定 headers 保留；新增成功事件反证，failed 后成功也累计错误；page-close 结束本地事件等待但拒绝验收，晚 failed 不能补为成功。普通响应仍唯一原 finished，held-read 仍原 finished/取消路径及晚拒绝门槛。originalBody/schema/decoder、Go 注入/SQL/Lookup/实际 Wait/资源预算未改。

离线 `53260` **actual exit 0，63 控制，0 unhandled**。`controls.cjs` 从固定 Git 版本读取实际 helper 和作者已有 57 控制，追加本实例六个控制：四种截断各在失败尾已完成后注入成功事件，仍必须拒绝、finished 调用为 0；未声明普通失败即使 finished 返回 null 仍拒绝且只调用 1 次；held-read 实际授权取消后，原 finished 晚拒绝仍失败且只调用 1 次。实际 PW 类方法控制同时验证固定 1.56.1 的 failure/finished 语义。所有声明、transport 事件、内存 evidence 和 schema 外部调用是控制输入；不证明真实网络/客户端发布/SQL，不启动 PG、浏览器或 socket。

在 `/workspace/agenteam-skills` 执行：

```sh
node .agent-state/work-cut-review/controls.cjs
```

探针对 Work 树只读，固定源码仅在内存加载，结果写入 Skills 忽略目录 `output/ai/skills/work-cut-review/independent-controls.json`。原作者 `69562` 57 控制、`29474` strict TS 的范围仍保留；此结论不使普通 native WIP 获得接受。待作者冻结 native 诊断接线后，另核其仅观察、不改变普通 finished/消费/发布门槛及实际观察尾。
