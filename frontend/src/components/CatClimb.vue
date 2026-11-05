<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, useId } from 'vue'
import CatSprite from './CatSprite.vue'
import { CAT_CLIMB_CONFIG as WORLD, CAT_CLIMB_THEME_LABELS, CAT_CLIMB_SURFACES, createCatClimbEngine } from '../cat-climb.js'

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
const catAnim = computed(() => state.value.cat.grounded ? 'sit' : 'pounce')
const themeLabel = computed(() => CAT_CLIMB_THEME_LABELS[state.value.theme] || '小台阶')
const surfaceLabel = computed(() => CAT_CLIMB_SURFACES[state.value.cat.surface]?.label || '普通地面')
const surfaceShort = computed(() => CAT_CLIMB_SURFACES[state.value.cat.surface]?.short || '普通')
const surfaceArt = (platform) => ({
  normal: { fill: '#d6ad7d', edge: '#b88d63' },
  ice: { fill: '#b5d4f4', edge: '#378add' },
  sticky: { fill: '#fac775', edge: '#ba7517' },
  belt: { fill: '#85b7eb', edge: '#185fa5' },
}[platform.surface] || { fill: '#d6ad7d', edge: '#b88d63' })
// 装饰动效统一由 simTime 相位驱动：暂停/隐藏自动冻结，且绝不改 p.x / p.y 碰撞坐标。
const reducedMotion = ref(false)
let motionQuery = null
const motionPhase = computed(() => reducedMotion.value ? 0 : state.value.simTime)
function onMotionChange(event) { reducedMotion.value = !!event.matches }
const iceShine = (platform, offset, k) => {
  const x = platform.x + offset - platform.width / 2 + k * platform.width / 5
  const y = screenY(platform.y)
  return `M ${x} ${y} L ${x + 9} ${y + 10}`
}
const iceGlint = (platform, offset) => {
  const span = platform.width + 24
  const x = (motionPhase.value * 42) % span - 12
  const left = platform.x + offset - platform.width / 2 + x
  const y = screenY(platform.y)
  return `M ${left} ${y + 1} L ${left + 10} ${y + 9}`
}
const stickyDot = (platform, offset, k) => {
  const wave = Math.sin(motionPhase.value * 4 + k * 1.7)
  return { cx: platform.x + offset - platform.width / 2 + k * platform.width / 4,
    cy: screenY(platform.y) + 5, r: 2 + 0.8 * wave, opacity: 0.65 + 0.3 * wave }
}
// 箭头朝向与滚动方向一致，按 20px 步长无缝铺满整个平台宽度。
const beltArrows = (platform, offset) => {
  const step = 20
  const dir = platform.beltDir >= 0 ? 1 : -1
  const shift = dir * ((motionPhase.value * 38) % step)
  const y = screenY(platform.y)
  const arrows = []
  for (let k = -2; k <= Math.ceil(platform.width / step) + 3; k++) {
    const local = k * step + shift
    if (local < 4 || local > platform.width - 4) continue
    const cx = platform.x + offset - platform.width / 2 + local
    arrows.push({ key: `${platform.id}-${offset}-${k}`,
      d: dir > 0
        ? `M ${cx - 4} ${y + 1} L ${cx + 4} ${y + 5} L ${cx - 4} ${y + 9} Z`
        : `M ${cx + 4} ${y + 1} L ${cx - 4} ${y + 5} L ${cx + 4} ${y + 9} Z` })
  }
  return arrows
}
const floatY = (platform) => (platform.type === 'moving' || platform.launchpad)
  ? 0 : Math.sin(motionPhase.value * 2.2 + platform.id * 0.9) * 1.2
