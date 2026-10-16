<script setup lang="ts">
import type { TableColumn, TableRow } from './types'
defineProps<{ columns: TableColumn[]; rows: TableRow[]; caption: string }>()
</script>
<template>
  <div class="ui-table-scroll" tabindex="0" :aria-label="caption + '，可横向滚动'">
    <table>
      <caption>
        {{
          caption
        }}
      </caption>
      <thead>
        <tr>
          <th v-for="column in columns" :key="column.key" scope="col">{{ column.label }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.id">
          <td v-for="column in columns" :key="column.key">
            <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]">{{
              row[column.key]
            }}</slot>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!rows.length" class="meta">暂无数据</p>
  </div>
</template>
