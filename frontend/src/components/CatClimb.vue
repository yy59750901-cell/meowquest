<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, useId } from 'vue'
import CatSprite from './CatSprite.vue'
import { CAT_CLIMB_CONFIG as WORLD, createCatClimbEngine } from '../cat-climb.js'

const props = defineProps({
  species: { type: String, default: 'black' },
  catName: { type: String, default: '小猫' },
  standalone: { type: Boolean, default: false },
})
const emit = defineEmits(['close'])
const titleId = useId()
const helpId = useId()
const dialog = ref(null)
const stage = ref(null)
const scene = ref(null)
const playfield = ref(null)
const startButton = ref(null)
const engine = createCatClimbEngine({ seed: 1 })
const state = shallowRef(engine.snapshot())
const viewport = shallowRef({ width: 360, height: 560, top: 0, left: 0 })
const stageSize = shallowRef({ width: 180, height: 280 })
const pointers = new Map()
const keys = new Set()
let mounted = false
let closing = false
let raf = null
let lastFrame = null
let previousFocus = null
let observer = null
let stageMeasured = false
let chosenSeed = 1

const screenY = (y) => WORLD.height - (y - state.value.cameraY)
const displayHeight = computed(() => Math.floor(state.value.height))
const difficultyCaption = computed(() => `每${state.value.difficultyEvery}层升档${state.value.difficultyCapped ? ' · 已到难度上限' : ''}`)
const catAnim = computed(() => state.value.jumps === 0 ? 'sit' : 'pounce')
const viewportStyle = computed(() => ({
  width: `${viewport.value.width}px`, height: `${viewport.value.height}px`,
  top: `${viewport.value.top}px`, left: `${viewport.value.left}px`,
  '--climb-viewport-height': `${viewport.value.height}px`,
}))
const stageStyle = computed(() => ({ width: `${stageSize.value.width}px`, height: `${stageSize.value.height}px` }))
const keyDirection = (event) => ({ ArrowLeft: -1, ArrowRight: 1, KeyA: -1, KeyD: 1 }[event.code]
  ?? { ArrowLeft: -1, ArrowRight: 1, a: -1, A: -1, d: 1, D: 1 }[event.key])

