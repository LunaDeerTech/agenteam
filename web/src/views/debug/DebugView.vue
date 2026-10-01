<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import * as Ui from '../../components/ui'
import DebugSection from './DebugSection.vue'
import { useTheme, type ThemeMode } from '../../composables/useTheme'
import { useActionFeedback } from '../../composables/useActionFeedback'
import { choices, sections, tokenNames, treeNodes, executionLog } from './fixtures'
const route = useRoute()
const router = useRouter()
const { mode, resolved, setTheme } = useTheme()
const theme = computed({ get: () => mode.value, set: (value) => setTheme(value as ThemeMode) })
const themeOptions = [
  { value: 'system', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
]
const text = ref('组件示例')
const invalid = ref('')
const checked = ref(true)
const enabled = ref(true)
const radio = ref('document')
const selection = ref('document')
const selectedNode = ref('design')
const expandedNodes = ref(['design'])
const collapse = ref(false)
const dialog = ref(false)
const danger = ref(false)
const drawer = ref(false)
const search = ref(false)
const info = ref(false)
const catalog = ref(false)
const tab = ref('overview')
const page = ref(1)
const message = ref('')
const messages = ref<{ id: number; text: string }[]>([])
const created = ref(false)
const recovered = ref(false)
const localResult = ref('尚未执行操作')
const savedValue = ref('组件示例')
const save = useActionFeedback()
const complete = useActionFeedback()
const send = useActionFeedback()
const retry = useActionFeedback()
const confirm = useActionFeedback()
const tokens = ref<{ name: string; value: string }[]>([])
function updateTokens() {
  const css = getComputedStyle(document.documentElement)
  tokens.value = tokenNames.map((name) => ({ name, value: css.getPropertyValue(name).trim() }))
}
watch(resolved, () => nextTick(updateTokens))
onMounted(updateTokens)
function saveText() {
  void save.run(() => {
    savedValue.value = text.value
  })
}
function sendText(value: string) {
  void send.run(() => {
    messages.value.push({ id: Date.now(), text: value })
    message.value = ''
  })
}
function menuAction(id: string) {
  if (id === 'delete') danger.value = true
  else if (id === 'reset') {
    selection.value = 'document'
    localResult.value = '已重置为文档'
  } else {
    drawer.value = true
  }
}
async function jump(id: string) {
  catalog.value = false
  await router.replace({ hash: '#' + id })
  await nextTick()
  const element = document.getElementById(id)
  element?.scrollIntoView({ block: 'start' })
  element?.focus({ preventScroll: true })
}
function selectSection(id: string) {
  void jump(id)
}
onMounted(() => {
  const id = route.hash.slice(1)
  if (sections.some((s) => s.id === id)) void jump(id)
})
</script>
<template>
  <div class="debug-layout">
    <aside class="debug-index">
      <nav aria-label="组件目录">
        <a
          v-for="section in sections"
          :key="section.id"
          :href="'#' + section.id"
          @click.prevent="jump(section.id)"
          >{{ section.label }}</a
        >
      </nav>
    </aside>
    <div class="debug-main">
      <div class="debug-heading">
        <div>
          <h1>组件与样式</h1>
          <p class="meta">本地演示数据 · 公共组件的独立展示，不代表业务页面布局。</p>
        </div>
        <div class="debug-tools">
          <Ui.UiButton class="mobile-catalog" @click="catalog = true">组件目录</Ui.UiButton
          ><Ui.UiButton @click="search = true"><Ui.UiIcon name="search" />查找组件</Ui.UiButton>
          <div class="theme-control">
            <Ui.UiSelect v-model="theme" label="主题" :options="themeOptions" />
          </div>
        </div>
      </div>
      <div class="debug-grid">
        <DebugSection
          id="buttons"
          title="按钮与反馈"
          description="成功反馈留在触发按钮；失败保留原因。"
        >
          <div class="ui-row">
            <Ui.UiButton
              variant="primary"
              :state="complete.state.value"
              success-label="已完成"
              @click="
                complete.run(() => {
                  localResult = '本地操作已完成'
                })
              "
              >完成操作</Ui.UiButton
            ><Ui.UiButton @click="localResult = '普通操作已触发'">普通按钮</Ui.UiButton
            ><Ui.UiButton variant="ghost" @click="localResult = '文字操作已触发'"
              >文字按钮</Ui.UiButton
            ><Ui.UiButton variant="danger" @click="danger = true">删除示例</Ui.UiButton
            ><Ui.UiButton icon aria-label="查看详情" @click="drawer = true"
              ><Ui.UiIcon name="more"
            /></Ui.UiButton>
          </div>
          <div class="ui-row">
            <Ui.UiButton disabled>暂不可用</Ui.UiButton
            ><Ui.UiButton state="loading" loading-label="处理中">加载</Ui.UiButton
            ><Ui.UiButton state="success" success-label="已保存">保存</Ui.UiButton>
          </div>
          <p class="meta" role="status">{{ localResult }}</p>
        </DebugSection>
        <DebugSection
          id="forms"
          title="表单控件"
          description="标签、帮助与错误有明确关联；保留原生表单语义。"
        >
          <Ui.UiField label="名称" hint="输入和保存只改变本地示例。"
            ><template #default="field"
              ><Ui.UiInput
                :id="field.id"
                v-model="text"
                :aria-describedby="field.describedby" /></template
          ></Ui.UiField>
          <Ui.UiField
            label="必填内容"
            :error="invalid.trim() ? undefined : '此字段不能为空'"
            required
            ><template #default="field"
              ><Ui.UiInput
                :id="field.id"
                v-model="invalid"
                :invalid="field.invalid"
                :aria-describedby="field.describedby"
                placeholder="输入后清除错误" /></template
          ></Ui.UiField>
          <Ui.UiField label="只读内容"
            ><template #default="field"
              ><Ui.UiInput
                :id="field.id"
                model-value="可以选择、复制的只读文字"
                readonly /></template
          ></Ui.UiField>
          <Ui.UiField label="禁用内容" hint="该示例字段未开放编辑。"
            ><template #default="field"
              ><Ui.UiInput
                :id="field.id"
                model-value="暂不可编辑"
                :aria-describedby="field.describedby"
                disabled /></template
          ></Ui.UiField>
          <div class="ui-row">
            <Ui.UiCheckbox v-model="checked">接收提醒</Ui.UiCheckbox
            ><Ui.UiCheckbox :model-value="false">未选中</Ui.UiCheckbox
            ><Ui.UiCheckbox disabled>不可用</Ui.UiCheckbox>
          </div>
          <Ui.UiRadioGroup
            v-model="radio"
            legend="内容类型"
            :options="choices.slice(0, 2)"
          /><Ui.UiSwitch v-model="enabled"
            >启用示例 · {{ enabled ? '已启用' : '已关闭' }}</Ui.UiSwitch
          ><Ui.UiSwitch :model-value="false" disabled>开关不可用</Ui.UiSwitch>
          <div class="ui-row">
            <Ui.UiButton
              variant="primary"
              :state="save.state.value"
              loading-label="保存中"
              success-label="已保存"
              @click="saveText"
              >保存修改</Ui.UiButton
            ><span class="meta">已保存：{{ savedValue }}</span>
          </div>
        </DebugSection>
        <DebugSection
          id="selectors"
          title="选择器与菜单"
          description="值选择、动作菜单和说明按用途区分。"
        >
          <Ui.UiField label="内容选择"
            ><template #default="field"
              ><Ui.UiSelect
                :id="field.id"
                v-model="selection"
                :options="choices"
                label="内容选择" /></template
          ></Ui.UiField>
          <Ui.UiSelect model-value="document" :options="choices" label="禁用选择器" disabled />
          <Ui.UiField label="原生选择器（禁用状态）"
            ><template #default="field"
              ><select :id="field.id" class="ui-input" disabled>
                <option>文档</option>
              </select></template
            ></Ui.UiField
          >
          <div class="ui-row">
            <Ui.UiMenu
              label="更多操作"
              :actions="[
                { id: 'detail', label: '查看详情' },
                { id: 'reset', label: '重置选择' },
                { id: 'unavailable', label: '尚未开放', disabled: true },
                { id: 'delete', label: '删除示例', danger: true },
              ]"
              @select="menuAction"
            /><Ui.UiPopover v-model:open="info" label="锚定说明"
              ><p class="popover-copy">
                简短说明保留在触发入口附近，不使用搜索浮层。
              </p></Ui.UiPopover
            >
          </div>
        </DebugSection>
        <DebugSection
          id="tree"
          title="文档树与折叠"
          description="选择名称与展开子节点独立，支持方向键和 Home / End。"
        >
          <Ui.UiTree
            v-model:selected="selectedNode"
            v-model:expanded="expandedNodes"
            :nodes="treeNodes"
            label="文档示例"
            ><template #suffix="{ node }"
              ><Ui.UiBadge v-if="node.id === 'colors'" tone="success">✓ 就绪</Ui.UiBadge></template
            ></Ui.UiTree
          >
          <p class="meta">选中编号：{{ selectedNode }}</p>
          <Ui.UiCollapse v-model:open="collapse" label="执行详情 · 默认折叠"
            ><Ui.UiCode :code="executionLog" plain />
            <p class="meta">日志限制高度，超过后内部滚动；连续点击可反向切换。</p></Ui.UiCollapse
          >
        </DebugSection>
        <DebugSection
          id="status"
          title="状态与身份"
          description="文字与图标共同表达状态；身份色独立。"
        >
          <div class="ui-row">
            <Ui.UiBadge tone="success">✓ 已完成</Ui.UiBadge
            ><Ui.UiBadge tone="warning">! 受阻</Ui.UiBadge
            ><Ui.UiBadge tone="danger">× 失败</Ui.UiBadge
            ><Ui.UiBadge tone="accent">◐ 选中</Ui.UiBadge
            ><Ui.UiBadge tone="progress">进行中</Ui.UiBadge><Ui.UiBadge>○ 待开始</Ui.UiBadge
            ><Ui.UiBadge>× 已取消</Ui.UiBadge>
          </div>
          <div class="ui-row">
            <Ui.UiIdentity>架构师</Ui.UiIdentity><Ui.UiIdentity :color="2">前端工程师</Ui.UiIdentity
            ><Ui.UiIdentity :color="3">审查员</Ui.UiIdentity>
          </div>
          <div class="status-progress"><Ui.UiProgress label="进度" :value="65" /></div>
        </DebugSection>
        <DebugSection id="cards" title="卡片与列表" description="只展示内容容器，不拼装业务页面。">
          <Ui.UiCard
            ><div class="ui-stack">
              <p class="meta">编号：<code>DEMO-001</code></p>
              <h3>很长的对象名称在这里自然换行，保留完整信息便于阅读</h3>
              <p class="meta">卡片正文与元信息使用不同层级。</p>
              <div><Ui.UiBadge>示例标签</Ui.UiBadge></div>
            </div></Ui.UiCard
          >
          <Ui.UiList
            label="文档列表"
            :items="[
              { id: 'components', label: '组件规范' },
              { id: 'theme', label: '颜色与主题' },
            ]"
            @select="drawer = true"
            ><template #suffix><span class="meta">查看详情 ›</span></template></Ui.UiList
          >
          <Ui.UiTable
            caption="演示文档"
            :columns="[
              { key: 'name', label: '名称' },
              { key: 'type', label: '类型' },
              { key: 'status', label: '状态' },
            ]"
            :rows="[
              { id: '1', name: '界面规范', type: '文档', status: '就绪' },
              { id: '2', name: '交互规则', type: '文档', status: '待检查' },
            ]"
          />
        </DebugSection>
        <DebugSection
          id="messages"
          title="消息与输入"
          description="没有头像；Agent 在左侧，用户在右侧。"
        >
          <div class="debug-thread">
            <Ui.UiMessage role="agent" name="演示 Agent" time="10:38"
              ><p>
                讨论正文示例，包含
                <Ui.UiReference @select="drawer = true">文档引用</Ui.UiReference
                >。消息只作为组件展示。
              </p>
              <Ui.UiCode code="--accent: semantic-color" /></Ui.UiMessage
            ><Ui.UiMessage role="user" name="你" time="10:39"
              ><p>确认采用当前样式，这里是一条右侧用户消息。</p></Ui.UiMessage
            ><Ui.UiMessage
              v-for="item in messages"
              :key="item.id"
              role="user"
              name="你"
              time="刚刚 · 演示"
              ><p>{{ item.text }}</p></Ui.UiMessage
            >
          </div>
          <Ui.UiComposer v-model="message" :state="send.state.value" @send="sendText" />
        </DebugSection>
        <DebugSection
          id="layers"
          title="浮层与提示"
          description="保持焦点和上下文，关闭后恢复触发位置。"
        >
          <div class="ui-row">
            <Ui.UiButton @click="search = true">搜索浮层</Ui.UiButton
            ><Ui.UiButton :state="confirm.state.value" success-label="已确认" @click="dialog = true"
              >模态对话框</Ui.UiButton
            ><Ui.UiButton @click="drawer = true">详情面板</Ui.UiButton
            ><Ui.UiPopover label="锚定说明"
              ><p class="popover-copy">这是独立的锚定说明。</p></Ui.UiPopover
            >
          </div>
          <div>
            <Ui.UiTooltip text="辅助解释不包含交互内容，悬停或键盘聚焦可查看。"
              >悬停或聚焦查看提示</Ui.UiTooltip
            >
          </div>
        </DebugSection>
        <DebugSection id="states" title="加载、空数据与失败" description="反馈留在对应组件中。">
          <div class="debug-states">
            <Ui.UiState kind="loading" title="正在读取" description="正在加载示例内容…"
              ><Ui.UiSkeleton /></Ui.UiState
            ><Ui.UiState
              kind="empty"
              :title="created ? '已有一项内容' : '还没有内容'"
              :description="created ? '本地示例已创建。' : '新增第一项示例。'"
              ><Ui.UiButton :disabled="created" @click="created = true">{{
                created ? '已创建' : '创建内容'
              }}</Ui.UiButton></Ui.UiState
            ><Ui.UiState
              :kind="recovered ? 'empty' : 'error'"
              :title="recovered ? '✓ 已恢复' : '读取失败'"
              :description="recovered ? '示例数据读取成功。' : '暂时无法读取，请重试。'"
              ><Ui.UiButton
                :state="retry.state.value"
                success-label="已恢复"
                @click="
                  retry.run(() => {
                    recovered = true
                  })
                "
                >重试</Ui.UiButton
              ></Ui.UiState
            >
          </div>
        </DebugSection>
        <DebugSection id="navigation" title="导航与分页" description="骨架之外的局部导航组件。">
          <Ui.UiBreadcrumb
            :items="[{ label: '组件', to: '/debug' }, { label: '导航' }]"
          /><Ui.UiTabs
            v-model="tab"
            label="内容导航"
            :items="[
              { value: 'overview', label: '概览' },
              { value: 'activity', label: '活动' },
              { value: 'disabled', label: '不可用', disabled: true },
            ]"
            ><template #default="{ value }"
              >{{ value === 'overview' ? '概览' : '活动' }}内容 · 演示</template
            ></Ui.UiTabs
          ><Ui.UiPagination v-model="page" :total="8" />
          <p class="meta" role="status">第 {{ page }} 页 · 演示</p>
        </DebugSection>
        <DebugSection id="tokens" title="样式参数" description="直接读取当前生效的 CSS 变量。">
          <dl class="token-list">
            <div v-for="token in tokens" :key="token.name">
              <dt>{{ token.name }}</dt>
              <dd>
                <span
                  v-if="token.value.startsWith('#')"
                  class="token-swatch"
                  :style="{ background: token.value }"
                />{{ token.value }}
              </dd>
            </div>
          </dl>
        </DebugSection>
      </div>
    </div>
  </div>
  <Ui.UiSearchDialog
    v-model:open="search"
    title="查找组件"
    :items="sections"
    @select="selectSection"
  />
  <Ui.UiDialog v-model:open="dialog" title="模态对话框"
    ><p>完整确认或需要专注的内容使用模态。此示例仅更新本地反馈。</p>
    <Ui.UiMenu
      label="模态内菜单"
      :actions="[{ id: 'local', label: '查看本地状态' }]"
      @select="localResult = '已查看模态内状态'"
    /><template #footer="{ close }"
      ><Ui.UiButton @click="close">取消</Ui.UiButton
      ><Ui.UiButton
        variant="primary"
        @click="
          () => {
            confirm.run(() => {
              localResult = '本地确认已完成'
            })
            close()
          }
        "
        >确认示例</Ui.UiButton
      ></template
    ></Ui.UiDialog
  >
  <Ui.UiDialog v-model:open="danger" title="删除这个示例？"
    ><p>确认后仅清空本地发送的演示消息，不删除真实内容。</p>
    <template #footer="{ close }"
      ><Ui.UiButton @click="close">取消</Ui.UiButton
      ><Ui.UiButton
        variant="danger"
        @click="
          () => {
            messages = []
            localResult = '本地演示消息已清空'
            close()
          }
        "
        >确认删除</Ui.UiButton
      ></template
    ></Ui.UiDialog
  >
  <Ui.UiDrawer v-model:open="drawer" title="详情面板"
    ><h2>独立详情组件</h2>
    <p class="meta">标题、属性、说明及折叠内容，不模拟正式业务布局。</p>
    <dl class="detail-properties">
      <dt>编号</dt>
      <dd>DEMO-001</dd>
      <dt>类型</dt>
      <dd>组件示例</dd>
      <dt>状态</dt>
      <dd><Ui.UiBadge tone="success">✓ 就绪</Ui.UiBadge></dd>
    </dl>
    <Ui.UiCollapse label="运行信息"><Ui.UiCode code="source: vue-debug" /></Ui.UiCollapse
  ></Ui.UiDrawer>
  <Ui.UiDialog v-model:open="catalog" title="组件目录"
    ><Ui.UiButton
      v-for="section in sections"
      :key="section.id"
      variant="ghost"
      @click="jump(section.id)"
      >{{ section.label }}</Ui.UiButton
    ></Ui.UiDialog
  >
</template>
<style src="./debug.css"></style>
