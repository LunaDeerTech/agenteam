# 当前工作：D15 Runner 身份与 Control Channel

- 工作树：`/workspace/agenteam-runner-control`；分支 `ai/runner-control`，基线正式 `f1c94ee5`。root 负责所有 Git 写入、共享范围及真实资源调度。
- 唯一规格：[d15-runner-control.md](../docs/development/work-items/d15-runner-control.md)，rev1 冻结待独立SPEC审查；设计/实施者 `/root/service_delivery/blocker_spec_review`，D15 独立验收须另派未参与者。
- 当前只有本卡/current 写入；未实施产品、00026 或共享接口；不会等待尚未实现的 Agent/Skills/Object 才设计，也不会以 stub 冒生产集成。
- root 已授权卡§2共享域及 `runner-identity` 闭集身份。00026 预留；真实迁移必须等24/25完整连续来源，不填空迁移。
- 固定依赖 `github.com/gorilla/websocket v1.5.3` 已按根单次下载窗 actual exit0，缓存为本树 `output/ai/runner-control/go-mod`；下载未改go.mod/go.sum。网络窗已释放，无服务/PG/browser/TCP/后台进程。
- 作者自查：卡7个链接含fragment/current链接与whitespace均通过。下一步：根安排独立SPEC审查，卡/current已冻结；接受前不实施。Linux/macOS真实支持矩阵、D10/D16/D17/D18真实绑定gate仍未验。
