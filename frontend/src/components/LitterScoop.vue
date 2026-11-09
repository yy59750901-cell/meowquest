<script setup>
// 猫砂盆全屏覆层：房间只摆一个小图，放大版铲屎动作全部在这里，不常驻、不占房间。
// 交互骨架来自已验收的独立原型 prototypes/litter-box-demo.html。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import CatSprite from './CatSprite.vue'
import {
  clampLitterState, litterClumpPositions, litterClumpMarkup, litterMoodTier,
  LITTER_MOOD_TIERS, LITTER_SCOOP_HOME, LITTER_PICKUP_RADIUS, LITTER_BIN_TARGET, inLitterBin,
} from '../litter-box.js'

const props = defineProps({
  state: { type: Number, required: true },
  species: { type: String, default: 'orange' },
  mood: { type: Number, default: 100 },
})
const emit = defineEmits(['close', 'scooped'])

const stageEl = ref(null)
const unit = ref(1)
const initial = clampLitterState(props.state)
const tier = computed(() => LITTER_MOOD_TIERS[litterMoodTier(props.mood)])
const moodLabel = computed(() => ({ good: '心情好', ok: '一般', low: '有点蔫' })[litterMoodTier(props.mood)])

const clumps = ref([])
const scoopPos = ref({ ...LITTER_SCOOP_HOME })
const held = ref(null)
const dragging = ref(false)
const demoRunning = ref(false)
const doneCount = ref(0)
let totalScooped = 0
const bubble = ref('')
const hint = ref('按住铲子 → 拖到砂块上铲起 → 拖到右边小桶松手')
const sub = ref('把结团砂块铲进右边小桶')
const binOpen = ref(false)
const binBump = ref(false)
const catHop = ref(false)
const scoopShake = ref(false)
const scoopReturning = ref(false)
const grains = ref([])
const hearts = ref([])

const clumpSvg = litterClumpMarkup()
const px = (n) => `calc(var(--u) * ${n})`
const clumpStyle = (c) => ({ left: px(c.x), top: px(c.y) })
const scoopStyle = computed(() => ({ left: px(scoopPos.value.x), top: px(scoopPos.value.y) }))
const catStyle = computed(() => ({
  left: px(tier.value.catX), top: px(tier.value.catY),
  filter: tier.value.grayscale ? 'grayscale(.75)' : 'none',
}))
const bubbleStyle = computed(() => ({ right: px(-8), bottom: 'calc(100% + 8px)' }))

function syncUnit() {
  if (stageEl.value) unit.value = stageEl.value.clientWidth / 680
}
function logical(e) {
  const r = stageEl.value.getBoundingClientRect()
  return {
    x: (e.clientX - r.left - stageEl.value.clientLeft) / unit.value,
    y: (e.clientY - r.top - stageEl.value.clientTop) / unit.value,
  }
}
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

function buildClumps() {
  clumps.value = litterClumpPositions(initial).map((p, i) => ({
    id: i, x: p.x, y: p.y, homeX: p.x, homeY: p.y, state: 'ground',
  }))
}
function setScoop(x, y) {
  scoopPos.value = { x, y }
  if (held.value) {
    held.value.x = x
    held.value.y = y + 12
  }
}
const scoopFace = () => ({ x: scoopPos.value.x, y: scoopPos.value.y + 12 })

function say(text) { bubble.value = text }

function spawnGrains(x, y) {
  const batch = Array.from({ length: 9 }, (_, i) => ({
    id: `${Date.now()}-${i}`,
    x: x - 18 + Math.random() * 36,
    y: y - 4 + Math.random() * 10,
    gx: `${Math.random() * 26 - 13}px`,
  }))
  grains.value = [...grains.value, ...batch]
  setTimeout(() => {
    const ids = new Set(batch.map((g) => g.id))
    grains.value = grains.value.filter((g) => !ids.has(g.id))
  }, 460)
}
function spawnHearts() {
  const batch = Array.from({ length: 4 }, (_, i) => ({
    id: `${Date.now()}-${i}`,
    x: tier.value.catX + 20 + i * 16,
    y: tier.value.catY + 6,
    hx: `${Math.random() * 30 - 15}px`,
    delay: `${i * 90}ms`,
  }))
  hearts.value = [...hearts.value, ...batch]
  setTimeout(() => {
    const ids = new Set(batch.map((h) => h.id))
    hearts.value = hearts.value.filter((h) => !ids.has(h.id))
  }, 1200)
}
function hopCat() {
  catHop.value = false
  void nextTick(() => { catHop.value = true })
  spawnHearts()
}

