<script setup lang="ts">
import type { MeetingDetail, MeetingReference } from '../../api/meetings'
import { UiButton } from '../ui'
defineProps<{ detail: MeetingDetail }>()
const emit = defineEmits<{ open: [reference: MeetingReference] }>()
</script>
<template>
  <div class="meeting-info">
    <section aria-label="参会成员">
      <h3>参会成员</h3>
      <ul>
        <li v-for="p in detail.participants.filter((p) => p.active)" :key="p.id">
          <span class="identity">{{ p.kind === 'agent' ? 'Agent' : '用户' }}</span
          ><span>{{ p.name }}</span>
        </li>
      </ul>
    </section>
    <section aria-label="会议摘要">
      <h3>会议摘要</h3>
      <p v-if="!detail.session.summary" class="muted">首轮完成后生成摘要。</p>
      <template v-else
        ><p v-if="detail.session.summary.pending_update" class="notice" role="status">
          摘要待更新，以下内容尚未覆盖最新变化。
        </p>
        <dl>
          <template
            v-for="item in [
              { key: 'goals', label: '目标' },
              { key: 'decisions', label: '结论' },
              { key: 'unresolved', label: '待解决' },
              { key: 'facts', label: '事实' },
            ] as const"
            :key="item.key"
            ><dt>{{ item.label }}</dt>
            <dd>{{ detail.session.summary[item.key] || '暂无' }}</dd></template
          >
        </dl></template
      >
    </section>
    <section v-if="detail.references.length" aria-label="会议引用">
      <h3>引用</h3>
      <ul>
        <li v-for="r in detail.references" :key="r.kind + ':' + r.id">
          <span class="identity">{{
            { task: '任务', knowledge: '知识', link: '链接', file: '文件' }[r.kind]
          }}</span
          ><UiButton
            variant="ghost"
            :aria-label="
              '打开' +
              { task: '任务', knowledge: '文档', link: '链接', file: '文件' }[r.kind] +
              ' ' +
              (r.label || r.id)
            "
            @click="emit('open', r)"
            >{{ r.label || r.id }}</UiButton
          >
        </li>
      </ul>
    </section>
  </div>
</template>
<style scoped>
.meeting-info {
  display: grid;
  gap: 24px;
  font-size: 13px;
}
h3 {
  margin: 0 0 12px;
  font-size: 13px;
  font-weight: 600;
}
ul {
  list-style: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: 10px;
}
li {
  display: flex;
  gap: 8px;
  align-items: baseline;
  overflow-wrap: anywhere;
}
.identity,
.muted,
dt {
  color: var(--muted);
}
.identity {
  font-size: 11px;
  flex: none;
}
dl {
  margin: 0;
}
dt {
  font-size: 12px;
  margin-top: 12px;
}
dd {
  margin: 4px 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.notice {
  color: var(--warning);
}
</style>
