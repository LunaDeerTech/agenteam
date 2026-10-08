<template>
  <span class="app-brand" aria-hidden="true">
    <img
      class="app-brand-image app-brand-image--light"
      src="/assets/brand/agenteam-horizontal-light.svg"
      alt=""
      width="1600"
      height="420"
      draggable="false"
    />
    <img
      class="app-brand-image app-brand-image--dark"
      src="/assets/brand/agenteam-horizontal-dark.svg"
      alt=""
      width="1600"
      height="420"
      draggable="false"
    />
  </span>
</template>

<style scoped>
.app-brand {
  display: block;
  width: 144px;
  max-width: 100%;
  line-height: 0;
}
.app-brand-image {
  display: block;
  width: 100%;
  height: auto;
}
.app-brand-image--dark {
  display: none;
}
:global(:root[data-theme='dark'] .app-brand-image--light) {
  display: none;
}
:global(:root[data-theme='dark'] .app-brand-image--dark) {
  display: block;
}
</style>
