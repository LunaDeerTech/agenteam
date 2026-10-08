# Project Owner 工作区 UI 规格验收归档

**rev1 全文、rev2 B1 修订及正式归位末件完整限定 STATIC PASS，root 已采纳。** [正式卡](../work-items/d27-project-owner-workspace-ui.md)提交 `8cf81b44a93fce53ac3f5a218bfc9569e193eeb7` 已推送且 root 核远端一致；2026-10-08 恢复后重核同一提交。固定产品基线是配置写入 `cc850b22`，归档 `b91cb89f`。这里接受的是规格，未接受 UI 产品、浏览器行为或生产 SPA 发布。

| 阶段 | 原结论与修订 |
| --- | --- |
| [rev1 完整独审](project-owner-workspace-ui-spec-verification-evidence/originals/independent/project-owner-workspace-ui-spec-review01/review.md) | NEEDS_REVISION 唯一 B1：整段禁用 `forgot-password` / `reset-password` 前缀，会拒绝 Account 规则允许的这两个真实用户名下的 Project 路径。其余完整规格 STATIC 通过。 |
| [rev2 差量与完整组合](project-owner-workspace-ui-spec-verification-evidence/originals/independent/project-owner-workspace-ui-spec-review02/review.md) | 只修 §3.1 及修订标题：单段公开密码路由仍优先且隔离 token；两个合法用户名的六个严格 Project/settings 路径进入 Resolve→稳定 ID Get。query、token、编码路径和多余后缀仍拒绝；未改 Account 规则、后端或 23 路径白名单。B1 关闭。 |
| [正式归位末件](project-owner-workspace-ui-spec-verification-evidence/originals/independent/project-owner-workspace-ui-spec-formal01/review.md) | 逆向行政/链接/来源附录变化后逐字节等于已验 rev2；65 来源、23 候选、8 既有指纹与 77 相对链接通过，技术无漂移。 |

范围为 22 技术路径加 #22 README（`useSession.ts` 为追加 #23），15 新/8 旧；16 个旧 selector 只是静态验收声明。共享唯一 Cookie owner、独立 Project intent、Unknown 与当前/历史投影、完整页和取消实际尾部等均须后续实施验证。作者静态列表包络 5,067,008 B 低于 5 MiB（5,242,880 B），三类投影上限 64 KiB；这不是实际运行 JSON、页面布局或浏览器极值观测。普通 Vite/同源 harness 也不等于 Central 托管、直达 fallback 或发布接受。

[来源映射](project-owner-workspace-ui-spec-verification-evidence/source-map.json)保存 32 份小原件及重复别名。65 项来源沿原输入与独审指纹、固定基线定位，不复制源码树；本轮只核原索引一致，未重核活动产品 65 文件。正式卡全文复用上述提交，保存两份原 patch，已逆向逐字节恢复 rev2 `c71bfc5a…72974` 与 rev1 `182dafda…35ba18`。原 [inputs](project-owner-workspace-ui-spec-verification-evidence/originals/author/draft02/inputs.json)保留五项路径/技能查找修正，包括不存在的 router meta、构建脚本、技能路径与测试文件，以及 SPA 停止探针原目录缺失；未把这些准备记录改写成产品失败或动态证据。

归档核原件字节/hash、JSON、脚本 AST、可逆 patch、新增链接及卡 §1 起全部原字节；未运行原 helper、Go、Node、Git 或资源。原件两份 patch 共 20 条格式记录（19 条尾空白、1 条原始 EOF 空白行），精确行号见 source-map；保原字节、不 ignore，此扫描不冒 root 的实际 Git 检查。当前实施分工及恢复边界见[2026-10-08 恢复记录](recovery-2026-10-08-environment.md)，与上述规格证据分开；全局未绑定和三停止保持。
