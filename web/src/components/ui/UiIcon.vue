<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 16 });

const paths: Record<string, string[]> = {
  grid: ["M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z"],
  settings: [
    "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8",
    "m9.5 3-.6 2-2 .9-1.9-.5-2.5 4.2 1.4 1.5v2L2.5 15 5 19.2l1.9-.5 2 .9.6 2.4h5l.6-2.4 2-.9 1.9.5 2.5-4.2-1.4-1.9v-2L21.5 9 19 4.8l-1.9.5-2-.9-.6-1.4z",
  ],
  menu: ["M4 6h16M4 12h16M4 18h16"],
  "chevron-down": ["m6 9 6 6 6-6"],
  "chevron-right": ["m9 6 6 6-6 6"],
  "arrow-left": ["m12 5-7 7 7 7M5 12h14"],
  sun: [
    "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8",
    "M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5",
  ],
  moon: ["M20.5 13a8.5 8.5 0 1 1-9.5-9.5A7 7 0 0 0 20.5 13Z"],
  monitor: ["M3 3h18v13H3zM8 21h8M12 16v5"],
  folder: [
    "M3 6a2 2 0 0 1 2-2h4l2 3h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z",
  ],
  check: ["m5 12 4 4L19 6"],
  close: ["m6 6 12 12M18 6 6 18"],
  plus: ["M12 5v14M5 12h14"],
  search: ["M10.5 3a7.5 7.5 0 1 0 0 15 7.5 7.5 0 0 0 0-15M16 16l5 5"],
  sliders: ["M4 7h6M14 7h6M10 4v6M4 17h10M18 17h2M14 14v6"],
  users: [
    "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8M17 4a4 4 0 0 1 0 7M22 21v-2a4 4 0 0 0-3-3.9",
  ],
  key: ["M7 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8M10 10l10 10M16 16l3-3M18 18l3-3"],
  lock: ["M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4"],
  file: ["M14 2H5v20h14V7zM14 2v6h5M8 13h8M8 17h5"],
  text: ["M4 5h16M12 5v14M8 19h8"],
  image: ["M3 3h18v18H3z", "m3 16 5-5 4 4 3-3 6 6M8 7h.01"],
  vector: ["M4 4v16h16", "M4 20 18 6M11 6h7v7"],
  link: [
    "m10 13 4-4M8 16l-1 1a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0M16 8l1-1a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0",
  ],
  clock: ["M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18M12 7v5l3 2"],
  shield: ["m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6z", "m8 12 3 3 5-6"],
  layers: ["m12 3 10 5-10 5L2 8zM2 12l10 5 10-5M2 16l10 5 10-5"],
  chart: ["M4 3v18h17M9 17v-6M14 17V7M19 17v-3"],
  code: ["m8 6-6 6 6 6M16 6l6 6-6 6M14 3l-4 18"],
  server: ["M3 3h18v7H3zM3 14h18v7H3zM7 6.5h.01M7 17.5h.01"],
  mail: ["M3 5h18v14H3z", "m3 5 9 7 9-7"],
  globe: [
    "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18",
    "M3 12h18M12 3c5 5 5 13 0 18-5-5-5-13 0-18",
  ],
  history: ["M3 11a9 9 0 1 1 2.6 7.4M3 4v7h7", "M12 7v5l3 2"],
  activity: ["M3 12h4l3-8 4 16 3-8h4"],
  user: [
    "M12 3a4 4 0 1 0 0 8 4 4 0 0 0 0-8",
    "M4 21v-2a4 4 0 0 1 4-4h8a4 4 0 0 1 4 4v2",
  ],
};
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    class="ui-icon"
  >
    <path
      v-for="(path, index) in paths[name] ?? paths.grid"
      :key="index"
      :d="path"
    />
  </svg>
</template>
