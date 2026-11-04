<script setup>
import { ref } from 'vue'
import CatClimb from '../components/CatClimb.vue'

// 只在开发环境开放；不接入账号、商店、任务或保存游戏成绩。
const opened = ref(true)
</script>

<template>
  <CatClimb v-if="opened" species="black" cat-name="黑黑" standalone @close="opened = false" />
  <main v-else class="climb-preview-home">
    <section>
      <p>本地手感原型</p>
      <h1>黑黑的无限爬高</h1>
      <p>落稳自动起跳，按住左右控制落点。先试固定路线，再试随机爬高。</p>
      <button type="button" @click="opened = true">重新进入爬高试玩</button>
      <small>不连接业务接口，不改变星星、任务或房间；不保存成绩。</small>
    </section>
  </main>
</template>

<style scoped>
.climb-preview-home { min-height: 100dvh; padding: 24px; box-sizing: border-box; display: grid; place-items: center; background: #f6ead8; color: #624c36; font-family: sans-serif; }
.climb-preview-home > section { width: min(420px, 100%); padding: 22px; box-sizing: border-box; border: 2px solid #c6a47e; border-radius: 8px; background: #fffaf0; }
h1 { font-size: 23px; }
p { line-height: 1.6; }
button { min-height: 44px; width: 100%; padding: 8px 12px; color: inherit; font: inherit; border: 2px solid #8ba286; border-radius: 5px; background: #e9efdf; cursor: pointer; }
small { display: block; margin-top: 14px; line-height: 1.6; color: #8d765e; }
</style>