function clockRunning() {
  return mounted && !closing && !document.hidden && state.value.phase === 'running'
}
function stopFrames() {
  if (raf !== null) cancelAnimationFrame(raf)
  raf = null
  lastFrame = null
}
function syncClock(timestamp = performance.now()) {
  if (!clockRunning()) { lastFrame = null; return }
  if (!Number.isFinite(timestamp) || (lastFrame !== null && timestamp <= lastFrame)) return
  const dt = lastFrame === null ? 0 : (timestamp - lastFrame) / 1000
  lastFrame = timestamp
  state.value = engine.tick(dt)
  if (state.value.phase !== 'running') {
    stopFrames()
    clearInputs(false)
  }
}
function frame(timestamp) {
  raf = null
  syncClock(timestamp)
  if (clockRunning()) raf = requestAnimationFrame(frame)
}
function startFrames() {
  if (clockRunning() && raf === null) {
    lastFrame = performance.now()
    raf = requestAnimationFrame(frame)
  }
}
function send(event) {
  // 先把时间归入旧阶段和旧输入；不能用新输入补算上一帧。
  syncClock()
  state.value = engine.dispatch(event)
  if (!clockRunning()) stopFrames()
  else startFrames()
}
function syncDirection() {
  const directions = [...keys].map((key) => keyDirection({ code: key, key }))
    .concat([...pointers.values()].map((p) => p.direction))
  const left = directions.includes(-1)
  const right = directions.includes(1)
  send({ type: 'direction', direction: left === right ? 0 : left ? -1 : 1 })
}
function clearInputs(advance = true) {
  if (advance) syncClock()
  const captures = [...pointers.entries()]
  pointers.clear()
  keys.clear()
  state.value = engine.dispatch({ type: 'direction', direction: 0 })
  for (const [id, { element }] of captures) {
    if (element.hasPointerCapture?.(id)) {
      try { element.releasePointerCapture(id) } catch { /* 浏览器可能已先撤销捕获。 */ }
    }
  }
}
function pointerDown(event, direction) {
  syncClock()
  if (event.button !== 0 || closing || state.value.phase !== 'running') return
  event.preventDefault()
  const element = event.currentTarget
  pointers.set(event.pointerId, { direction, element })
  try { element.setPointerCapture(event.pointerId) } catch { pointers.delete(event.pointerId) }
  syncDirection()
}
function cancelPointer(event) {
  if (!pointers.has(event.pointerId)) return
  syncClock()
  const active = pointers.get(event.pointerId)
  if (!active) return
  const { element } = active
  pointers.delete(event.pointerId)
  if (element.hasPointerCapture?.(event.pointerId)) {
    try { element.releasePointerCapture(event.pointerId) } catch { /* 捕获取消与指针结束可同时发生。 */ }
  }
  syncDirection()
}
function pointerMove(event) {
  const active = pointers.get(event.pointerId)
  if (!active) return
  event.preventDefault()
  const r = active.element.getBoundingClientRect()
  if (event.clientX < r.left || event.clientX > r.right || event.clientY < r.top || event.clientY > r.bottom) cancelPointer(event)
}
// 兜底热区：整块游戏区左右半屏都能按住控制，孩子按不到两侧大键时照样能玩。
function playfieldPointerDown(event) {
  if (event.target?.closest?.('button, .climb-overlay')) return
  const r = playfield.value?.getBoundingClientRect()
  if (!r || !r.width) return
  pointerDown(event, event.clientX < r.left + r.width / 2 ? -1 : 1)
}
function keyboardTarget(target) {
  return target === stage.value || target?.classList?.contains('climb-left') || target?.classList?.contains('climb-right')
}
function keyboardDown(event) {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    requestClose()
    return
  }
  if (event.key === 'Tab') {
    clearInputs()
    const elements = [...dialog.value.querySelectorAll('button:not(:disabled), [tabindex="0"]')]
    const first = elements[0]
    const last = elements.at(-1)
    if (event.shiftKey && (document.activeElement === first || !elements.includes(document.activeElement))) {
      event.preventDefault()
      last?.focus({ preventScroll: true })
    } else if (!event.shiftKey && (document.activeElement === last || !elements.includes(document.activeElement))) {
      event.preventDefault()
      first?.focus({ preventScroll: true })
    }
    return
  }
  const direction = keyDirection(event)
  if (direction === undefined || !keyboardTarget(event.target)) return
  event.preventDefault()
  event.stopPropagation()
  syncClock()
  if (state.value.phase !== 'running' || event.repeat) return
  keys.add(event.code || event.key)
  syncDirection()
}
function keyboardUp(event) {
  const key = event.code || event.key
  if (!keys.has(key)) return
  event.preventDefault()
  event.stopPropagation()
  syncClock()
  keys.delete(key)
  syncDirection()
}
function focusOut(event) {
  if (keyboardTarget(event.target)) {
    syncClock()
    keys.clear()
    syncDirection()
  }
}
function suspend() {
  if (!mounted || closing) return
  clearInputs()
  send({ type: 'pause' })
  stopFrames()
}
function visibilityChanged() { if (document.hidden) suspend() }
function togglePause() {
  if (state.value.phase === 'paused') {
    if (document.hidden || !document.hasFocus()) return
    clearInputs(false)
    send({ type: 'resume' })
    if (state.value.phase === 'running') stage.value.focus({ preventScroll: true })
  } else suspend()
}
function start() {
  if (closing || document.hidden) return
  clearInputs()
  send({ type: 'start' })
  if (state.value.phase === 'running') stage.value.focus({ preventScroll: true })
}
async function restart(mode = state.value.mode) {
  clearInputs()
  stopFrames()
  // 奇数步进在 uint32 周期内不重复种子；固定路线重开不推进，也不保存历史。
  if (mode === 'random') chosenSeed = (chosenSeed + 0x9e3779b9) >>> 0
  state.value = engine.dispatch({ type: 'reset', mode, seed: chosenSeed })
  // 重新开始仍等待开始按钮；不把退出/重开按钮的方向键转交游戏。
  await nextTick()
  if (mounted && !closing) startButton.value?.focus({ preventScroll: true })
}
function requestClose() {
  if (closing) return
  clearInputs()
  closing = true
  state.value = engine.dispatch({ type: 'pause' })
  stopFrames()
  emit('close')
}
function retainFocus(event) {
  if (!closing && dialog.value && !dialog.value.contains(event.target)) stage.value?.focus({ preventScroll: true })
}
function fitStage() {
  if (!scene.value) return
  const r = scene.value.getBoundingClientRect()
  const width = Math.max(0, Math.min(r.width, r.height * WORLD.width / WORLD.height))
  const height = width * WORLD.height / WORLD.width
  if (Math.abs(stageSize.value.width - width) < 0.1 && Math.abs(stageSize.value.height - height) < 0.1) {
    stageMeasured = true
    return
  }
  if (stageMeasured) suspend()
  stageMeasured = true
  stageSize.value = { width, height }
}
function updateViewport() {
  const v = window.visualViewport
  viewport.value = { width: v?.width ?? window.innerWidth, height: v?.height ?? window.innerHeight,
    top: v?.offsetTop ?? 0, left: v?.offsetLeft ?? 0 }
  nextTick(fitStage)
}
function resize() {
  suspend()
  updateViewport()
}
onMounted(() => {
  mounted = true
  previousFocus = document.activeElement
  chosenSeed = Math.floor(Math.random() * 4294967296)
  state.value = engine.dispatch({ type: 'reset', seed: chosenSeed })
  updateViewport()
  startButton.value?.focus({ preventScroll: true })
  window.addEventListener('blur', suspend)
  window.addEventListener('resize', resize)
  window.addEventListener('orientationchange', resize)
  window.addEventListener('keyup', keyboardUp)
  window.visualViewport?.addEventListener('resize', resize)
  window.visualViewport?.addEventListener('scroll', resize)
  document.addEventListener('visibilitychange', visibilityChanged)
  document.addEventListener('focusin', retainFocus)
  observer = new ResizeObserver(fitStage)
  observer.observe(scene.value)
  if (document.hidden) suspend()
})
onBeforeUnmount(() => {
  clearInputs()
  mounted = false
  stopFrames()
  observer?.disconnect()
  window.removeEventListener('blur', suspend)
  window.removeEventListener('resize', resize)
  window.removeEventListener('orientationchange', resize)
  window.removeEventListener('keyup', keyboardUp)
  window.visualViewport?.removeEventListener('resize', resize)
  window.visualViewport?.removeEventListener('scroll', resize)
  document.removeEventListener('visibilitychange', visibilityChanged)
  document.removeEventListener('focusin', retainFocus)
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})
</script>

