# candidate03 与 integration 离线原件审核

**限定版本组合 PASS**：复用candidate01完整STATIC＋candidate02关闭IC-PG-01＋unit04普通/race pure接受，candidate03仅fixture字符串拼接两侧空格，无语义或import/fileset变化。作者integration-offline-summary01指纹 `c0b204b35fc368068a579025669131a0724f00ef97e7f7f466779bbfece7178e`。

逐项核9条实际command/result/stdout/stderr指纹及原argv，均输入完整字典前后一致、direct actualwait 1、adopted wait 0、两次owned空、无signal/强制尾部/超时，均小于45s。原format-preview01 actual1及其仅格式diff/空stderr保留；后继8条actual0为format02、full图、dynamic图、integration race-c、精确list、race vet及两cmd build。gofmt原失败不是生产/编译/PG失败。driver03只比已审driver02换固定overlay路径，42s干预/45s总预算不变。

独立重新解析两份已有go list原始JSON（没有重跑Go）：full453包/2966文件/20generated main；dynamic385包/2404文件；均无Error/DepsErrors，按7-map有效路径解析后与固定实际图完全相同，extra0。生成main指纹逐项相符，Project main无_test/_xtest.TestMain；两UI删除路径及web源均未被实际图消费。所有实际源/生成main已纳input-build并纳compile实际inputs_before。compile在两图终局之后，未把旧Audit图冒新图。没有执行其他19个main。

三binary当前SHA与汇总相同。Project binary `87cf0ea881f5c11240d177be27e6cbccfe739d471524ab54ceda7b926a0b18ce` 已纳精确list前输入；list原stdout严格等于四个预定top：新Facts/TransactionBoundary及旧两B02初始化selector。只有编译发现，不等于任何新/旧真实测试通过。两cmd仅build，未启动。

本轮仅仅读原件并运行自有Python指纹/JSON审核脚本，不执行Go/test/资源/安装/Git，不读取UI源内容。完整产品尚缺真实PG及独立风险补集，README未授；Skills/root/Object runtime等未绑定与三停止保持。后续PG仍须独占窗口、固定运行闭包和原120s含Cleanup/包6m、fresh/live基线/7资源actualwait双清。原IC-UNIT-01/02及IC-PG-01静态缺陷、pure的CGO前置缺项和格式原红全部沿旧原件保存。

独立私有补集另在 `../pg-probes01/`：两个top、7个源码子例，明确复用作者固定fixture并只新增单源/8-map；状态PREPARED_ONLY，未format/compile/实跑，actual_probe_graph=null。它不是本报告的动态证据。

证据机械核对详见checks.json与check.py；三个阶段来源result沿父目录candidate02-static和unit04-offline-review。报告冻结后停写。