function tryPickup() {
  if (held.value || doneCount.value >= clumps.value.length) return
  const f = scoopFace()
  let best = null
  let bestD = LITTER_PICKUP_RADIUS
  for (const c of clumps.value) {
    if (c.state !== 'ground') continue
    const d = Math.hypot(c.x - f.x, c.y - f.y)
    if (d < bestD) { bestD = d; best = c }
  }
  if (!best) return
  held.value = best
  best.state = 'held'
  spawnGrains(best.x, best.y)
  scoopShake.value = false
  void nextTick(() => { scoopShake.value = true })
  say(tier.value.pick)
  setScoop(scoopPos.value.x, scoopPos.value.y)
}

function release() {
  const c = held.value
  if (!c) return
  held.value = null
  binOpen.value = false
  if (inLitterBin(scoopFace())) {
    c.state = 'gone'
    doneCount.value += 1
    totalScooped += 1
    binBump.value = false
    void nextTick(() => { binBump.value = true })
    hopCat()
    if (doneCount.value >= clumps.value.length) {
      say(tier.value.done)
      hint.value = '全铲干净啦！点「关闭」回房间，看看小图变什么样'
      sub.value = '铲完啦，关掉覆层看看小图'
    } else {
      say(tier.value.drop)
    }
    scoopReturning.value = true
    setScoop(LITTER_SCOOP_HOME.x, LITTER_SCOOP_HOME.y)
    setTimeout(() => { scoopReturning.value = false }, 480)
  } else {
    c.state = 'ground'
    c.x = c.homeX
    c.y = c.homeY
    say(tier.value.back)
  }
}

function reset() {
  demoRunning.value = false
  held.value = null
  doneCount.value = 0
  totalScooped = 0
  binOpen.value = false
  binBump.value = false
  catHop.value = false
  scoopShake.value = false
  grains.value = []
  hearts.value = []
  hint.value = '按住铲子 → 拖到砂块上铲起 → 拖到右边小桶松手'
  sub.value = '把结团砂块铲进右边小桶'
  buildClumps()
  setScoop(LITTER_SCOOP_HOME.x, LITTER_SCOOP_HOME.y)
  say(tier.value.start)
}

function onScoopDown(e) {
  if (demoRunning.value) return
  e.preventDefault()
  e.stopPropagation()
  dragging.value = true
  scoopReturning.value = false
  e.currentTarget.setPointerCapture(e.pointerId)
  const p = logical(e)
  setScoop(p.x, p.y)
}
function onScoopMove(e) {
  if (!dragging.value || demoRunning.value) return
  const p = logical(e)
  setScoop(p.x, p.y)
  tryPickup()
  if (held.value) binOpen.value = true
}
function onScoopUp(e) {
  if (!dragging.value) return
  dragging.value = false
  if (e && e.pointerId !== undefined && e.currentTarget.hasPointerCapture?.(e.pointerId)) {
    e.currentTarget.releasePointerCapture(e.pointerId)
  }
  release()
}

function glideTo(x, y, ms, pickup) {
  return new Promise((resolve) => {
    const from = { ...scoopPos.value }
    const t0 = performance.now()
    const step = (t) => {
      const k = Math.min(1, (t - t0) / ms)
      const e = k < 0.5 ? 2 * k * k : 1 - Math.pow(-2 * k + 2, 2) / 2
      setScoop(from.x + (x - from.x) * e, from.y + (y - from.y) * e)
      if (k < 1) requestAnimationFrame(step)
      else {
        // 演示路径会路过别的砂块，只在终点判定一次，保证每轮铲的是目标那块。
        if (pickup) tryPickup()
        if (held.value) binOpen.value = true
        resolve()
      }
    }
    requestAnimationFrame(step)
  })
}
async function autoDemo() {
  if (demoRunning.value) return
  reset()
  demoRunning.value = true
  await sleep(320)
  const targets = [...clumps.value]
  for (let i = 0; i < targets.length && demoRunning.value; i++) {
    const c = targets[i]
    await glideTo(c.homeX, c.homeY - 12, 700, true)
    await sleep(360)
    if (!demoRunning.value) return
    await glideTo(LITTER_BIN_TARGET.x, LITTER_BIN_TARGET.y, 820)
    await sleep(220)
    release()
    if (i < targets.length - 1) await sleep(620)
  }
  demoRunning.value = false
}

