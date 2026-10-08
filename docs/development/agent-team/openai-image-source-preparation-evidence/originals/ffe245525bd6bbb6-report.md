# Image 固定原件本地定位

结论：**BLOCKED / STOP，未取得必需 Image 原件，不进入 strict-profile 草案。** 本轮仅沿 root 指定的设计 §6、旧研究 SHA `41f04a20…` 与已提交 Chat wire source 引用链定位；没有全盘搜索或全局缺失结论。

`source-manifest.json` 明确只收 17 份 Chat 采用原件，commit 为 `becc1d20eed83c1b8d85e15dc131a372d9dc7813`，不包含以下四件。README 明确存档只保选定摘录、来源记录、commit 对象与许可证，没有 SDK 对象库。记录引用的 `/workspace/agenteam-openai-chat-source-5t6d72b5/objects.git` 及其根目录、两份 `/tmp/agenteam-openai-wire-rev2-u62vqiwd/restored-sources{,-final}` 当前均不存在；未在其它未知目录继续猜搜。

必需但本轮未取得：

- `src/openai/types/image_generate_params.py`
- `src/openai/types/images_response.py`
- `src/openai/types/image.py`
- `src/openai/resources/images.py`

四件 SHA/blob 保持 null；必要传递类型须从取得的真实 imports/字段追溯，当前不猜文件名或枚举。原研究报告 `41f04a202111e3ab99ea0c904112accc76dfc6844975b3c518640c1e090a515f` 也未取得实际 bytes；已读 field manifest 只回指正式设计的旧 SHA/行号，并未提供该原报告或 Image 文件的本地入口。

已实际核得的有限证据：

| 原件 | 本轮只读结果 |
| --- | --- |
| 归档 commit-object.base64 | 标准库无损解码 1659 B，SHA256 `4a18fa5995a190f5cbff6f3313422abbae18861e401e2251ba73e7c8a59a9702`；按 Git commit 对象编码计算 SHA1，精确等于 becc1d20…。其 tree 为 `f1b9a07a4b1f2d908bcb08d9cac4449f93616933`，未取得 tree 对象或 Image blob。 |
| LICENSE.openai | 11336 B，SHA256 `636eb7d79da9bb6d515a4b3fd417aa26679eb3cf16396ddab4bc55fa74e616e4`、Git blob `cbb5bb26e40dd13a02c7874298f70b9d68bab57e`，与同 commit 的旧 license 记录匹配。 |
| 原 fetch/独审/离线重建记录 | 保留官方 URL、精确 commit 和当时成功事实；本轮只读其记录。未再次取回对象、验证签名或把旧成功写成当前 17 完整源重建成功。 |

这些证据绑定已归档 commit/许可证的字节身份，不能推导未取得的 Image 内容、当前远端真实性、账号/型号能力或 server conformance。设计 §6 的已采纳字段事实保留，未因本轮缺原件而改写；同时不能用其文字或 Embedding 六原件补造 Image parser/profile。

最近阻塞是上述固定版本原件及其必要传递闭包。本轮已按“找不到必需固定原件即 STOP”结束；后继只有取得可明确定位的本地固定源输入后才继续本地核对，或由 root 另行决定来源恢复任务。本轮没有网络/fetch/install、Git 命令、SDK 导入执行、Go/Node、业务资源或仓库改动；未写正式卡/产品/strict-profile，也未修改原 T3 草案。小检查与输入指纹见 `source-checks.json`，原存档原位复用，没有复制 SDK 树。
