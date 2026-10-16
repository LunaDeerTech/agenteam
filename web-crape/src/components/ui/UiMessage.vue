<script setup lang="ts">
defineProps<{ role: 'user' | 'agent'; name: string; time: string }>()
</script>
<template>
  <article class="ui-message" :class="role">
    <div class="message-content">
      <div class="message-meta">
        <strong>{{ name }} · {{ role === 'user' ? '用户' : 'Agent' }}</strong
        ><span>{{ time }}</span>
      </div>
      <div class="message-bubble"><slot /></div>
    </div>
  </article>
</template>