const squash = (platform) => {
  if (reducedMotion.value) return 1
  const hit = state.value.lastLanding
  if (!hit || hit.id !== platform.id) return 1
  const elapsed = state.value.simTime - hit.time
  if (elapsed < 0 || elapsed > 0.18) return 1
  return 1 - 0.26 * (1 - elapsed / 0.18)
}
const platformMotion = (platform) => {
  const y = screenY(platform.y)
  return `translate(0 ${y + floatY(platform)}) scale(1 ${squash(platform)}) translate(0 ${-y})`
}
const movingHint = (platform, offset, side) => {
  const cx = platform.x + offset + side * (platform.width / 2 - 6)
  const y = screenY(platform.y) + 5
  return side < 0
    ? `M ${cx + 3} ${y - 3} L ${cx - 3} ${y} L ${cx + 3} ${y + 3} Z`
    : `M ${cx - 3} ${y - 3} L ${cx + 3} ${y} L ${cx - 3} ${y + 3} Z`
}
const wrapOffsets = [-WORLD.width, 0, WORLD.width]
const seamX = computed(() => (state.value.cat.x + WORLD.width / 2) % WORLD.width)
const parallaxX = computed(() => (state.value.cat.x * 0.18) % 60)
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
// 按住期间方向只由手指当前 x 决定：以「按下元素的中线」为分界，左半向左、右半向右。
// 分界线随按下位置固定并延伸到整块场地，手指滑出控件也不会突然反向，不用抬手再按。
function directionAt(element, clientX) {
  const r = element.getBoundingClientRect()
  return clientX < r.left + r.width / 2 ? -1 : 1
}
function pointerMove(event) {
  const active = pointers.get(event.pointerId)
  if (!active) return
  event.preventDefault()
  const field = playfield.value?.getBoundingClientRect()
  // 只有彻底滑出游戏场地才当作松手；在控件与舞台之间移动始终实时跟随。
  if (field && field.width && (event.clientX < field.left || event.clientX > field.right
    || event.clientY < field.top || event.clientY > field.bottom)) {
    cancelPointer(event)
    return
  }
  const direction = directionAt(active.element, event.clientX)
  if (direction !== active.direction) {
    active.direction = direction
    syncDirection()
  }
}
// 兜底热区：舞台左右半屏都能按住控制，孩子按不到方向控件时照样能玩。
function playfieldPointerDown(event) {
  if (event.target?.closest?.('button, .climb-overlay, .climb-controls')) return
  const field = playfield.value
  if (!field?.getBoundingClientRect().width) return
  pointerDown(event, directionAt(field, event.clientX))
}
// 左下双向控件与兜底热区共用同一套实时判向逻辑。
function steerDown(event) {
  pointerDown(event, directionAt(event.currentTarget, event.clientX))
}
function jumpDown(event) {
  event.preventDefault()
  if (closing || state.value.phase !== 'running') return
  syncClock()
  send({ type: 'jump' })
}
function keyboardTarget(target) {
  return target === stage.value || ['climb-left', 'climb-right', 'climb-steer', 'climb-jump']
    .some((name) => target?.classList?.contains(name))
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
  if (event.code === 'Space' || [' ', 'ArrowUp', 'KeyW', 'w', 'W'].includes(event.key)) {
    if (keyboardTarget(event.target)) {
      event.preventDefault()
      event.stopPropagation()
      send({ type: 'jump' })
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
// iOS Safari 忽略 viewport 里的 user-scalable / maximum-scale，双指操作（方向＋跳跃）
// 会被识别成整页捏合缩放。只在游戏全屏期间拦截，退出即移除，不影响主应用滚动。
let removeZoomGuard = null
function installZoomGuard() {
  const blockGesture = (event) => event.preventDefault()
  const blockMultiTouch = (event) => {
    if (event.touches?.length > 1) event.preventDefault()
  }
  const guards = [
    ['gesturestart', blockGesture], ['gesturechange', blockGesture], ['touchmove', blockMultiTouch],
  ]
  for (const [type, fn] of guards) document.addEventListener(type, fn, { passive: false })
  return () => { for (const [type, fn] of guards) document.removeEventListener(type, fn) }
}
let lastScale = 1
function updateViewport() {
  const v = window.visualViewport
  // 页面已被捏合缩放时固定按布局视口尺寸，游戏容器不跟着缩小。
  const scaled = typeof v?.scale === 'number' && Math.abs(v.scale - 1) > 0.01
  viewport.value = scaled
    ? { width: window.innerWidth, height: window.innerHeight, top: 0, left: 0 }
    : { width: v?.width ?? window.innerWidth, height: v?.height ?? window.innerHeight,
        top: v?.offsetTop ?? 0, left: v?.offsetLeft ?? 0 }
  nextTick(fitStage)
}
function resize() {
  const scale = window.visualViewport?.scale ?? 1
  const zooming = Math.abs(scale - 1) > 0.01 || Math.abs(lastScale - 1) > 0.01
  lastScale = scale
  // 缩放引起的 visualViewport 变化不暂停游戏；真实窗口尺寸变化才走原有暂停逻辑。
  if (zooming) { updateViewport(); return }
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
  motionQuery = window.matchMedia?.('(prefers-reduced-motion: reduce)') || null
  reducedMotion.value = !!motionQuery?.matches
  motionQuery?.addEventListener?.('change', onMotionChange)
  window.addEventListener('blur', suspend)
  window.addEventListener('resize', resize)
  window.addEventListener('orientationchange', resize)
  window.addEventListener('keyup', keyboardUp)
  window.visualViewport?.addEventListener('resize', resize)
  window.visualViewport?.addEventListener('scroll', resize)
  document.addEventListener('visibilitychange', visibilityChanged)
  document.addEventListener('focusin', retainFocus)
  removeZoomGuard = installZoomGuard()
  observer = new ResizeObserver(fitStage)
  observer.observe(scene.value)
  if (document.hidden) suspend()
})
onBeforeUnmount(() => {
  clearInputs()
  mounted = false
  stopFrames()
  observer?.disconnect()
  motionQuery?.removeEventListener?.('change', onMotionChange)
  window.removeEventListener('blur', suspend)
  window.removeEventListener('resize', resize)
  window.removeEventListener('orientationchange', resize)
  window.removeEventListener('keyup', keyboardUp)
  window.visualViewport?.removeEventListener('resize', resize)
  window.visualViewport?.removeEventListener('scroll', resize)
  document.removeEventListener('visibilitychange', visibilityChanged)
  document.removeEventListener('focusin', retainFocus)
  removeZoomGuard?.()
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
        <span class="climb-scroll" :data-scroll="state.scrollSpeed">↑ {{ state.scrollSpeed }}px/s</span>
        <span class="climb-surface" :data-surface="state.cat.surface">{{ surfaceShort }}</span>
        <small class="climb-caption">{{ themeLabel }} · {{ difficultyCaption }}</small>
      </div>
      <span :id="helpId" class="climb-help sr-only">左下控制左右，右下点按跳跃；地面不断上升，别被甩出屏幕。</span>
      <div class="climb-header-actions">
        <button v-if="state.phase === 'running'" type="button" class="climb-pause" @click="togglePause"><span class="climb-label">暂停</span></button>
      </div>
    </header>
    <div ref="playfield" class="climb-playfield"
      @pointerdown="playfieldPointerDown" @pointermove="pointerMove" @pointerup="cancelPointer" @pointercancel="cancelPointer">
      <div ref="scene" class="climb-scene">
        <div ref="stage" class="climb-stage" :style="stageStyle" tabindex="0"
          aria-label="爬高场地，方向键左右或 A、D 移动，空格或上箭头跳跃" data-world="360x560" data-cylinder="true">
          <svg viewBox="0 0 360 560" preserveAspectRatio="xMidYMid meet" aria-hidden="true">
            <g shape-rendering="crispEdges">
              <rect width="360" height="560" fill="#fff2df" />
              <g :transform="`translate(${-parallaxX} 0)`">
                <rect v-for="i in 8" :key="`bg-${i}`" :x="(i - 1) * 60" y="0" width="2" height="560" fill="#efdbbd" opacity="0.6" />
                <rect x="360" y="0" width="2" height="560" fill="#efdbbd" opacity="0.6" />
              </g>
              <line :x1="seamX" y1="0" :x2="seamX" y2="560" stroke="#d9b98a" stroke-width="3" stroke-dasharray="10 8" />
              <g v-for="platform in state.platforms" :key="platform.id" class="climb-platform"
                :data-id="platform.id" :data-type="platform.type" :data-x="platform.x" :data-y="platform.y"
                :data-world-x="platform.x" :data-world-y="platform.y" :data-width="platform.width"
                :data-index="platform.index" :data-stage="platform.stage" :data-theme="platform.theme"
                :data-surface="platform.surface" :data-belt="platform.beltDir">
                <g v-for="offset in wrapOffsets" :key="`${platform.id}-${offset}`"
                  :data-copy="offset" :data-surface="platform.surface" :data-motion="platform.type"
                  :transform="platformMotion(platform)">
                  <rect :x="platform.x + offset - platform.width / 2" :y="screenY(platform.y)" :width="platform.width" height="10"
                    :fill="platform.launchpad ? '#edbbad' : surfaceArt(platform).fill" />
                  <rect :x="platform.x + offset - platform.width / 2" :y="screenY(platform.y) + 10" :width="platform.width" height="4"
                    :fill="platform.launchpad ? '#c78776' : surfaceArt(platform).edge" />
                  <g v-if="platform.surface === 'ice' && !platform.launchpad">
                    <path v-for="k in 4" :key="`ice-${k}`" :d="iceShine(platform, offset, k)"
                      stroke="#ffffff" stroke-width="1.5" opacity="0.4" />
                    <path :d="iceGlint(platform, offset)" stroke="#ffffff" stroke-width="2.4" opacity="0.95" />
                  </g>
                  <g v-else-if="platform.surface === 'sticky' && !platform.launchpad">
                    <circle v-for="k in 3" :key="`sticky-${k}`" fill="#854f0b"
                      :cx="stickyDot(platform, offset, k).cx" :cy="stickyDot(platform, offset, k).cy"
                      :r="stickyDot(platform, offset, k).r" :opacity="stickyDot(platform, offset, k).opacity" />
                  </g>
                  <g v-else-if="platform.surface === 'belt' && !platform.launchpad">
                    <path v-for="arrow in beltArrows(platform, offset)" :key="arrow.key" :d="arrow.d"
                      fill="#ffffff" opacity="0.95" />
                  </g>
                  <g v-if="platform.type === 'moving' && !platform.launchpad" opacity="0.55">
                    <path :d="movingHint(platform, offset, -1)" fill="#ffffff" />
                    <path :d="movingHint(platform, offset, 1)" fill="#ffffff" />
                  </g>
                </g>
              </g>
              <line x1="0" y1="552" x2="360" y2="552" stroke="#c78776" stroke-width="3" stroke-dasharray="8 6" />
            </g>
            <g class="climb-cat" :data-x="state.cat.x" :data-y="state.cat.y" :data-vy="state.cat.vy"
              :data-height="displayHeight" :height="displayHeight" :data-anim="catAnim" :data-grounded="state.cat.grounded">
              <g v-for="offset in wrapOffsets" :key="`cat-${offset}`"
                :transform="`translate(${state.cat.x + offset} ${screenY(state.cat.y)})`">
                <g :transform="`scale(${state.cat.facing === 1 ? -1 : 1} 1)`">
                  <!-- pounce 最后一行透明，实际脚底在第十一行的下缘。 -->
                  <CatSprite :key="catAnim" x="-24" :y="catAnim === 'pounce' ? -33 : -36" :size="48" :species="species"
                    :anim="catAnim" :paused="state.phase !== 'running'" />
                </g>
              </g>
            </g>
          </svg>
        </div>
      </div>
      <div v-if="state.phase === 'running'" class="climb-controls">
        <div class="climb-steer" role="group" aria-label="左右移动"
          @pointerdown.stop="steerDown" @pointermove.stop="pointerMove" @pointerup="cancelPointer"
          @pointercancel="cancelPointer" @lostpointercapture="cancelPointer" @contextmenu.prevent>
          <svg class="climb-arrow" viewBox="0 0 48 48" aria-hidden="true"><path d="M30 10 L14 24 L30 38 Z" /></svg>
          <span class="climb-steer-divider"></span>
          <svg class="climb-arrow" viewBox="0 0 48 48" aria-hidden="true"><path d="M18 10 L34 24 L18 38 Z" /></svg>
          <span class="climb-label">方向</span>
        </div>
        <button type="button" class="climb-jump" aria-label="点按跳跃" @pointerdown.stop="jumpDown" @contextmenu.prevent>
          <svg class="climb-arrow" viewBox="0 0 48 48" aria-hidden="true"><path d="M24 8 L40 30 L8 30 Z" /></svg>
          <span class="climb-label">跳跃</span>
        </button>
      </div>
      <div v-else-if="state.phase === 'ready'" class="climb-overlay climb-ready" role="group" aria-label="准备开始">
        <div class="climb-overlay-card">
          <strong class="climb-overlay-title">别被地面追上</strong>
          <p class="climb-help">左下按住控制左右，右下点按跳跃。<br><span>地面一直上升，要不停往上跳；左右走到头会穿到另一边。</span></p>
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
          <span>没跟上上升的地面，本次到第 {{ state.floor }} 层</span>
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
  -webkit-tap-highlight-color: transparent; touch-action: none; }
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
.climb-playfield { position: relative; flex: 1 1 auto; min-height: 0; display: grid; place-items: center; touch-action: none; }
.climb-scene { display: grid; place-items: center; min-width: 0; min-height: 0; overflow: hidden;
  width: calc(100% - 2 * clamp(116px, 20vw, 190px)); height: 100%; }
.climb-stage { position: relative; overflow: hidden; background: #fff2df; border-radius: 3px; touch-action: none; }
.climb-stage > svg { display: block; width: 100%; height: 100%; }
.climb-scroll { font-size: 12px; color: #906036; white-space: nowrap; font-variant-numeric: tabular-nums; }
.climb-surface { font-size: 11px; padding: 1px 6px; border-radius: 6px; background: #f1efe8; color: #5f5e5a; white-space: nowrap; }
.climb-surface[data-surface='ice'] { background: #e6f1fb; color: #185fa5; }
.climb-surface[data-surface='sticky'] { background: #faeeda; color: #854f0b; }
.climb-surface[data-surface='belt'] { background: #e6f1fb; color: #0c447c; }
.climb-controls { position: absolute; inset: 0; z-index: 3; pointer-events: none; }
.climb-steer, .climb-jump { position: absolute; z-index: 3; pointer-events: auto;
  bottom: clamp(18px, 10vh, 76px); width: clamp(96px, 14vw, 132px); height: clamp(96px, 14vw, 132px);
  box-sizing: border-box; border: 2px solid #639922; border-radius: 28px;
  background: #eaf3de; color: #27500a; font: inherit; font-weight: 700; cursor: pointer;
  touch-action: none; user-select: none; -webkit-user-select: none; -webkit-touch-callout: none;
  -webkit-tap-highlight-color: transparent; }
.climb-steer { left: clamp(12px, 3vw, 48px); display: flex; align-items: center; justify-content: center; gap: 2px; }
.climb-jump { right: clamp(12px, 3vw, 48px); display: flex; flex-direction: column; align-items: center;
  justify-content: center; gap: 2px; background: #faeeda; border-color: #ba7517; color: #633806; }
.climb-steer:active { background: #c0dd97; }
.climb-jump:active { background: #f5c775; }
.climb-steer-divider { width: 2px; height: 46%; background: #97c459; border-radius: 2px; }
.climb-arrow { width: 38px; height: 38px; fill: #3b6d11; pointer-events: none; }
.climb-jump .climb-arrow { fill: #ba7517; }
.climb-steer .climb-label { position: absolute; bottom: 8px; font-size: 11px; }
.climb-jump .climb-label { font-size: 13px; }
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
  .climb-steer, .climb-jump { bottom: 12px; width: min(42vw, 132px); height: 100px; border-radius: 22px; }
  .climb-steer { left: 14px; }
  .climb-jump { right: 14px; }
  .climb-arrow { width: 32px; height: 32px; }
  .climb-header { flex-basis: 48px; height: 48px; gap: 6px; padding-left: 10px; padding-right: 70px; }
  .climb-title { font-size: 13px; }
  .climb-hud { flex-wrap: nowrap; gap: 6px; }
  .climb-difficulty { font-size: 11px; }
  .climb-caption { display: none; }
}
@media (orientation: portrait) and (max-width: 360px) {
  .climb-title { display: none; }
  .climb-scroll { font-size: 11px; }
  .climb-surface { font-size: 10px; padding: 1px 4px; }
  .climb-hud { gap: 4px; }
}
@media (orientation: landscape) and (max-height: 520px) {
  .climb-header { flex-basis: 44px; height: 44px; padding-block: 4px; }
  .climb-steer, .climb-jump { bottom: 12px; width: 96px; height: 96px; border-radius: 22px; }
  .climb-arrow { width: 30px; height: 30px; }
  .climb-overlay-card { padding: 12px; gap: 6px; }
  .climb-cushion { width: 64px; height: 44px; }
  .climb-caption { display: none; }
}
</style>
