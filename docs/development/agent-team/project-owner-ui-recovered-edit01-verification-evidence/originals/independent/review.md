edit01 保留实际 FAIL；原件完整性与 owned 退役独立复核 PASS。确认一项 harness 必修：冻结 browser-v3 的精确名称 label 定位不匹配 required UiField 的标签文本。该结论没有接受完整 edit/layouts，也不证明 edit01 当时所有状态因素均已排除。

40 个原件大小/SHA 全部匹配、无缺项或额外文件。实际 direct exit1，命令 96.558s，唯一 Go top FAIL 52.78s；浏览器在固定 45s 总预算耗尽时停于 #21:627 的 getByLabel("项目名称", {exact:true}).fill("owner-duplicate")。前一行取消修改后的描述空值断言已完成。原始日志未保存失败时 DOM，也未显示元素已解析后的 readonly/actionability 阻塞，因此不能将它诊断为已证明的产品取消状态缺陷。

固定 ProjectGeneralSettings 将名称字段声明为 required，UiField 的 label 增加 aria-hidden 星号，实际标签文本结构为“项目名称 *”；description 无星号。两个输入均在同一 editor.ready form 内，cancelEdits 经守卫调用 applyCurrent，复制当前字段并设置 editor.ready=true，没有单独隐藏名称字段的分支。固定 Playwright1.56.1 的 internal:label 按 elementText 的完整标签文字匹配，并不因 aria-hidden 去掉星号。

独立本地 label-semantics02 使用同一冻结 Playwright 和系统 Chromium，只对 about:blank/setContent 的合成标签执行三组控制：原 required/aria-hidden 结构下 exact getByLabel 名称计数0且短 fill 预期超时；exact getByRole(textbox, name="项目名称") 计数1且 fill 成功。去 aria-hidden 后 role 仅匹配带星号名称；去星号后二者均正常匹配。描述 exactLabel 在三组均为1。实验实际 exit0，1.483s 含清理，browser.close、direct+4 adopted 实际 wait、owned 双空和 runtime 空均完成，0页面请求，无业务服务/资产/fixture。它证明选择器语义，不补回 edit01 当时的 DOM。

首次 label-semantics01 因我设置过长 TMPDIR 导致 Chromium SingletonSocket 启动失败，未执行 DOM/locator；原 script/result/raw 保留。direct exit1、4 adopted wait0、owned 双空在0.627s完成，遗留空目录的精确 rmdir 另记为后续清理，不能回填15s内完整清理通过。root 明确授权仅短 TMPDIR 的第二次实验，experiment.cjs 字节相同。环境缺 playwright 技能，root 根据用户既有验证授权明确允许直接使用已冻结工具；没有声称读过该技能。

最小修复建议是仅 #21 定义一个按 textbox 精确无障碍名称定位的 name helper，统一替换冻结 v3 的七处627/632/658/1117/1119/1131/1153，描述定位和共享 UiField 产品源码保持不变。最小语义回归已完成；修复后的固定差量与必要离线 format/type/list 另审，随后真实 new-edit 和 layouts 仍须在各自授权窗口验证。

失败前的顺序断言已完成首笔描述保存（空格/换行保留、version+1）、保存后 disabled/no-op 事实不变、清空描述、dirty navigation 继续编辑，以及取消修改后描述复原。10 个 sidecar/6 份原始 body 全部匹配：001–004 为准备 GET，005–006 是浏览器初读，007 PATCH/008 GET 为 version2，009 PATCH/010 GET 为 version3空描述。重复名称提交、版本冲突、改名canonical、已确认后当前读失败、最终 schema/public-client 和完成结果均未执行；没有 schema/client PASS 或截图。

edit01 业务资源的失败退役 PASS：direct PID/starttime 与4个adopted wait均匹配原过程记录，watchdog join；7个精确 container/network ID逐个两扫absent，owned/runtime/new resource双空、无关Docker baseline不变，monitor/cancellation/forced均0。TCP全状态差量包括TIME_WAIT在额外40.366s后双清，仅属补充轮询。4个新增PID1 daemon shim非owned，未停止且未宣称join。原raw记录Node、proxy Serve/body、Project准备服务/Guard及正式root实际join。1165输入前后相同，root在最终TCP扫描之后恢复原3-file资产，不构成本轮漂移。

本审查已STOP；无活动命令或本轮持有进程/运行资源。全部原件指纹、两次实验原件、精确ID及证据限度见evidence.json与manifest.json。未改产品、测试源码、仓库文档或Git。
