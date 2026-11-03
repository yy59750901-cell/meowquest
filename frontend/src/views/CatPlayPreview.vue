<script setup>
import { ref } from 'vue'
import CatPlayground from '../components/CatPlayground.vue'

// 只通过开发环境路由访问，不登录，不创建宠物，不请求任何业务接口。
const opened = ref(true)
const species = ref('black')
</script>

<template>
  <CatPlayground v-if="opened" :species="species" cat-name="黑黑" standalone @close="opened = false" />
  <main v-else class="play-preview-home">
    <section>
      <p class="eyebrow">本地试玩 · 第一段</p>
      <h1>和黑黑玩羽毛</h1>
      <p>拖一拖、停一停，看到猫准备好了再松手。</p>
      <label for="preview-cat">选择猫的外观</label>
      <select id="preview-cat" v-model="species">
        <option value="black">黑猫</option>
        <option value="orange">橘猫</option>
        <option value="cow">奶牛猫</option>
        <option value="white">白猫</option>
      </select>
      <button type="button" @click="opened = true">再和猫玩一次</button>
      <small>这里不连接业务接口，不改变任务、星星或房间。次数只保留在本轮试玩。</small>
    </section>
  </main>
</template>

<style scoped>
.play-preview-home { min-height: 100dvh; display: grid; place-items: center; padding: 24px; box-sizing: border-box; color: #624833; background: #f6ead7; font-family: sans-serif; }
.play-preview-home > section { width: min(480px, 100%); padding: 24px; box-sizing: border-box; background: #fffaf0; border: 2px solid #c99d78; border-radius: 16px; }
h1 { margin: 8px 0; font-size: 24px; }
p { line-height: 1.6; }
.eyebrow, small { color: #806345; font-size: 13px; }
label { display: block; margin-top: 18px; }
select, button { font: inherit; min-height: 44px; width: 100%; margin: 8px 0; border: 2px solid #c99d78; border-radius: 8px; padding: 8px 12px; color: #624833; background: #fff6e8; }
button { cursor: pointer; background: #e9f1dd; border-color: #b9caa7; }
small { display: block; margin-top: 12px; line-height: 1.6; }
</style>
