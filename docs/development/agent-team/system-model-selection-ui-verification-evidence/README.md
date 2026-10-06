# System 平台模型用途 UI 证据原件

[正式报告](../system-model-selection-ui-verification.md)绑定接受提交 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`。

[archive-index.json](archive-index.json)映射1235个逻辑原件至445个按SHA256去重的原字节对象，共10441518字节。source字段只保存历史出处；离线核验不依赖scratch或活动产品树。25源及README第26路径绑定接受Git；1041依赖路径沿固定基线 `bc17167c`，31个dist只保留指纹。没有复制依赖、缓存或二进制。

在仓库根运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-model-selection-ui-verification-evidence/verify_archive.py
```

[核验脚本](verify_archive.py)只核原件字节、固定Git、版本差量、原实际退出/输入/双清记录，不执行归档命令或产品。作者新组5+1、独立B02+A03分别组合通过，原失败轮不改写为全绿。四份确认缺失的历史通过源码与十二份格式化暂态输入未定位均明确保留；失败测试源和最终受测源齐全，不声称每个历史版本均可重建。原pending/README未写等文字保留当时状态，最终结果以正式报告和readme-delivery01为准。
