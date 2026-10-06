# System Model UI 证据原件

[正式报告](../system-models-ui-verification.md)绑定接受提交 `bc17167c42ee5d5fc1427adaac099ff888ca959b`。

[archive-index.json](archive-index.json)映射794个逻辑原件至302个按SHA256去重的原字节对象，共4515988字节；source字段仅保存历史出处，离线核验不依赖scratch或活动产品树。原命令、输入、失败、源版本、截图和资源终局均保持原字节。29个dist只留指纹；依赖从固定Git `f465f45` 的1030路径和锁重建，不保存依赖、缓存或二进制。

在仓库根运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/system-models-ui-verification-evidence/verify_archive.py
```

[核验脚本](verify_archive.py)仅核原件字节、固定Git26交付路径和1030基线、三版输入/差量、原实际退出与双清记录，不执行归档命令或产品。早期pure未保存完整env、首轮snapshot无现场字段差异、作者六组分两版复用和历史PPID1 Z限制见正式报告；原报告中的pending/README deferred属于其当时状态，最终接受以正式报告及final-delivery02原件为准。
