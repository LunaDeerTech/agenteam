# OpenAI Embeddings float wire：规格接受与来源记录

主线程已采纳[完整独立STATIC PASS原报告](openai-embeddings-wire-spec-verification-evidence/objects/208d90a678e22364debe5f59b86abd9b21a0449566815cf1d28203c828456f51)，将[正式卡](../work-items/d09-openai-embeddings-wire.md)提交推送为`15d7e08fd72eb2c05f7ef64c14cc0de4f5a87597`并核远端一致。本结果仅接受规格，产品、构建、真实资源及供应商一致性尚未验收。技术§1–7 SHA256 `1320107dd2bebba97188c976780a111d0ec12e2fb11ae866c9ad3473b5e53133`保持原字节。

## 1. 固定输入与规格历史

[受审private rev0.1](openai-embeddings-wire-spec-verification-evidence/objects/91767b4685c37209648fca103672db33de537031ca30c51c1e40cc4de28d36f0)全文SHA `91767b4685c37209648fca103672db33de537031ca30c51c1e40cc4de28d36f0`；固定产品`c1427fa4fb7fa118b12b2fb1f5d5f8917113ce2c`沿已接受C0、D04及text/structured wire。[作者输入](openai-embeddings-wire-spec-verification-evidence/objects/757e16523c31a8b01bfdf59497209940b479a9bd99fa4ce65ae6cd01c28d29f1)恰31项Git来源，[独审核对](openai-embeddings-wire-spec-verification-evidence/objects/3b8f8eebe93f98f2b45fbac724e707f5cea3c334bc528f1c6aa9865e81fcf4ef)复核原31项，[额外输入记录](openai-embeddings-wire-spec-verification-evidence/objects/f41fab77b8505aca47c8feec86a8f33acd0b703deaeee76b919de68a43f85ed8)仅追加同Git的`internal/central/outbound/client.go`；不把32项回填成作者原31项，也不冒运行闭包。

[先前有限候选梳理](openai-embeddings-wire-spec-verification-evidence/objects/ca5d00236890c7ddc8e894d0ab378e1de58f18dd48e39af645e4740b2f0ea349)仅保留当时的候选与未知边界。[正式化差量](openai-embeddings-wire-spec-verification-evidence/objects/531b05cb8811dc5808383f09a002f7d93345984b886b7068f173a6ffdc6ef2c4)只改页首；正式全文SHA `9154874ac03b0e6b4563b1eb994f5dcde0b11a66ce305953c6cf550d28585b15`由Git`15d7e08`复原。旧“待审/尚未开工”原件不回改。候选16路径为新12、既有生产/fixture3及最后后端说明1；现有依赖与锁不扩大。

## 2. 官方材料及失败边界

[官方来源说明](openai-embeddings-wire-spec-verification-evidence/objects/3c583fe3a4c4782fe217ae45966e7427e1f139900524ac01f4e621b9e3906626)与六份原件固定在`openai/openai-python@becc1d20eed83c1b8d85e15dc131a372d9dc7813`，包括五份Python源码和LICENSE。[首次取证记录](openai-embeddings-wire-spec-verification-evidence/objects/3fa82e87428df04665cf2f5283e543e335ac8b9f70ba2b4f643a814407082e08)及[追加记录](openai-embeddings-wire-spec-verification-evidence/objects/e6ae43b19a689093ed72d422043643b74616e1c92c83c3fda313bfab8bcd99a4)保留实际URL、revision、HTTP状态、SHA与字节。官方guide和[cookbook revision查询](openai-embeddings-wire-spec-verification-evidence/objects/6d8c87a01c353f125e6c0e263182fd4c3712d37778bd82b08af52bc464c13c10)各一次代理CONNECT403；没有取得guide页面、cookbook revision或Notebook正文，本次未重试。

SDK仅作字段来源，不代表SDK执行、所有兼容Provider conformance、真实账号可用性、默认维度或容量保证。首版显式`encoding_format=float`、文本数组及参数`{}`；原生`dimensions`、token数组、base64、user等不支持。`ExpectedDimensions`是调用方必须给出的本地结果要求，不能从首行响应推断或当作Provider默认值。Nonchat、InvocationID、Resolver/Facts/Runtime、Usage写入与真实consumer授权均未绑定。

## 3. 独立结论与当前授权

[原审查JSON](openai-embeddings-wire-spec-verification-evidence/objects/68129d089806c1226666a7d36f322185933df37de52349ae957c60ade33803ab)确认全部§1–7有界STATIC PASS、无阻断。严格JSON/向量索引/usage、单次POST、共享64/8 Budget及真正join后释放的验收要求已明确；专用解析缓冲小于21MiB的算术不冒实际RSS或性能结果。D04公开Response.Close当前返回nil，不能声称其暴露原生body内部Close错误。三个新真实顶层与六个旧Chat顶层只是后续验收计划，尚未运行。

[作者自查原结果](openai-embeddings-wire-spec-verification-evidence/objects/5423e1011d51fad026ca6cc8d10fec0bc66cc90381fd7b255d82521a646a3c15)记录原argv/cwd、exit0及嵌入stdout；它仅校验文档、路径、哈希和算术，没有另存受监督raw/env/actualwait，不能补造成产品命令证据。[作者报告](openai-embeddings-wire-spec-verification-evidence/objects/900603ebfad954ce38ee12c5ec5d66160a887f30b1a874ca37000e9e13dc44fb)中的两次早期定位错误只有叙述记录，无单独命令/raw，不造缺失原件。独立审查未运行产品、网络或资源。

按主线程实际ACK，唯一backend已启动私有阶段A五路径：输入/编码/parser、纯测试及manifest；constructor/Start/handle尚未实施，Budget/transport/fixture及最后README未开始，没有真实资源授权或产品接受。本档不读取活动实现。Runtime UI作者C已冻结1875检查，独立C已开始；该UI仍无真实资源或整卡接受，此句只记录协调事实，不纳其活动证据。

## 4. 最小档案与离线核验

[索引](openai-embeddings-wire-spec-verification-evidence/index.json)保存27逻辑原件、26个原字节对象（122343B）及正式卡Git引用；作者31项与独审另增1项固定Git源码不复制。所有必要声明原件已定位；上述记录粒度限制照实保留，不补造原运行。源码、依赖树、缓存与二进制均不在本次对象范围。

```sh
env PYTHONDONTWRITEBYTECODE=1 GIT_NO_LAZY_FETCH=1 python3 docs/development/agent-team/openai-embeddings-wire-spec-verification-evidence/verify_archive.py --repo /workspace/agenteam
```

[校验器](openai-embeddings-wire-spec-verification-evidence/verify_archive.py)只核原字节、固定Git、原页首差量、卡技术、行政旧段、链接和格式，不运行保存的脚本或产品。私有payload的实际检查结果随精确交付清单提供。既有Audit、Runtime与所有旧规格档按当时事实保留。

完整D08–D28/E01未完成、E01未开始，Summary初值待决、ready503及未绑定能力保持。Object原join、OpenAI tools独立任务、SPA concurrent-publication三项安全停止不重试、不改写、不改派；SPA有限受控/native通过不代表完整产品接受。
