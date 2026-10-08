# Image 固定源：现有 HTTPS 代理通道取证 — FAIL / STOP

首个 tree GET 的代理 CONNECT 收到403，curl实际exit56；stderr逐字匹配安全固定诊断 `curl: (56) CONNECT tunnel failed, response 403`。代理隧道未建立，**没有观察到 GitHub 应用层HTTP响应**。这不是GitHub API返回403的证据，也不能从正式GitHub push可用推导该读取通道获准。

本轮使用现有环境的标准curl代理行为，没有读取／输出代理值或凭据，没有新增／更换代理、修改DNS／trust／no_proxy或使用netrc／Git凭据。首失败立即停止：1次尝试，0次raw源请求，无重试、重定向或其它通道。原四路径均未取回，字段／直接import仍unknown，不是空集合。

curl请求实耗0.014061549秒，实际wait得到exit56；PID1085237/start5126715随后两次身份不存在，无强制终止。外层工具2237f9实际exit1，任务命令与reader已停止；不声称主机／完整进程树为空。原件保留headers、47B stderr、0B stdout、spawn、receipt及停止ledger。代理header声明Content-Length16，但curl未把该CONNECT错误body输出：0B仅指所存stdout，不能冒完整代理body、GitHub空响应或SDK源。

旧本地commit对象仍重算为 `becc1d20eed83c1b8d85e15dc131a372d9dc7813`，指向tree `f1b9a07a4b1f2d908bcb08d9cac4449f93616933`；LICENSE原blob复用。未验数字签名，也未取得tree成员证明或四blob。旧直连DNS失败e08e32cb及全部原件不改，它只描述旧直连工具；本轮新增的是现有代理CONNECT被拒，二者不能合并成所有GitHub通道不可用。

未安装或执行SDK，未形成Image字段契约、provider conformance或profile批准。无Go／Node／业务网络／业务资源／Git／仓库写入；仅获授公开来源尝试与本scratch记录。Jina与三停止保持。**失败原件冻结，STOP；不再网络或重复准备。**
