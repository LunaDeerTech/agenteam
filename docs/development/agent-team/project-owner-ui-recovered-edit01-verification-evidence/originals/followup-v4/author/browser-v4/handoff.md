# browser-v4：项目名称可访问角色定位

root 正式授权依据 runtime 唯一本地语义后继02：required UiField 的 aria-hidden星号仍使 exact getByLabel(项目名称) 计数0并导致fill超时；role textbox exact accessible name项目名称计数1且fill成功。描述及星号/aria-hidden控制通过。该结论确定 locator 缺陷；edit01没有DOM记录，不能据此排除当轮所有状态因素。edit01原失败与v1-v3原件均保持。

本版仅修改#21：新增 projectName(page) => page.getByRole('textbox', {name:'项目名称', exact:true})，替换7处名称定位（edit3/layouts4）。fill/value/focus/样式观察/readonly行为断言及全部description定位保持；差量见delta.patch。#20、19UI、Go、共享控件、锁、README没有修改。

新冻结两路径中#20为只读复用同SHA；imports/tools原字节复用v3。最终限定离线format-check0.602s、strict TS/checkJs1.499s、五mode各1项--list4.284s均PASS；actualwait、输入同、owned PID双空，见checks.json。format write01 inputs_same=false正常保留。

本实例没有运行真实浏览器业务/PG/listener、资产交换或独立语义实验。两源当前STOP；后续final03由root独审并绑定另授资源。browser-v4真实edit/layout等仍pending，不能将离线检查写为完整D27接受。
