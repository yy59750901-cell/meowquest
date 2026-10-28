<script setup>
// CatSprite：hyclub 像素猫引擎的 Vue 组件版
// 帧表/序列/调色板 100% 复用逆向源码；prop: species / anim(auto,walk,sit,sleep,eat) / size
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps({
  species: { type: String, default: 'orange' },
  anim: { type: String, default: 'sit' }, // auto | walk | sit | sleep | eat
  size: { type: Number, default: 110 },
})

const PAL = {
  orange: { B: '#E8975A', p: '#F5E6C8', e: '#1B5E3A', z: '#2B2B2B', m: '#C14B6E' },
  black: { B: '#2B2B2B', p: '#2B2B2B', e: '#F6E58D', z: '#2B2B2B', m: '#E8688E' },
  cow: { B: '#FAF8F3', p: '#2B2B2B', e: '#2B2B2B', z: '#2B2B2B', m: '#E8688E' },
  white: { B: '#FAF8F3', p: '#FAF8F3', e: '#3B82C4', z: '#2B2B2B', m: '#E8688E' },
}
const FR = {
  idle: ['.B....B.....BB..', '.BB..BB.....BB..', '.BBBBBB.....BB..', 'BeBeBBBBppBBBB..', 'BBBBBBBpppBBBB..', 'BBBBBBBBBBBBB...', '.BBBBBBBBBBBB...', '.BBBBpppBBBBB...', '.BBBBpppBBBBB...', '.BB.BB..BB.BB...', '.BB.BB..BB.BB...', '.B...B..B...B...'],
  wag: ['.B....B......B..', '.BB..BB......BB.', '.BBBBBB......BB.', 'BeBeBBBBppBBBBB.', 'BBBBBBBpppBBBBB.', 'BBBBBBBBBBBBB...', '.BBBBBBBBBBBB...', '.BBBBpppBBBBB...', '.BBBBpppBBBBB...', '.BB.BB..BB.BB...', '.BB.BB..BB.BB...', '.B...B..B...B...'],
  walkA: ['.B....B.....BB..', '.BB..BB.....BB..', '.BBBBBB.....BB..', 'BeBeBBBppBBBBB..', 'BBBBBBpppBBBBB..', 'BBBBBBBBBBBBB...', '.BBBBBBBBBBBB...', '.BBBBpppBBBBB...', '.BBBBpppBBBBB...', 'BB...BBBB...BB..', 'BB...BBBB...BB..', 'B.....BB.....B..'],
  walkB: ['.B....B.....BB..', '.BB..BB.....BB..', '.BBBBBB.....BB..', 'BeBeBBBppBBBBB..', 'BBBBBBpppBBBBB..', 'BBBBBBBBBBBBB...', '.BBBBBBBBBBBB...', '.BBBBpppBBBBB...', '.BBBBpppBBBBB...', '..BBB....BBB....', '..BBB....BBB....', '...BB.....BB....'],
  sit: ['..B....B........', '..BB..BB........', '..BBBBBB........', '.BeBeBBB........', '.BBBBBBB........', '..BBBBBB........', '...BBBBppB......', '...BBBBppBB.....', '...BBBBBBBBB....', '...BB.BBBBBB..B.', '...BB.BppBBBBBB.', '..BBB.BppBBBBB..'],
  sleep: ['................', '................', '................', '................', '................', '..B....B........', '..BB..BBppBBB...', '..BBBBBBppBBBB..', '.BeeBeeBBBBBBBB.', '.BBBBBBBBppBBBB.', '..BBBBBBBppBBBB.', '.BBBBBBBBBBBBB..'],
  eat: ['.B....B.....BB..', '.BB..BB.....BB..', '.BBBBBB.....BB..', 'BeBeBBBBppBBBB..', 'BmmBBBBpppBBBB..', 'BmmBBBBBBBBBB...', '.BBBBBBBBBBBB...', '.BBBBpppBBBBB...', '.BBBBpppBBBBB...', '.BB.BB..BB.BB...', '.BB.BB..BB.BB...', '.B...B..B...B...'],
}
const VARf = (f, row, nl) => f.map((r, i) => (i === row ? nl : r))
const FR_sitBlink = VARf(FR.sit, 3, '.BBBBBBB........')
const FR_sitTail = VARf(FR.sit, 8, '...BBBBBBBBB..B.')
const FR_sleepDream = VARf(FR.sleep, 5, '..B....BppBBB...')
const SEQ = {
  sit: [[FR.sit, 1900], [FR_sitBlink, 140], [FR.sit, 850], [FR_sitTail, 260], [FR.sit, 260], [FR_sitTail, 260], [FR.sit, 1000]],
  sleep: [[FR.sleep, 1700], [FR_sleepDream, 1300], [FR.sleep, 900]],
  eat: [[FR.eat, 375]],
}
const AUTO_IDLE = ['idle', 'idle', 'idle', 'wag', 'idle', 'idle']
const AUTO_WALK = ['walkA', 'walkB', 'walkA', 'walkB']

const frame = ref(FR.sit)
let timer = null

function currentSeq() {
  if (props.anim === 'walk') return AUTO_WALK.map((f) => [FR[f], 180])
  if (props.anim === 'auto' || props.anim === 'idle') return AUTO_IDLE.map((f) => [FR[f], 600])
  return SEQ[props.anim] || SEQ.sit
}
function play() {
  clearTimeout(timer)
  const seq = currentSeq()
  let idx = 0
  const tick = () => {
    const cur = seq[idx % seq.length]
    frame.value = cur[0]
    idx++
    timer = setTimeout(tick, cur[1])
  }
  tick()
}
onMounted(play)
watch(() => [props.anim, props.species], play)
onBeforeUnmount(() => clearTimeout(timer))

const rects = computed(() => {
  const pal = PAL[props.species] || PAL.orange
  const out = []
  const f = frame.value
  for (let y = 0; y < 12; y++) {
    const row = f[y] || ''
    for (let x = 0; x < 16; x++) {
      const ch = row[x]
      if (!ch || ch === '.' || ch === ' ') continue
      const c = pal[ch]
      if (!c) continue
      out.push({ x, y, c })
    }
  }
  return out
})
</script>

<template>
  <svg :width="size" :height="Math.round(size * 0.75)" viewBox="0 0 16 12" shape-rendering="crispEdges"
    style="image-rendering: pixelated; filter: drop-shadow(2px 2px 0 rgba(43,43,43,.25)); display: block">
    <rect v-for="(r, i) in rects" :key="i" :x="r.x" :y="r.y" width="1.02" height="1.02" :fill="r.c" />
  </svg>
</template>
