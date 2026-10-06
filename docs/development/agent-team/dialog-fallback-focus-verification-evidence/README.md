# Dialog 显式后备焦点：最小证据

对应[正式报告](../dialog-fallback-focus-verification.md)，固定产品提交 `fd32120eba4c76f67b649248377f4825a78d5d79`、组件基线 `870ebbb986f56bb62be34ccdfa2c77819ce995a6`、规格提交 `3c79fd4069837c4cee0b1ed37e00480ea8f1909f`。

[index.json](index.json) 保存 219 个逻辑原件名、原来源、SHA／字节数与逐轮输入映射；相同字节去重为 `objects/<sha256>` 的 139 份原件，共 1,660,403 字节。原 raw、diff、失败附件保持原字节，不作格式清理。五交付源、127 项隔离 web 指纹和组件闭包的未改动部分通过 140 个固定 Git 引用核对，不复制完整源树。

- `author/`：作者报告、input01、五源、逐轮有限候选版本、10 轮实际命令／日志／退出／输入／清理。old01 discovery、old02 真实焦点 RED 及截图／trace、pure01 两处布局前提原红和格式等价记录均保留。
- `independent/`：计划、固定输入、最终报告、纯测两版 probe 与最小差量、旧／新私有 tsconfig、真实壳／probe／runner、6 轮实际记录。8 项纯测为原七项＋受影响一项组合，type01 保留 exit2，browser01 三项通过。
- `run_input_bindings`：每份原前后输入映射至已归档字节、固定 Git，或明确标为只保存指纹的工具／依赖。早期 type01 原 tsconfig 使用其实际保留副本，不能拿后来的配置替代。

在仓库根运行：

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 \
  python3 docs/development/agent-team/dialog-fallback-focus-verification-evidence/verify_archive.py
```

脚本只核归档原字节、固定 Git、五条交付路径、16 轮原退出／wait／清理和卡技术正文，不启动产品、浏览器、网络或资源，也不读活动 Account 产品。原 runtime 的 7094 文件／28 symlink 清单只保留来源、SHA 和原检查结果，不保存全量清单或依赖实体；Node／Chromium／包工具指纹同样不表示离线重建了安装树。

old01 原 `clean=false` 因未进入 server lifecycle 而保留，其所属进程两扫空、端口不变与 TMP 消失分列。历史 PPID1 Z 不纳入本轮清理；作者 old02 与新 probe 只证明项目格式规范化后等价，不是原运行文件逐字节相同。独立纯测使用 jsdom 布局桩，真实几何结论来自固定 Chromium 三项及作者 45 项。

Account new01／new02 和原独立归因／静审在索引中仅作原来源／SHA 交叉引用，未重复复制，也不属于本共享结果的业务接受证据；其前因已由[工作卡 §1](../../work-items/d27-dialog-fallback-focus.md#1-责任边界与固定证据)记录。Account rev2 的真实 Session／Navigation 组合仍待该卡后续接受。
