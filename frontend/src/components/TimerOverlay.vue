<script setup>
// TimerOverlay：全屏计时页（大倒计时 + 猫陪练 + 防挂机）
// props: minutes, taskName, icon; emits: done(duration_sec), giveup
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { showConfirmDialog } from 'vant'
import CatSprite from './CatSprite.vue'

const props = defineProps({
  minutes: { type: Number, required: true },
  taskName: { type: String, default: '' },
  icon: { type: String, default: '⏱' },
  species: { type: String, default: 'orange' },
})
const emit = defineEmits(['done', 'giveup'])

const totalSec = computed(() => props.minutes * 60)
const leftSec = ref(props.minutes * 60)
const paused = ref(false)
const done = computed(() => leftSec.value <= 0)
const catAnim = ref('sit')

let tickTimer = null
let guardTimer = null
let guardTimeout = null

const mmss = computed(() => {
  const m = Math.floor(leftSec.value / 60)
  const ss = leftSec.value % 60
  return `${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`
})
const progress = computed(() => ((totalSec.value - leftSec.value) / totalSec.value) * 100)

function tick() {
  if (paused.value || done.value) return
  leftSec.value--
  if (done.value) {
    stopGuard()
    catAnim.value = 'eat'
  }
}

// 防挂机：随机 60~120s 弹"还在练吗"，15 秒不点暂停计时
function scheduleGuard() {
  stopGuard()
  guardTimer = setTimeout(() => {
    showGuard.value = true
    guardTimeout = setTimeout(() => {
      paused.value = true
      catAnim.value = 'sleep'
    }, 15000)
  }, 60000 + Math.random() * 60000)
}
const showGuard = ref(false)
function stillHere() {
  showGuard.value = false
  paused.value = false
  catAnim.value = 'sit'
  clearTimeout(guardTimeout)
  scheduleGuard()
}
function stopGuard() {
  clearTimeout(guardTimer)
  clearTimeout(guardTimeout)
}

async function quit() {
  const r = await showConfirmDialog({
    title: '不练了吗？',
    message: '现在退出这次计时就不算数啦，坚持一下下？',
    confirmButtonText: '继续练',
    cancelButtonText: '退出',
  }).catch(() => 'cancel')
  if (r !== 'confirm') {
    cleanup()
    emit('giveup')
  }
}

function finish() {
  cleanup()
  emit('done', totalSec.value)
}

function cleanup() {
  clearInterval(tickTimer)
  stopGuard()
}

onMounted(() => {
  tickTimer = setInterval(tick, 1000)
  scheduleGuard()
})
onBeforeUnmount(cleanup)
</script>

<template>
  <div class="timer-mask">
    <div class="timer-box">
      <div class="tt">{{ icon }} {{ taskName }}</div>

      <div class="big" :class="{ done }">{{ done ? '✓' : mmss }}</div>
      <div class="sub">{{ done ? '完成啦！' : '专心练，我陪着你～' }}</div>

      <!-- 猫陪练 -->
      <div class="pet"><CatSprite :anim="done ? 'eat' : catAnim" :size="110" :species="species" /></div>

      <div class="track"><i :style="{ width: progress + '%' }" /></div>

      <!-- 防挂机 -->
      <div v-if="showGuard && !done" class="guard" @click.stop>
        <b>🐾 还在练吗？</b>
        <small>{{ paused ? '计时已暂停…' : '15 秒内点一下，不然我要睡着啦' }}</small>
        <button @click="stillHere">在呢在呢！</button>
      </div>

      <div class="btns">
        <button v-if="!done" class="ghost" @click="quit">放弃</button>
        <button v-if="done" class="primary" @click="finish">提交任务</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.timer-mask { position: fixed; inset: 0; z-index: 200; background: linear-gradient(#FDF6E3, #FBEFD1); display: flex; align-items: center; justify-content: center; }
.timer-box { text-align: center; padding: 20px; }
.tt { font-size: 18px; font-weight: 700; color: #5F5E5A; }
.big { font-size: 96px; font-weight: 800; color: #2C2C2A; font-variant-numeric: tabular-nums; letter-spacing: 4px; margin: 18px 0 4px; }
.big.done { color: #1D9E75; font-size: 80px; }
.sub { font-size: 14px; color: #888780; margin-bottom: 22px; }
.pet { display: flex; justify-content: center; margin-bottom: 18px; }
.track { width: min(420px, 80vw); height: 18px; margin: 0 auto; background: #fff; border: 2px solid #2C2C2A; border-radius: 9px; overflow: hidden; }
.track i { display: block; height: 100%; background: linear-gradient(90deg, #97C459, #3B6D11); transition: width 1s linear; }
.btns { margin-top: 26px; display: flex; justify-content: center; gap: 14px; }
.btns button { font-size: 16px; font-weight: 700; cursor: pointer; border: 2px solid #2C2C2A; border-radius: 10px; padding: 12px 32px; box-shadow: 3px 3px 0 rgba(44,44,42,.7); }
.btns button:active { transform: translate(3px, 3px); box-shadow: none; }
.btns .primary { background: #1D9E75; color: #fff; }
.btns .ghost { background: #fff; color: #888780; }
.guard { position: fixed; left: 50%; bottom: 16vh; transform: translateX(-50%); background: #fff; border: 2px solid #2C2C2A; border-radius: 14px; box-shadow: 4px 4px 0 rgba(44,44,42,.6); padding: 16px 22px; text-align: center; animation: pop .2s ease; }
.guard b { font-size: 16px; display: block; margin-bottom: 6px; }
.guard small { color: #888780; font-size: 12px; display: block; margin-bottom: 10px; }
.guard button { font-size: 14px; font-weight: 700; cursor: pointer; background: #EF9F27; color: #fff; border: 2px solid #2C2C2A; border-radius: 8px; padding: 8px 20px; }
@keyframes pop { from { transform: translateX(-50%) scale(.9); opacity: 0; } }
</style>