function close() {
  demoRunning.value = false
  // 再玩一次会重置本轮进度，累计值钳到打开时的档位，避免重复扣状态。
  emit('scooped', Math.min(totalScooped, initial))
  emit('close')
}
function onKeydown(e) {
  if (e.key === 'Escape') close()
}

let observer = null
onMounted(() => {
  syncUnit()
  if (window.ResizeObserver) {
    observer = new ResizeObserver(syncUnit)
    observer.observe(stageEl.value)
  }
  window.addEventListener('resize', syncUnit)
  document.addEventListener('keydown', onKeydown)
  reset()
})
onBeforeUnmount(() => {
  demoRunning.value = false
  observer?.disconnect()
  window.removeEventListener('resize', syncUnit)
  document.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <div class="litter-overlay" role="dialog" aria-modal="true" aria-label="铲猫屎全屏覆层" @pointerdown.self="close">
    <div class="litter-card">
      <div class="litter-head">
        <div><b>铲猫屎</b><small>{{ sub }}</small></div>
        <span class="spacer"></span>
        <button type="button" @click="close">关闭 ✕</button>
      </div>
      <div class="litter-controls">
        <span class="mood-chip">心情 {{ Math.round(mood) }} · {{ moodLabel }}
          <span class="bar"><i :style="{ width: Math.max(0, Math.min(100, mood)) + '%' }" /></span>
        </span>
        <span class="spacer"></span>
        <span class="progress">已铲 {{ doneCount }} / {{ clumps.length }}</span>
        <button type="button" class="primary" :disabled="demoRunning" @click="autoDemo">看一遍演示</button>
        <button type="button" :disabled="demoRunning" @click="reset">再玩一次</button>
      </div>
      <div class="litter-stage-shell">
        <div class="litter-stage" ref="stageEl" :style="{ '--u': unit + 'px' }"
          role="img" aria-label="猫砂盆近景：拖动铲子把结团砂块铲进右侧小桶">
          <div class="litter-world">
            <div class="wall"></div>
            <div class="skirting"></div>
            <div class="floor"></div>

            <svg class="pixel tub" viewBox="0 0 380 150" shape-rendering="crispEdges" aria-hidden="true">
              <rect x="0" y="24" width="380" height="126" fill="#8B6345"/>
              <rect x="8" y="30" width="364" height="112" fill="#C9A276"/>
              <rect x="8" y="6" width="364" height="24" fill="#E0BC90"/>
              <rect x="8" y="6" width="364" height="6" fill="#F2D9B0"/>
              <rect x="8" y="24" width="364" height="6" fill="#B98C61"/>
              <rect x="20" y="38" width="340" height="78" fill="#EFE0BE"/>
              <rect x="20" y="38" width="340" height="5" fill="#E4D0A6"/>
              <g fill="#D8BF95">
                <rect x="36" y="54" width="5" height="4"/><rect x="72" y="72" width="4" height="4"/>
                <rect x="112" y="50" width="5" height="4"/><rect x="146" y="88" width="4" height="4"/>
                <rect x="188" y="58" width="5" height="4"/><rect x="224" y="98" width="4" height="4"/>
                <rect x="268" y="52" width="5" height="4"/><rect x="304" y="82" width="4" height="4"/>
                <rect x="336" y="62" width="5" height="4"/><rect x="58" y="102" width="4" height="4"/>
                <rect x="128" y="106" width="5" height="4"/><rect x="210" y="46" width="4" height="4"/>
                <rect x="292" y="106" width="5" height="4"/><rect x="342" y="96" width="4" height="4"/>
              </g>
              <rect x="0" y="116" width="380" height="34" fill="#A47B55"/>
              <rect x="8" y="120" width="364" height="7" fill="#D4A875"/>
              <rect x="8" y="138" width="364" height="12" fill="#8B6345"/>
              <rect x="24" y="132" width="26" height="6" fill="#C99B6E"/>
              <rect x="330" y="132" width="26" height="6" fill="#C99B6E"/>
            </svg>

            <div v-for="c in clumps" :key="c.id" class="clump"
              :class="{ held: c.state === 'held', gone: c.state === 'gone' }"
              :style="clumpStyle(c)" v-html="clumpSvg"></div>

            <div class="bin-open" :class="{ show: binOpen }"><span class="bin-label">放这里松手</span></div>
            <svg class="pixel bin" viewBox="0 0 116 132" shape-rendering="crispEdges" aria-hidden="true"
              :class="{ bump: binBump }">
              <rect x="18" y="26" width="80" height="98" fill="#6E8F78"/>
              <rect x="24" y="30" width="68" height="92" fill="#9DBBA3"/>
              <rect x="24" y="30" width="10" height="92" fill="#B6CFB9"/>
              <rect x="82" y="30" width="10" height="92" fill="#82A18A"/>
              <rect x="8" y="14" width="100" height="16" fill="#5F8169"/>
              <rect x="8" y="14" width="100" height="5" fill="#7FA48A"/>
              <rect x="50" y="4" width="16" height="12" fill="#5F8169"/>
              <rect x="40" y="54" width="7" height="46" fill="#7FA48A"/>
              <rect x="55" y="54" width="7" height="46" fill="#7FA48A"/>
              <rect x="70" y="54" width="7" height="46" fill="#7FA48A"/>
              <rect x="18" y="118" width="80" height="8" fill="#5F8169"/>
            </svg>

            <div class="cat" :class="{ hop: catHop }" :style="catStyle">
              <CatSprite :species="species" :anim="tier.anim" :size="110" />
              <div class="bubble" :style="bubbleStyle">{{ bubble }}</div>
            </div>

            <div class="scoop" :class="{ shake: scoopShake, returning: scoopReturning }"
              :style="scoopStyle" aria-label="可拖动的猫砂铲"
              @pointerdown="onScoopDown" @pointermove="onScoopMove"
              @pointerup="onScoopUp" @pointercancel="onScoopUp">
              <svg viewBox="0 0 92 78" shape-rendering="crispEdges">
                <rect x="40" y="0" width="13" height="30" fill="#8B6345"/>
                <rect x="36" y="0" width="21" height="9" fill="#A47B55"/>
                <rect x="40" y="3" width="13" height="4" fill="#C99B6E"/>
                <rect x="6" y="28" width="80" height="42" fill="#8B6345"/>
                <rect x="10" y="32" width="72" height="34" fill="#E0CDA8"/>
                <g stroke="#A4825A" stroke-width="2">
                  <line x1="24" y1="32" x2="24" y2="66"/><line x1="38" y1="32" x2="38" y2="66"/>
                  <line x1="52" y1="32" x2="52" y2="66"/><line x1="66" y1="32" x2="66" y2="66"/>
                  <line x1="10" y1="44" x2="82" y2="44"/><line x1="10" y1="56" x2="82" y2="56"/>
                </g>
                <rect x="6" y="66" width="80" height="8" fill="#A47B55"/>
                <rect x="6" y="28" width="80" height="4" fill="#C99B6E"/>
              </svg>
            </div>

            <div v-for="g in grains" :key="g.id" class="grain"
              :style="{ left: px(g.x), top: px(g.y), '--gx': g.gx }"></div>
            <div v-for="h in hearts" :key="h.id" class="heart"
              :style="{ left: px(h.x), top: px(h.y), '--hx': h.hx, animationDelay: h.delay }">♥</div>
          </div>
        </div>
        <div class="litter-hint">{{ hint }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
  .litter-overlay {
    position: fixed; inset: 0; z-index: 96; display: flex; align-items: center; justify-content: center;
    padding: 12px; background: rgba(58, 44, 32, .58);
  }
  .litter-card {
    width: min(940px, 100%); max-height: calc(100dvh - 24px); overflow-y: auto;
    background: #FFFCF6; color: #5D493B; border: 2px solid #CBB49A; border-radius: 18px;
    box-shadow: 0 24px 60px rgba(45, 32, 20, .3);
    font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "Microsoft YaHei", sans-serif;
  }
  button {
    font: inherit; font-size: 12px; font-weight: 600; color: #5D493B;
    border: 1.5px solid #D9C6AD; background: #FAF3E3; border-radius: 10px;
    padding: 7px 11px; cursor: pointer; white-space: nowrap; touch-action: manipulation;
  }
  button.primary { background: #C78776; border-color: #B0705F; color: #fff; }
  button:disabled { opacity: .5; cursor: default; }
  .spacer { flex: 1 1 auto; }
  .litter-head {
    display: flex; align-items: center; gap: 12px; padding: 14px 18px;
    border-bottom: 1px solid #E6D8C5; background: #FFFDF8;
  }
  .litter-head b { font-size: 16px; }
  .litter-head small { display: block; font-size: 11px; color: #8A7561; margin-top: 2px; }
  .litter-controls {
    display: flex; flex-wrap: wrap; gap: 8px; align-items: center;
    padding: 10px 18px; border-bottom: 1px solid #E6D8C5; background: #FFFDF8;
  }
  .mood-chip { display: flex; align-items: center; gap: 7px; font-size: 12px; font-weight: 600; }
  .mood-chip .bar { width: 68px; height: 8px; border-radius: 5px; background: #E9E1D3; overflow: hidden; }
  .mood-chip .bar i { display: block; height: 100%; background: #99AE89; }
  .progress { font-size: 12px; font-weight: 600; color: #8A7561; }
  .litter-stage-shell { padding: 14px 18px 16px; background: #F6EFE1; }
  .litter-stage {
    position: relative; width: 100%; max-width: 680px; margin: 0 auto;
    aspect-ratio: 680 / 420; border: 2px solid #D9C6AD; border-radius: 12px;
    background: #E7CDA6; overflow: hidden; touch-action: none; user-select: none;
  }
  .litter-world { position: absolute; left: 0; top: 0; width: calc(var(--u) * 680); height: calc(var(--u) * 420); }
  .wall { position: absolute; left: 0; top: 0; width: calc(var(--u) * 680); height: calc(var(--u) * 252); background: #FAF3E3; }
  .wall::after { content: ''; position: absolute; inset: 0;
    background: repeating-linear-gradient(0deg, transparent 0 23px, rgba(190,168,135,.10) 23px 24px); }
  .skirting { position: absolute; left: 0; top: calc(var(--u) * 246); width: calc(var(--u) * 680); height: calc(var(--u) * 16);
    background: linear-gradient(to bottom, #c69969 0 30%, #a77950 30% 58%, #8b6345 58% 72%, #ae8056 72% 92%, #79583e 92%); }
  .floor { position: absolute; left: 0; top: calc(var(--u) * 262); width: calc(var(--u) * 680); height: calc(var(--u) * 158);
    background: repeating-linear-gradient(90deg, #E7CDA6 0 78px, #DCC097 78px 80px, #EBD4AF 80px 158px, #D3B48C 158px 160px); }
  .floor::after { content: ''; position: absolute; inset: 0;
    background: repeating-linear-gradient(0deg, transparent 0 51px, rgba(120,88,56,.12) 51px 53px); }
  .pixel { position: absolute; image-rendering: pixelated; display: block; }
  .tub { left: calc(var(--u) * 62); top: calc(var(--u) * 202); width: calc(var(--u) * 380); height: calc(var(--u) * 150); }
  .bin { left: calc(var(--u) * 540); top: calc(var(--u) * 212); width: calc(var(--u) * 116); height: calc(var(--u) * 132); }
  .bin.bump { animation: bump .28s ease; }
  @keyframes bump { 0%,100% { translate: 0 0 } 45% { translate: 0 calc(var(--u) * -6) } }
  .bin-open {
    position: absolute; left: calc(var(--u) * 528); top: calc(var(--u) * 194);
    width: calc(var(--u) * 134); height: calc(var(--u) * 104);
    border: calc(var(--u) * 3) dashed #5D8C6A; border-radius: calc(var(--u) * 12);
    background: rgba(157,187,163,.16); opacity: 0; transition: opacity .18s ease;
    pointer-events: none; z-index: 3;
  }
  .bin-open.show { opacity: 1; }
  .bin-label { position: absolute; left: 0; right: 0; top: calc(var(--u) * -24); text-align: center;
    font-size: max(10px, calc(var(--u) * 12)); font-weight: 600; color: #4E7557; white-space: nowrap; }
  .clump { position: absolute; width: calc(var(--u) * 44); height: calc(var(--u) * 40);
    transform: translate(-50%, -50%); filter: drop-shadow(0 calc(var(--u) * 3) 0 rgba(90,62,38,.18)); }
  .clump :deep(svg), .clump svg { width: 100%; height: 100%; display: block; }
  .clump.held { z-index: 9; filter: drop-shadow(0 calc(var(--u) * 8) calc(var(--u) * 4) rgba(90,62,38,.28)); }
  .clump.gone { display: none; }
  .scoop { position: absolute; width: calc(var(--u) * 92); height: calc(var(--u) * 78);
    transform: translate(-50%, -50%); cursor: grab; z-index: 8; touch-action: none; }
  .scoop svg { width: 100%; height: 100%; display: block; overflow: visible; }
  .scoop.shake { animation: shake .22s ease; }
  @keyframes shake { 0%,100% { rotate: 0deg } 30% { rotate: -7deg } 70% { rotate: 7deg } }
  .scoop.returning { transition: left .45s ease, top .45s ease; }
  /* 不给 left/top 加过渡：舞台 --u 初始化时坐标会整体变化，过渡期间猫会滑去压住小桶。 */
  .cat { position: absolute; z-index: 6; width: calc(var(--u) * 110); height: calc(var(--u) * 83);
    transition: filter .3s ease; }
  /* CatSprite 内部 svg 是固定 110px，必须压回容器尺寸，否则窄屏会溢出压到小桶上。 */
  .cat :deep(svg) { width: 100%; height: 100%; display: block; }
  .cat.hop { animation: hop .5s ease 2; }
  @keyframes hop { 0%,100% { translate: 0 0 } 45% { translate: 0 calc(var(--u) * -14) } }
  .bubble {
    position: absolute; z-index: 9; max-width: max(160px, calc(var(--u) * 220)); white-space: nowrap;
    background: #FFFCF6; border: 1.5px solid #CBB49A; border-radius: 12px;
    padding: 7px 11px; font-size: max(11px, calc(var(--u) * 13)); font-weight: 600;
    line-height: 1.5; box-shadow: 0 6px 14px rgba(103,75,47,.14); pointer-events: none;
  }
  .litter-hint { width: fit-content; max-width: 100%; margin: 10px auto 0; padding: 6px 14px;
    border-radius: 20px; background: #FFFCF6; border: 1px solid #D9C6AD;
    font-size: 12px; font-weight: 600; color: #6D5844; text-align: center; line-height: 1.6; }
  .grain { position: absolute; width: calc(var(--u) * 5); height: calc(var(--u) * 5);
    background: #C9A87A; z-index: 7; pointer-events: none; animation: fall .42s ease-in forwards; }
  @keyframes fall { from { opacity: 1; translate: 0 0 } to { opacity: 0; translate: var(--gx, 0px) calc(var(--u) * 34) } }
  .heart { position: absolute; z-index: 11; color: #C14B6E; font-size: max(14px, calc(var(--u) * 22));
    font-weight: 700; pointer-events: none; animation: floatup .9s ease-out forwards; }
  @keyframes floatup { from { opacity: 1; translate: 0 0 } to { opacity: 0; translate: var(--hx, 0px) calc(var(--u) * -48) } }
  @media (max-width: 560px) {
    .litter-overlay { padding: 0; }
    .litter-card { width: 100%; min-height: 100dvh; max-height: none; border: 0; border-radius: 0;
      display: flex; flex-direction: column; justify-content: center; }
    .litter-head { padding: 12px 14px; }
    .litter-controls { padding: 10px 14px; gap: 6px; }
    .litter-controls .spacer { flex-basis: 100%; height: 0; }
    .litter-stage-shell { padding: 10px 10px 12px; }
  }
</style>
