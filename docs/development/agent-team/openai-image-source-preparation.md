# OpenAI Image 固定来源准备：BLOCKED

2026-10-08，root 已接受以下有界取证事实。状态仅为 **SOURCE / BLOCKED / STOP**：四个必需 Image 原件仍未取得，字段、直接 import 和必要传递闭包均为 unknown/null，尚未形成 strict-profile 或实施卡。本归档没有新联网、重试或产品检查。

| 停止来源 | 实际结果 | 证据界限 |
| --- | --- | --- |
| [本地固定引用链](openai-image-source-preparation-evidence/originals/ffe245525bd6bbb6-report.md) | 已归档 Chat source 只收17个采用原件，不含所需四件；原记录中的对象库及恢复目录在当时限定检查中不存在。 | 只说明指定引用链，不是全盘文件缺失结论；未取得旧研究报告41f04a20的原bytes。 |
| [旧直连请求](openai-image-source-preparation-evidence/originals/e08e32cbc4f4100e-report.md) | 2026-10-08 17:31:33 UTC 首个tree请求DNS失败，0.009836785s，HTTP status=null，工具exit1；四个raw请求均未发起。 | helper使用直接 `HTTPSConnection`，忽略已有环境代理；没有HTTP响应。0B tree.body是占位，不是服务端空响应。 |
| [现有标准代理请求](openai-image-source-preparation-evidence/originals/ae9db5d55f972f6a-report.md) | 17:43:12 UTC 首个tree请求在代理CONNECT收到403，curl实际wait/exit56，0.014061549s；外层工具exit1。四个raw请求均未发起。 | 隧道未建立，GitHub应用层status=null；这不是GitHub API或SDK官方返回403的证据。 |

旧直连与后继标准代理分别保留。前者没有消费环境代理；后者沿现有环境的curl标准代理行为运行，未读取/输出代理值或凭据，也未更改代理、DNS、trust或no_proxy。没有重试、重定向或补试其它通道；两次各1个tree尝试不能推断所有GitHub通道不可用，也不能从Git push可用推导该读取通道获准。

固定版本为 `openai/openai-python` commit `becc1d20eed83c1b8d85e15dc131a372d9dc7813`，旧commit记录指向tree `f1b9a07a4b1f2d908bcb08d9cac4449f93616933`。两轮计划的首请求均为 `https://api.github.com/repos/openai/openai-python/git/trees/f1b9a07a4b1f2d908bcb08d9cac4449f93616933?recursive=1`。下列四条固定commit raw URL只列在 [source-map](openai-image-source-preparation-evidence/source-map.json) 作为未启动计划，不表示取得源内容：

- `src/openai/types/image_generate_params.py`
- `src/openai/types/images_response.py`
- `src/openai/types/image.py`
- `src/openai/resources/images.py`

四件的SHA/blob、fields、direct_imports和传递闭包保持null，不写成空集合。没有从Chat、Embedding摘录或设计文字补造Image parser/profile，也没有声明型号/账号能力、provider conformance或server行为。原正式设计已采纳的历史事实不被本轮缺件改写，新的实施契约仍缺固定原件。

[代理原header](openai-image-source-preparation-evidence/originals/d601a9fcf1ee2f33-tree.headers.raw)声明 `Content-Length: 16`，[原stderr](openai-image-source-preparation-evidence/originals/115ac061d5c11b9f-tree.stderr.raw)为47B固定CONNECT失败诊断；curl保存的stdout为0B。curl没有输出该CONNECT错误body，不能把0B捕获称作完整代理响应体、GitHub空响应或SDK源。[直连工具终局](openai-image-source-preparation-evidence/originals/091ea1f3197e1446-tool-terminal.json)只保helper捕获后输出的异常JSON，没有可补造的原stderr/traceback。代理[请求receipt](openai-image-source-preparation-evidence/originals/6060bb370d189f59-tree.result.json)和[工具终局](openai-image-source-preparation-evidence/originals/5a207bad5f2c5aed-tool-terminal.json)记录直接curl PID/starttime、实际wait与随后两次身份不存在；没有强杀，也不冒主机或完整进程树清空。

旧[Chat来源manifest](evidence/d09-openai-chat-wire-source/source-manifest.json)、[commit对象](evidence/d09-openai-chat-wire-source/commit-object.base64)和[LICENSE.openai](evidence/d09-openai-chat-wire-source/LICENSE.openai)原位复用。原记录重算的commit对象为1,659B/SHA256 `4a18fa5995a190f5cbff6f3313422abbae18861e401e2251ba73e7c8a59a9702`；许可证为11,336B/SHA256 `636eb7d79da9bb6d515a4b3fd417aa26679eb3cf16396ddab4bc55fa74e616e4`、Git blob `cbb5bb26e40dd13a02c7874298f70b9d68bab57e`。这只绑定已保存的旧commit/许可bytes；没有新下载、数字签名验证、tree成员证明或四Image blob。没有重复制旧Chat17源或SDK树。原前沿候选59752cac仅列固定scratch来源身份，不复制全后端建议，也不把候选当Image实施许可。

新增26个逻辑原件以SHA去重为25个实体，共55,446B；两轮0B tree.body映射同一空实体，语义在source-map分别登记。12个旧Chat永久来源及1个设计来源原位引用，原件内容不重扫。必要记录包含请求helper原文、请求计划/argv、实际失败、原header/stderr、spawn/receipt/终局和报告/冻结；helper仅归档，未执行。完整来源、bytes/SHA见 [source-map](openai-image-source-preparation-evidence/source-map.json) 与 [manifest](openai-image-source-preparation-evidence/manifest.json)，本次检查范围见 [checks](openai-image-source-preparation-evidence/checks.json)。

原header的7条CRLF（原第7行为空行）全部保留：[format.json](openai-image-source-preparation-evidence/format.json) 登记7条CR尾白、1条EOF空行及7条CRLF字节指标。未normalize原raw，也未运行Git；root另做实际暂存空白检查。生成报告/JSON的UTF-8、LF、末尾换行、尾白与本地链接已自查，历史证据内原路径保持原文。

后继只有取得可验证的四个固定commit原件、对应blob/tree关系及真实imports的必要闭包后，才能继续字段核对；任何新来源尝试需另行明确移交。本档不解除原失败STOP、Jina或三停止，不授权SDK安装/执行、真实模型服务、Go/Node/业务资源或生产绑定。归档完成并停止写入。
