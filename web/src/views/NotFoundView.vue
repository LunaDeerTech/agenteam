<template>
  <main id="main-content" class="not-found page-layout page-layout--collection">
    <header class="page-header">
      <div class="page-heading">
        <h1>未找到页面</h1>
        <p>请检查页面地址。</p>
      </div>
    </header>
    <div class="page-actions"><RouterLink to="/">返回入口</RouterLink></div>
  </main>
</template>
<style scoped>
.not-found {
  padding-top: 64px;
}
@media (max-width: 650px) {
  .not-found {
    padding-top: 40px;
  }
}
</style>
