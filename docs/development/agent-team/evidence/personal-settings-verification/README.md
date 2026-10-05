# 个人设置验收证据

结果与限制见[正式报告](../../personal-settings-verification.md)。本目录保存作者、独立验收和首 CDP 只读诊断的原字节；`original-map.json` 将原绝对路径映射到本目录，原报告/JSON 内路径不被改写。`SHA256SUMS` 固定本目录除其自身外全部文件。

## 固定输入

业务底座为 `9a710f272026b41ef69852bbeb41cb7670b500a8`，仅覆盖 `author/evidence/fixture-input-01/manifest.json` 的 24 路径，逐项来自已接受 `c54f73f3324caa11608d84e5d207141985eb6074`。不要把整个 c54 提交当作当时测试树。独立 real 再覆盖 `independent/evidence/ready-input.json` 的 3 probe；UI01/02 pure 使用其 `input.json` 中的 probe/config 和对应历史源码。

`source-locators.json` 绑定 Git 基础/已接受对象、`source-variants/` 的唯一历史字节与 probe；原绝对源位置只用于溯源。两项作者事后精确 SHA 重建明确保留 provenance，不能冒充同时备份。最终 9 个 dist 原件的 SHA 在本次核过，未收 bundle，恢复运行须按固定锁、工具和 build 记录重新生成并核 hash；本次没有重新 build。历史 UI01 dist 仅有输入清单 hash，未主张其 browser 覆盖。`source-limitations.json` 列仅 SHA 的早期中间版本和受影响命令，不补造原源或 exit。

在仓库根运行（只读 Git/字节，不启动业务）：

```sh
python3 docs/development/agent-team/evidence/personal-settings-verification/verify_sources.py --repo . --check
```

脚本核原件 hash、最终 24、所有有定位的源以及独立固定源码输入；可用 `--materialize /任务自有空目录` 仅展开上述基线和精确覆盖，不下载依赖、不运行测试、不复制原缓存或生成产物。实际运行命令、argv/env/exit 仍以每轮原 JSON/log 为准，路径需换为恢复者自有资源，不能重用历史容器 ID 或秘密。

## 主要原件

- `author/evidence/author-final/`：作者报告、110 pure 与五轮真实的分版本覆盖。旧认证首父组 FAIL 保留，后续仅原样 keyboard 通过，未称原父组全绿。
- `reviews/core01`、`core02`、`ui01`、`ui02`、`ui03`、`fixture`：独立固定阶段报告及索引。UI-F1 独立 pure 有原红/同 probe 修后绿；UI-F2 独立为静态发现。
- `independent/`：最终报告、3 probe 定位、TS 原红/修后、两份真实 plan、observer、原输入/资源、runtime 长度前置红及成功的 1 top/1 browser case。
- `cdp-diagnosis/`：旧 auth keyboard 首红定位；原因未知，不从后续 PASS 反推修复或旧响应码。
- `afterdocs/`：另组冻结的三验后文档 rev02 索引/检查与 CSS zoom 窄澄清，不是产品运行输入。

首轮作者 Node PID 采样缺口、CSS zoom 非原生、无生产 hosting/Vite/真实 Unknown 注入和完整 D26/Object 修复的边界全部保留。原 patch/log 的实际格式诊断只按 `whitespace-exceptions.json` 中精确路径+SHA 排除；其余本次文件检查格式。原合成 mock 材料保持字节，不含实际凭据或浏览器秘密。