<template>
  <section ref="dialog" class="cat-climb" :class="{ 'is-standalone': standalone }" :style="viewportStyle"
    :data-phase="state.phase" :data-mode="state.mode" :data-difficulty="state.difficultyStage"
    :data-floor="state.floor" :data-difficulty-level="state.difficultyLevel" :data-difficulty-every="state.difficultyEvery"
    :data-jumps="state.jumps" :data-landing-id="state.lastLanding?.id ?? 0" :data-seed="state.seed" role="dialog" aria-modal="true"
    :aria-labelledby="titleId" :aria-describedby="helpId" @keydown="keyboardDown" @focusout="focusOut"
    @pointerdown.stop @pointermove.stop @pointerup.stop @click.stop @wheel.stop @contextmenu.prevent
    @selectstart.prevent @dragstart.prevent>
    <header class="climb-header">
      <h2 :id="titleId" class="climb-title">{{ catName }}的爬高</h2>
      <div class="climb-hud">
        <p class="climb-height">第 <strong>{{ state.floor }}</strong> 层</p>
        <span class="climb-difficulty" :data-difficulty="state.difficultyStage" :data-level="state.difficultyLevel">难度 {{ state.difficultyLevel }}</span>
        <small class="climb-caption">{{ difficultyCaption }}</small>
      </div>
      <span :id="helpId" class="climb-help sr-only">按住左右，松手就停。条纹台会慢移。</span>
      <div class="climb-header-actions">
        <button v-if="state.phase === 'running'" type="button" class="climb-pause" @click="togglePause"><span class="climb-label">暂停</span></button>
      </div>
    </header>
    <div ref="playfield" class="climb-playfield"
      @pointerdown="playfieldPointerDown" @pointermove="pointerMove" @pointerup="cancelPointer" @pointercancel="cancelPointer">
      <div ref="scene" class="climb-scene">
        <div ref="stage" class="climb-stage" :style="stageStyle" tabindex="0"
          aria-label="爬高场地，按方向键左右或 A、D 控制小猫" data-world="360x560">
          <svg viewBox="0 0 360 560" preserveAspectRatio="xMidYMid meet" aria-hidden="true">
            <defs>
              <pattern :id="`${titleId}-stripe`" width="12" height="8" patternUnits="userSpaceOnUse">
                <rect width="12" height="8" fill="#84a99a" />
                <path d="M0 8L8 0M8 8L16 0" stroke="#e3efcf" stroke-width="3" />
              </pattern>
            </defs>
            <g shape-rendering="crispEdges">
              <rect width="360" height="560" fill="#fff2df" />
              <path d="M24 0V560M336 0V560" stroke="#efdbbd" stroke-width="12" />
              <path d="M28 40h16M316 160h16M28 310h16M316 440h16" stroke="#e1c399" stroke-width="4" />
              <g v-for="platform in state.platforms" :key="platform.id" class="climb-platform"
                :data-id="platform.id" :data-type="platform.type" :data-x="platform.x" :data-y="platform.y"
                :data-world-x="platform.x" :data-world-y="platform.y" :data-width="platform.width"
                :data-index="platform.index" :data-stage="platform.stage">
                <rect :x="platform.x - platform.width / 2" :y="screenY(platform.y)" :width="platform.width" height="10"
                  :fill="platform.launchpad ? '#edbbad' : platform.type === 'moving' ? `url(#${titleId}-stripe)` : '#d6ad7d'" />
                <rect :x="platform.x - platform.width / 2" :y="screenY(platform.y) + 10" :width="platform.width" height="4"
                  :fill="platform.type === 'moving' ? '#638c7c' : '#b88d63'" />
              </g>
            </g>
            <g class="climb-cat" :data-x="state.cat.x" :data-y="state.cat.y" :data-vy="state.cat.vy"
              :data-height="displayHeight" :height="displayHeight" :data-anim="catAnim"
              :transform="`translate(${state.cat.x} ${screenY(state.cat.y)})`">
              <g :transform="`scale(${state.cat.facing === 1 ? -1 : 1} 1)`">
                <!-- pounce 最后一行透明，实际脚底在第十一行的下缘。 -->
                <CatSprite :key="catAnim" x="-24" :y="catAnim === 'pounce' ? -33 : -36" :size="48" :species="species"
                  :anim="catAnim" :paused="state.phase !== 'running'" />
              </g>
            </g>
          </svg>
        </div>
      </div>
      <template v-if="state.phase === 'running'">
        <button type="button" class="climb-dir climb-left" aria-label="按住向左"
          @pointerdown.stop="pointerDown($event, -1)" @pointermove="pointerMove" @pointerup="cancelPointer"
          @pointercancel="cancelPointer" @lostpointercapture="cancelPointer" @contextmenu.prevent>
          <svg class="climb-arrow" viewBox="0 0 48 48" aria-hidden="true"><path d="M30 10 L14 24 L30 38 Z" /></svg>
          <span class="climb-label">向左</span>
        </button>
        <button type="button" class="climb-dir climb-right" aria-label="按住向右"
          @pointerdown.stop="pointerDown($event, 1)" @pointermove="pointerMove" @pointerup="cancelPointer"
          @pointercancel="cancelPointer" @lostpointercapture="cancelPointer" @contextmenu.prevent>
          <svg class="climb-arrow" viewBox="0 0 48 48" aria-hidden="true"><path d="M18 10 L34 24 L18 38 Z" /></svg>
          <span class="climb-label">向右</span>
        </button>
      </template>
      <div v-else-if="state.phase === 'ready'" class="climb-overlay climb-ready" role="group" aria-label="准备开始">
        <div class="climb-overlay-card">
          <strong class="climb-overlay-title">准备好就出发</strong>
          <p class="climb-help">按住左右，松手就停。条纹台会慢移。<br><span>仅本次试玩，不产生星星。</span></p>
          <button ref="startButton" type="button" class="climb-start" @click="start"><span class="climb-label">开始爬高</span></button>
          <div class="climb-modes" role="group" aria-label="选择爬高路线">
            <button type="button" :aria-pressed="state.mode === 'random'" @click="restart('random')"><span class="climb-label">随机爬高</span></button>
            <button type="button" :aria-pressed="state.mode === 'fixed'" @click="restart('fixed')"><span class="climb-label">固定路线</span></button>
          </div>
          <button type="button" class="climb-close" @click="requestClose"><span class="climb-label">退出</span></button>
        </div>
      </div>
      <div v-else-if="state.phase === 'paused'" class="climb-overlay climb-stage-message" role="status">
        <div class="climb-overlay-card">
          <strong class="climb-overlay-title">休息一下</strong>
          <button type="button" class="climb-pause" @click="togglePause"><span class="climb-label">继续玩</span></button>
          <button type="button" class="climb-restart" @click="restart()"><span class="climb-label">重开</span></button>
          <button type="button" class="climb-close" @click="requestClose"><span class="climb-label">退出</span></button>
        </div>
      </div>
      <div v-else-if="state.phase === 'ended'" class="climb-overlay climb-result" role="status" aria-live="polite">
        <div class="climb-overlay-card">
          <svg viewBox="0 0 96 64" class="climb-cushion" aria-hidden="true">
            <rect x="6" y="48" width="84" height="12" fill="#edbbad" />
            <rect x="12" y="44" width="72" height="8" fill="#f7d4bf" />
            <CatSprite x="24" y="8" :size="48" :species="species" anim="sit" paused />
          </svg>
          <strong>软垫接住啦</strong>
          <span>没接住平台，本次到第 {{ state.floor }} 层</span>
          <button type="button" class="climb-restart" @click="restart()"><span class="climb-label">再来一次</span></button>
          <button type="button" class="climb-close" @click="requestClose"><span class="climb-label">退出</span></button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.cat-climb {
  position: fixed; inset: 0; z-index: 3000; display: flex; flex-direction: column;
  padding: 0; box-sizing: border-box; background: #f6ead8;
  color: #624c36; font-family: inherit; overscroll-behavior: contain;
  user-select: none; -webkit-user-select: none; -webkit-touch-callout: none;
  -webkit-tap-highlight-color: transparent; touch-action: none;
}
.cat-climb.is-standalone { background: #f6ead8; }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
button { min-height: 44px; min-width: 44px; border: 2px solid #c6a47e; border-radius: 8px; padding: 8px 12px;
  background: #ffecd5; color: inherit; font: inherit; font-weight: 700; font-size: 13px; cursor: pointer;
  user-select: none; -webkit-user-select: none; -webkit-touch-callout: none;
  -webkit-tap-highlight-color: transparent; touch-action: manipulation; }
button:disabled { opacity: .55; cursor: default; }
button:focus-visible { outline: 3px solid #527d66; outline-offset: 2px; }
.climb-stage:focus-visible { outline: 2px solid #d9c7a8; outline-offset: -2px; }
.climb-label { pointer-events: none; }
/* 高度固定：暂停按钮出现不能改变 header 高度，否则 scene 变化会触发 fitStage 暂停游戏。 */
.climb-header { flex: 0 0 52px; height: 52px; display: flex; align-items: center; gap: 12px;
  padding: 6px clamp(12px, 2vw, 24px); padding-right: 96px; box-sizing: border-box; background: #fffaf0;
  border-bottom: 2px solid #c6a47e; overflow: hidden; }
.climb-title { margin: 0; font-size: 16px; line-height: 1.3; white-space: nowrap; flex-shrink: 0; }
.climb-hud { display: flex; align-items: baseline; gap: 12px; flex-wrap: wrap; min-width: 0; }
.climb-height { margin: 0; font-size: 13px; white-space: nowrap; }
.climb-height strong { font-size: 20px; color: #906036; font-variant-numeric: tabular-nums; }
.climb-difficulty { font-size: 12px; color: #6b8060; white-space: nowrap; }
.climb-caption { font-size: 11px; line-height: 14px; color: #8d765e; white-space: nowrap; }
.climb-header-actions { margin-left: auto; flex-shrink: 0; }
.climb-header-actions button { min-height: 36px; padding: 4px 12px; }
.climb-playfield { position: relative; flex: 1 1 auto; min-height: 0; display: grid; place-items: center; }
.climb-scene { display: grid; place-items: center; min-width: 0; min-height: 0; overflow: hidden;
  width: calc(100% - 2 * clamp(116px, 20vw, 190px)); height: 100%; }
.climb-stage { position: relative; overflow: hidden; background: #fff2df; border-radius: 3px; touch-action: none; }
.climb-stage > svg { display: block; width: 100%; height: 100%; }
.climb-dir { position: absolute; z-index: 3; bottom: clamp(18px, 10vh, 76px);
  width: clamp(96px, 14vw, 132px); height: clamp(96px, 14vw, 132px);
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px;
  border-radius: 28px; background: #eaf3de; border-color: #639922; color: #27500a; touch-action: none; }
.climb-left { left: clamp(12px, 3vw, 48px); }
.climb-right { right: clamp(12px, 3vw, 48px); }
.climb-dir:active { background: #c0dd97; }
.climb-arrow { width: 40px; height: 40px; fill: #3b6d11; pointer-events: none; }
.climb-dir .climb-label { font-size: 14px; }
.climb-overlay { position: absolute; inset: 0; z-index: 4; display: grid; place-items: center;
  padding: 16px; box-sizing: border-box; background: rgb(246 234 216 / 94%); }
.climb-overlay-card { width: min(320px, 100%); display: flex; flex-direction: column; align-items: center; gap: 10px;
  padding: 18px; box-sizing: border-box; border: 2px solid #c6a47e; border-radius: 14px;
  background: #fffaf0; text-align: center; }
.climb-overlay-title { font-size: 18px; }
.climb-help { margin: 0; font-size: 12px; line-height: 18px; text-align: center; }
.climb-help span { color: #8d765e; }
.climb-start { width: 100%; min-height: 52px; font-size: 16px; background: #e9efdf; border-color: #8ba286; }
.climb-modes { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; width: 100%; }
.climb-modes button { padding-inline: 4px; background: #fff7e9; font-size: 12px; }
.climb-modes button[aria-pressed='true'] { background: #e9efdf; border-color: #8ba286; }
.climb-pause, .climb-close, .climb-restart { width: 100%; background: #fff7e9; }
.climb-cushion { width: 96px; height: 64px; flex-shrink: 0; }
.climb-result strong { font-size: 17px; }
.climb-result .climb-overlay-card > span { font-size: 13px; line-height: 1.5; }
@media (orientation: portrait) and (max-width: 600px) {
  .climb-scene { width: 100%; height: calc(100% - 124px); align-self: start; }
  .climb-dir { bottom: 12px; width: min(42vw, 132px); height: 100px; border-radius: 22px; }
  .climb-left { left: 14px; }
  .climb-right { right: 14px; }
  .climb-header { flex-basis: 48px; height: 48px; gap: 6px; padding-left: 10px; padding-right: 70px; }
  .climb-title { font-size: 13px; }
  .climb-hud { flex-wrap: nowrap; gap: 6px; }
  .climb-difficulty { font-size: 11px; }
  .climb-caption { display: none; }
}
@media (orientation: landscape) and (max-height: 520px) {
  .climb-header { flex-basis: 44px; height: 44px; padding-block: 4px; }
  .climb-dir { bottom: 12px; width: 96px; height: 96px; border-radius: 22px; }
  .climb-arrow { width: 32px; height: 32px; }
  .climb-overlay-card { padding: 12px; gap: 6px; }
  .climb-cushion { width: 64px; height: 44px; }
  .climb-caption { display: none; }
}
</style>
