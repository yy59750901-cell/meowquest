<script setup>
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, useId } from 'vue'
import CatSprite from './CatSprite.vue'
import { CAT_PLAY_CONFIG as WORLD, CAT_PLAY_CHALLENGE_CONFIG as CHALLENGE, CAT_PLAY_HELP, CAT_PLAY_CHALLENGE_HELP, createCatPlayEngine, getCatPlayFeatherBounds } from '../cat-play.js'

const props = defineProps({
  species: { type: String, default: 'orange' },
  catName: { type: String, default: '小猫' },
  standalone: { type: Boolean, default: false },
})
const emit = defineEmits(['close'])
const titleId = useId()
const helpId = useId()
const dialog = ref(null)
const stage = ref(null)
const featherButton = ref(null)
const closeButton = ref(null)
const engine = createCatPlayEngine()
const state = shallowRef(engine.snapshot())
const manualPaused = ref(false)
let raf = null
let lastFrame = null
let mounted = false
let closing = false
let backgroundSuspended = false
let previousFocus = null
let capturedPointerId = null
let dragOffset = { x: 0, y: 0 }
let resizeObserver = null
const stageSize = shallowRef({ width: 0, height: 0 })
const inputBlocked = computed(() => state.value.phase === 'paused' || state.value.phase === 'pounce' || state.value.challenge?.completed === true)
const challengeProgress = computed(() => state.value.challenge?.completed ? '挑战完成'
  : state.value.challenge?.checkpointVisited ? '已经过脚印 / 扑到目标' : '经过脚印 / 再扑到目标')
let spaceHeld = false
let suppressKeyboardClick = false
let clickSuppressionTimer = null

const catAnim = computed(() => {
  const current = state.value
  const stopDistance = current.challenge?.checkpointVisited ? CHALLENGE.followStopDistance : WORLD.followStopDistance
  if (current.phase === 'follow' && (current.blocked || Math.hypot(current.cat.x - current.feather.x, current.cat.y - current.feather.y) <= stopDistance + 0.01)) return 'idle'
  return {
    idle: 'idle', follow: 'walk', charging: 'crouch', ready: 'crouch',
    pounce: 'pounce', caught: 'catch', recover: 'recover', paused: 'sit',
  }[current.phase]
})
const phaseLabel = computed(() => {
  if (state.value.phase !== 'paused' && state.value.challenge?.completed) return '挑战完成'
  if (state.value.phase === 'follow' && state.value.challenge) {
    return state.value.challenge.checkpointVisited ? '把羽毛引到目标圈' : '先引猫到上方脚印'
  }
  return {
    idle: '按住羽毛开始', follow: '拖动后停一停', charging: '蓄势中 · 保持不动',
    ready: '现在松手时机好', pounce: '扑过去啦', caught: '抓住羽毛啦',
    recover: '还可以再来', paused: '休息一下',
  }[state.value.phase]
})
const featherStyle = computed(() => ({
  left: `${state.value.feather.x / WORLD.width * 100}%`,
  top: `${state.value.feather.y / WORLD.height * 100}%`,
  width: `${WORLD.featherHitSize}px`,
  height: `${WORLD.featherHitSize}px`,
}))

function clockRunning() {
  return mounted && !closing && !manualPaused.value && !backgroundSuspended && !document.hidden && state.value.phase !== 'paused'
}

function syncClock(timestamp = performance.now()) {
  if (!clockRunning()) {
    lastFrame = null
    return
  }
  if (!Number.isFinite(timestamp) || (lastFrame !== null && timestamp <= lastFrame)) return
  const dt = lastFrame === null ? 0 : Math.min((timestamp - lastFrame) / 1000, WORLD.maxDt)
  lastFrame = timestamp
  state.value = engine.tick(dt)
}

function send(event) {
  if (['pointerdown', 'pointermove', 'pointerup'].includes(event.type)) {
    syncClock()
    if (!clockRunning()) return
  } else {
    // 取消、暂停、切模式和重置不补算旧阶段；后台时间也不带入新回合。
    lastFrame = null
  }
  state.value = engine.dispatch(event)
  if (clockRunning() && lastFrame === null) lastFrame = performance.now()
}

function stopFrames() {
  if (raf !== null) cancelAnimationFrame(raf)
  raf = null
  lastFrame = null
}

function frame(timestamp) {
  raf = null
  if (!clockRunning()) {
    lastFrame = null
    return
  }
  syncClock(timestamp)
  raf = requestAnimationFrame(frame)
}

function startFrames() {
  if (clockRunning() && raf === null) {
    lastFrame = performance.now()
    raf = requestAnimationFrame(frame)
  }
}

function releaseCapture() {
  const pointerId = capturedPointerId
  capturedPointerId = null
  if (pointerId !== null && featherButton.value?.hasPointerCapture(pointerId)) {
    featherButton.value.releasePointerCapture(pointerId)
  }
}

function suppressNativeClick() {
  suppressKeyboardClick = true
  clearTimeout(clickSuppressionTimer)
  clickSuppressionTimer = null
}

function finishKeyInteraction() {
  spaceHeld = false
  suppressNativeClick()
  // 原生按钮 click 在 keyup 默认动作阶段发生，只屏蔽本轮事件，不影响后续读屏激活。
  clickSuppressionTimer = setTimeout(() => {
    suppressKeyboardClick = false
    clickSuppressionTimer = null
  }, 0)
}

function cancelDrag() {
  if (spaceHeld) finishKeyInteraction()
  if (state.value.held || state.value.phase === 'pounce') send({ type: 'cancel' })
  releaseCapture()
}

function syncBounds(cancelInput = false) {
  if (cancelInput) cancelDrag()
  const rect = stage.value.getBoundingClientRect()
  if (rect.width < WORLD.featherEdgeMargin * 2 || rect.height < WORLD.featherEdgeMargin * 2) return
  if (rect.width === stageSize.value.width && rect.height === stageSize.value.height) return
  cancelDrag()
  stageSize.value = { width: rect.width, height: rect.height }
  send({ type: 'resize', bounds: getCatPlayFeatherBounds(rect.width, rect.height) })
}

function stageResized() { syncBounds(true) }

function worldPoint(event) {
  const rect = stage.value.getBoundingClientRect()
  return {
    x: (event.clientX - rect.left) / rect.width * WORLD.width,
    y: (event.clientY - rect.top) / rect.height * WORLD.height,
  }
}

function pointerDown(event) {
  syncClock()
  if (event.button !== 0 || event.isPrimary === false || state.value.held || capturedPointerId !== null || inputBlocked.value || closing) return
  event.preventDefault()
  syncBounds()
  clearTimeout(clickSuppressionTimer)
  clickSuppressionTimer = null
  suppressKeyboardClick = false
  featherButton.value.focus({ preventScroll: true })
  const point = worldPoint(event)
  send({ type: 'pointerdown', pointerId: event.pointerId, ...state.value.feather })
  if (!state.value.held) return
  // 引擎可能已结束抱羽毛并放好下一轮，必须用处理后的坐标保持拖动连续。
  dragOffset = { x: point.x - state.value.feather.x, y: point.y - state.value.feather.y }
  try {
    featherButton.value.setPointerCapture(event.pointerId)
    capturedPointerId = event.pointerId
  } catch {
    cancelDrag()
  }
}

function pointerMove(event) {
  syncClock()
  if (!state.value.held || state.value.pointerId !== event.pointerId) return
  const point = worldPoint(event)
  send({ type: 'pointermove', pointerId: event.pointerId, x: point.x - dragOffset.x, y: point.y - dragOffset.y })
}

function pointerUp(event) {
  syncClock()
  if (!state.value.held || state.value.pointerId !== event.pointerId) return
  pointerMove(event)
  send({ type: 'pointerup', pointerId: event.pointerId })
  releaseCapture()
}

function pointerCancel(event) {
  if (state.value.pointerId !== event.pointerId && state.value.phase !== 'pounce') return
  send({ type: 'pointercancel', pointerId: event.pointerId })
  releaseCapture()
}

function lostCapture(event) {
  if (capturedPointerId === event.pointerId) {
    capturedPointerId = null
    if (state.value.held) send({ type: 'pointercancel', pointerId: event.pointerId })
  }
}

function keyboardDown(event) {
  syncClock()
  if (event.code === 'Space' || event.key === ' ') {
    event.preventDefault()
    suppressNativeClick()
    if (event.repeat || spaceHeld) return
    syncBounds()
    spaceHeld = true
    if (!state.value.held && !inputBlocked.value && !closing) send({ type: 'pointerdown', pointerId: 'keyboard', ...state.value.feather })
    return
  }
  const direction = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[event.key]
  if (!direction) return
  event.preventDefault()
  if (state.value.pointerId !== 'keyboard') return
  send({
    type: 'pointermove', pointerId: 'keyboard',
    x: state.value.feather.x + direction[0] * 12,
    y: state.value.feather.y + direction[1] * 12,
  })
}

function keyboardUp(event) {
  syncClock()
  if (event.code !== 'Space' && event.key !== ' ') return
  event.preventDefault()
  finishKeyInteraction()
  if (state.value.pointerId === 'keyboard') send({ type: 'pointerup', pointerId: 'keyboard' })
}

function accessibleClick(event) {
  syncClock()
  if (event.detail !== 0 || suppressKeyboardClick || inputBlocked.value || closing) return
  event.preventDefault()
  syncBounds()
  if (state.value.pointerId === 'keyboard') send({ type: 'pointerup', pointerId: 'keyboard' })
  else if (!state.value.held) send({ type: 'pointerdown', pointerId: 'keyboard', ...state.value.feather })
}

function suspend(type) {
  backgroundSuspended = true
  cancelDrag()
  send({ type })
  releaseCapture()
  stopFrames()
}

function resumeBackground() {
  if (document.hidden || !document.hasFocus()) return
  backgroundSuspended = false
  if (!manualPaused.value) {
    send({ type: 'resume' })
    startFrames()
  }
}

function visibilityChanged() {
  if (document.hidden) suspend('visibilitychange')
  else resumeBackground()
}
function windowBlur() { suspend('blur') }

function togglePause() {
  manualPaused.value = !manualPaused.value
  if (manualPaused.value) {
    cancelDrag()
    send({ type: 'pause' })
    releaseCapture()
    stopFrames()
  } else if (!backgroundSuspended && !document.hidden) {
    send({ type: 'resume' })
    startFrames()
  }
}

function switchMode(mode) {
  if (closing || state.value.mode === mode) return
  stopFrames()
  cancelDrag()
  finishKeyInteraction()
  send({ type: 'set-mode', mode })
  if (manualPaused.value || backgroundSuspended || document.hidden) send({ type: 'pause' })
  else startFrames()
}

function restart() {
  stopFrames()
  cancelDrag()
  finishKeyInteraction()
  send({ type: 'reset' })
  if (manualPaused.value || backgroundSuspended || document.hidden) send({ type: 'pause' })
  else startFrames()
}

function requestClose() {
  if (closing) return
  closing = true
  cancelDrag()
  send({ type: 'pause' })
  stopFrames()
  emit('close')
}

function focusables() {
  return [...dialog.value.querySelectorAll('button:not(:disabled), [tabindex="0"]')]
}

function dialogKeydown(event) {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    requestClose()
  } else if (event.key === 'Tab') {
    // 切换焦点不是松手扑跃；避免 Space 的 keyup 丢失而一直握住。
    cancelDrag()
    const elements = focusables()
    const first = elements[0]
    const last = elements.at(-1)
    if (event.shiftKey && (document.activeElement === first || !elements.includes(document.activeElement))) {
      event.preventDefault()
      last?.focus()
    } else if (!event.shiftKey && (document.activeElement === last || !elements.includes(document.activeElement))) {
      event.preventDefault()
      first?.focus()
    }
  }
}

function retainFocus(event) {
  if (!closing && dialog.value && !dialog.value.contains(event.target)) closeButton.value?.focus({ preventScroll: true })
}

// iOS Safari 忽略 viewport 的 user-scalable / maximum-scale，多指手势可能被识别成整页捏合缩放。
// 只在游戏挂载期间拦截，退出即移除，不影响主应用滚动与家长端放大看图。
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

onMounted(() => {
  mounted = true
  previousFocus = document.activeElement
  closeButton.value.focus({ preventScroll: true })
  syncBounds()
  window.addEventListener('blur', windowBlur)
  window.addEventListener('focus', resumeBackground)
  window.addEventListener('resize', stageResized)
  window.addEventListener('orientationchange', stageResized)
  document.addEventListener('visibilitychange', visibilityChanged)
  document.addEventListener('focusin', retainFocus)
  removeZoomGuard = installZoomGuard()
  resizeObserver = new ResizeObserver(() => syncBounds())
  resizeObserver.observe(stage.value)
  if (document.hidden) suspend('visibilitychange')
  else startFrames()
})

onBeforeUnmount(() => {
  mounted = false
  stopFrames()
  cancelDrag()
  clearTimeout(clickSuppressionTimer)
  clickSuppressionTimer = null
  resizeObserver?.disconnect()
  window.removeEventListener('blur', windowBlur)
  window.removeEventListener('focus', resumeBackground)
  window.removeEventListener('resize', stageResized)
  window.removeEventListener('orientationchange', stageResized)
  document.removeEventListener('visibilitychange', visibilityChanged)
  document.removeEventListener('focusin', retainFocus)
  removeZoomGuard?.()
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})
</script>

<template>
  <section ref="dialog" class="cat-playground"
    :class="{ 'is-standalone': standalone, 'play-mode-free': state.mode === 'free', 'play-mode-challenge': state.mode === 'challenge' }"
    :data-mode="state.mode" :data-phase="state.phase" :data-challenge-completed="state.challenge?.completed ?? false"
    role="dialog" aria-modal="true" :aria-labelledby="titleId" :aria-describedby="helpId"
    @keydown="dialogKeydown" @pointerdown.stop @pointermove.stop @pointerup.stop @click.stop @wheel.stop>
    <div class="play-panel">
      <header class="play-header">
        <div>
          <span class="play-eyebrow">羽毛逗猫 · {{ state.mode === 'free' ? '自由试玩' : '绕垫子引猫' }}</span>
          <h2 :id="titleId">{{ catName }}的游乐场</h2>
        </div>
        <button ref="closeButton" type="button" class="play-close" aria-label="退出小猫游乐场" @click="requestClose">退出</button>
      </header>
      <div class="play-modes" role="group" aria-label="选择羽毛玩法">
        <button type="button" data-mode="free" :aria-pressed="state.mode === 'free'" @click="switchMode('free')">自由玩</button>
        <button type="button" data-mode="challenge" :aria-pressed="state.mode === 'challenge'" @click="switchMode('challenge')">小挑战</button>
      </div>
      <p :id="helpId" class="play-help">{{ state.mode === 'challenge' ? CAT_PLAY_CHALLENGE_HELP : CAT_PLAY_HELP }}</p>

      <div ref="stage" class="play-stage" data-world="640x400" :data-phase="state.phase"
        :data-stage-width="stageSize.width" :data-stage-height="stageSize.height"
        :data-input-left="state.bounds.left" :data-input-right="state.bounds.right"
        :data-input-top="state.bounds.top" :data-input-bottom="state.bounds.bottom">
        <svg class="play-room" viewBox="0 0 640 400" preserveAspectRatio="xMidYMid meet" aria-hidden="true">
          <defs>
            <pattern :id="`${titleId}-boards`" width="128" height="48" patternUnits="userSpaceOnUse">
              <rect width="128" height="48" fill="#f5d6a6" />
              <path d="M0 0H128M0 48H128M64 0V24M0 24H128M16 24V48" fill="none" stroke="#dfb789" stroke-width="2" />
              <path d="M76 10h28M28 36h32" stroke="#ebc695" stroke-width="3" />
            </pattern>
          </defs>
          <g shape-rendering="crispEdges">
            <rect width="640" height="400" fill="#fff2d9" />
            <rect y="134" width="640" height="266" :fill="`url(#${titleId}-boards)`" />
            <rect y="130" width="640" height="10" fill="#c59771" />
            <rect y="126" width="640" height="5" fill="#e8c6a0" />
            <rect x="42" y="25" width="112" height="83" fill="#c59771" />
            <rect x="49" y="32" width="98" height="69" fill="#bfdfdc" />
            <rect x="56" y="39" width="84" height="55" fill="#def1e8" />
            <rect x="68" y="69" width="24" height="8" fill="#bed3ad" />
            <rect x="94" y="60" width="24" height="17" fill="#bed3ad" />
            <path d="M98 32v69M49 66h98" stroke="#fff8e9" stroke-width="6" />
            <rect x="34" y="20" width="22" height="83" fill="#efb4a0" />
            <rect x="144" y="20" width="22" height="83" fill="#efb4a0" />
            <rect x="30" y="18" width="140" height="6" fill="#b68b6e" />
            <rect x="530" y="78" width="64" height="8" fill="#b58a6a" />
            <rect x="534" y="47" width="22" height="31" fill="#e0a88c" />
            <rect x="540" y="27" width="8" height="28" fill="#94b985" />
            <rect x="529" y="32" width="15" height="10" fill="#a9c699" />
            <rect x="548" y="22" width="13" height="10" fill="#a9c699" />
            <rect x="573" y="42" width="6" height="36" fill="#c59c8a" />
            <rect x="580" y="38" width="7" height="40" fill="#e6b78b" />
            <g v-if="state.mode === 'free'">
              <rect x="194" y="343" width="252" height="26" fill="#e9bc97" />
              <rect x="202" y="339" width="236" height="26" fill="#f2c9b0" />
              <path d="M212 352h216" stroke="#fbe0c3" stroke-width="4" stroke-dasharray="12 8" />
            </g>
          </g>
          <g v-if="state.challenge" class="play-route" aria-hidden="true">
            <path d="M182 271 L220 190 L430 185 L478 286" fill="none" stroke="#97784e" stroke-width="3" stroke-dasharray="7 9" opacity=".45" />
            <rect class="play-rug" :x="state.challenge.rug.x" :y="state.challenge.rug.y"
              :width="state.challenge.rug.w" :height="state.challenge.rug.h" rx="8" fill="#dcaa90" stroke="#a16e57" stroke-width="4" />
            <path d="M280 258h80M280 306h80" stroke="#f6d4ba" stroke-width="4" stroke-dasharray="8 5" />
            <text x="320" y="286" text-anchor="middle" fill="#734e3d" font-size="17" font-weight="700">绕过垫子</text>
            <g class="play-waypoint" :data-cx="state.challenge.waypoint.x" :data-cy="state.challenge.waypoint.y"
              :data-radius="state.challenge.waypoint.radius" :data-visited="state.challenge.checkpointVisited">
              <circle :cx="state.challenge.waypoint.x" :cy="state.challenge.waypoint.y" :r="state.challenge.waypoint.radius"
                :fill="state.challenge.checkpointVisited ? '#d5ead1' : '#fff3ca'" stroke="#688259" stroke-width="3" stroke-dasharray="6 4" />
              <g :transform="`translate(${state.challenge.waypoint.x} ${state.challenge.waypoint.y})`" fill="#688259">
                <ellipse cy="5" rx="11" ry="9" /><circle cx="-12" cy="-7" r="5" /><circle cx="0" cy="-12" r="5" /><circle cx="12" cy="-7" r="5" />
              </g>
              <text :x="state.challenge.waypoint.x" y="160" text-anchor="middle" fill="#486140" font-size="16" font-weight="700">上方脚印</text>
            </g>
            <g class="play-goal" :data-cx="state.challenge.goal.x" :data-cy="state.challenge.goal.y" :data-radius="state.challenge.goal.radius">
              <circle :cx="state.challenge.goal.x" :cy="state.challenge.goal.y" :r="state.challenge.goal.radius"
                fill="#f1f4d6" fill-opacity=".6" stroke="#598461" stroke-width="4" stroke-dasharray="9 5" />
              <text :x="state.challenge.goal.x" y="345" text-anchor="middle" fill="#486140" font-size="17" font-weight="700">扑到目标</text>
            </g>
          </g>
          <ellipse :cx="state.cat.x" :cy="state.cat.y + 3" :rx="39 - state.cat.lift * 0.24" ry="9" fill="#946c4e" opacity=".18" />
          <g v-if="state.phase === 'charging' || state.phase === 'ready'">
            <ellipse :cx="state.cat.x" :cy="state.cat.y + 3" rx="55" ry="17" fill="none"
              :stroke="state.phase === 'ready' ? '#48835a' : '#bf8b37'" stroke-width="4" stroke-dasharray="10 6" />
            <rect :x="state.cat.x - 38" :y="state.cat.y + 25" width="76" height="7" rx="2" fill="#ecdab3" />
            <rect :x="state.cat.x - 38" :y="state.cat.y + 25" :width="76 * state.chargeProgress" height="7" rx="2"
              :fill="state.phase === 'ready' ? '#48835a' : '#bf8b37'" />
          </g>
          <g class="play-cat" :data-anim="catAnim" :data-x="state.cat.x" :data-y="state.cat.y"
            :data-lift="state.cat.lift" :data-pounce-progress="state.pounceProgress"
            :transform="`translate(${state.cat.x} ${state.cat.y - state.cat.lift})`">
            <g :transform="`scale(${state.cat.facing === 1 ? -1 : 1} 1)`">
              <CatSprite x="-56" y="-84" :species="species" :anim="catAnim" :size="112" :paused="state.phase === 'paused'" />
            </g>
          </g>
          <g v-if="state.phase === 'caught'" :transform="`translate(${state.cat.x} ${state.cat.y - 92})`" stroke="#ba7e38" stroke-width="4">
            <path d="M-26 8l-8-8M0 2v-10M26 8l8-8" />
          </g>
        </svg>
        <span class="play-cue" :class="{ 'is-ready': state.phase === 'ready' }">{{ phaseLabel }}</span>
        <button ref="featherButton" type="button" class="play-feather" :style="featherStyle" :data-phase="state.phase"
          :class="{ 'is-held': state.held, 'is-ready': state.phase === 'ready', 'is-caught': state.phase === 'caught' }"
          :aria-disabled="inputBlocked" :aria-pressed="state.held" :data-hit-size="WORLD.featherHitSize"
          aria-label="羽毛玩具：按住拖动，准备好后松手；键盘按住空格并用方向键移动"
          @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerCancel"
          @lostpointercapture="lostCapture" @keydown="keyboardDown" @keyup="keyboardUp" @click="accessibleClick" @blur="cancelDrag">
          <svg viewBox="0 0 56 64" width="42" height="48" aria-hidden="true" shape-rendering="crispEdges">
            <path d="M28 49L18 37V16L24 7H36L43 17V33L35 44Z" fill="#b497c9" />
            <path d="M28 49L28 17L36 7H24L18 16V37Z" fill="#dcc6e6" />
            <path d="M28 14V55M28 24l-8-7M28 33l-8-7M28 41l-7-7M29 24l9-8M29 33l11-9M29 42l7-7" fill="none" stroke="#806392" stroke-width="3" />
            <path d="M28 53L33 58H43" fill="none" stroke="#bf8b56" stroke-width="3" />
          </svg>
        </button>
      </div>

      <div class="play-status play-evaluation" role="status" aria-live="polite" aria-atomic="true"
        :data-phase="state.phase" :grade="state.evaluation?.grade ?? 'none'" :data-grade="state.evaluation?.grade ?? 'none'">
        <template v-if="state.evaluation">
          <strong class="play-evaluation-title">{{ state.evaluation.title }}</strong>
          <span class="play-evaluation-detail">{{ state.evaluation.detail }}</span>
        </template>
        <span v-else class="play-evaluation-detail">{{ state.status }}</span>
      </div>
      <footer class="play-footer">
        <span v-if="state.challenge" class="play-progress" aria-live="polite" :data-checkpoint-visited="state.challenge.checkpointVisited">{{ challengeProgress }}</span>
        <span v-else class="play-count" aria-live="polite">本次抓到 <strong>{{ state.catches }}</strong> 次</span>
        <div class="play-controls">
          <button type="button" class="play-pause" :aria-label="manualPaused ? '继续羽毛逗猫' : '暂停羽毛逗猫'" @click="togglePause">
            {{ manualPaused ? '继续玩' : '休息一下' }}
          </button>
          <button type="button" class="play-restart" :aria-label="state.challenge ? '再挑战一次，重置脚印和完成状态' : '重新开始羽毛逗猫，清零本次抓到次数'" @click="restart">{{ state.challenge ? '再挑战一次' : '重新开始' }}</button>
        </div>
      </footer>
      <p class="play-keyboard">键盘：按住空格，用方向键移动；绿圈亮起后松手。<span>只在当前试玩中，不消耗、不产生星星。</span></p>
    </div>
  </section>
</template>

<style scoped>
.cat-playground {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 12px;
  box-sizing: border-box;
  background: rgb(85 64 47 / 32%);
  color: #624833;
  font-family: inherit;
  overscroll-behavior: contain;
}
.cat-playground.is-standalone { background: #f6ead7; }
.play-panel {
  width: min(860px, 100%);
  max-height: calc(100vh - 24px);
  max-height: calc(100dvh - 24px);
  overflow: auto;
  box-sizing: border-box;
  padding: 20px;
  border: 3px solid #c99d78;
  border-radius: 16px;
  background: #fffaf0;
  box-shadow: 0 5px 0 #dab68e, 0 16px 48px rgb(74 51 32 / 12%);
}
.play-header { position: sticky; top: 0; z-index: 2; display: flex; justify-content: space-between; align-items: center; gap: 12px; background: #fffaf0; }
.play-eyebrow { display: block; font-size: 12px; color: #946d4c; letter-spacing: .04em; }
h2 { margin: 5px 0 0; font-size: 22px; line-height: 1.25; }
button { font: inherit; color: inherit; cursor: pointer; -webkit-tap-highlight-color: transparent; touch-action: none; }
button:focus-visible { outline: 3px solid #527b66; outline-offset: 3px; }
.play-close, .play-controls button {
  min-height: 44px;
  padding: 8px 14px;
  border: 2px solid #c99d78;
  border-radius: 9px;
  background: #ffedd8;
  font-size: 14px;
  font-weight: 700;
  white-space: nowrap;
}
.play-close { flex-shrink: 0; background: #fff6e8; }
.play-modes { display: flex; gap: 4px; margin: 10px 0 0; padding: 3px; width: fit-content; border: 1px solid #d6b58c; border-radius: 10px; background: #f5e8d5; }
.play-modes button { min-width: 92px; min-height: 44px; padding: 6px 16px; border: 0; border-radius: 7px; background: transparent; font-size: 14px; font-weight: 700; }
.play-modes button[aria-pressed='true'] { background: #fffaf0; color: #315b3e; box-shadow: 0 1px 3px rgb(74 51 32 / 15%); }
.play-help { margin: 10px 0; font-size: 15px; line-height: 1.4; }
.play-stage {
  position: relative;
  width: 100%;
  aspect-ratio: 8 / 5;
  margin: 0 auto;
  box-sizing: content-box;
  overflow: hidden;
  border-radius: 9px;
  outline: 2px solid #ddba92;
  touch-action: none;
  user-select: none;
  -webkit-user-select: none;
}
.play-room { display: block; width: 100%; height: 100%; }
.play-cue {
  position: absolute;
  top: 10px;
  left: 50%;
  transform: translateX(-50%);
  padding: 5px 10px;
  border: 1px solid #dec195;
  border-radius: 6px;
  background: #fffaec;
  font-size: 13px;
  font-weight: 700;
  white-space: nowrap;
  pointer-events: none;
}
.play-cue.is-ready { color: #315b3e; background: #edf7df; border-color: #77a069; }
.play-feather {
  position: absolute;
  transform: translate(-50%, -50%);
  box-sizing: border-box;
  display: grid;
  place-items: center;
  padding: 0;
  border: 2px dashed rgb(128 99 146 / 45%);
  border-radius: 50%;
  background: rgb(255 250 240 / 45%);
  touch-action: none;
  cursor: grab;
}
.play-feather svg { pointer-events: none; }
.play-feather.is-held { cursor: grabbing; border-style: solid; background: rgb(251 241 255 / 65%); }
.play-feather.is-ready { border: 3px solid #51815a; background: rgb(234 248 212 / 70%); box-shadow: 0 0 0 4px rgb(113 161 92 / 20%); }
.play-feather.is-caught { border-color: transparent; background: transparent; }
.play-feather[aria-disabled='true'] { opacity: 1; cursor: default; }
.play-status {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: flex-start;
  gap: 3px;
  height: 100px;
  min-height: 100px;
  overflow: hidden;
  margin: 10px 0;
  padding: 8px 12px;
  box-sizing: border-box;
  border-radius: 8px;
  background: #f7eedf;
  font-size: 14px;
  line-height: 1.35;
}
.play-evaluation-title { flex-shrink: 0; font-size: 22px; line-height: 1.15; }
.play-evaluation-detail { display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; overflow: hidden; }
.play-evaluation[grade='perfect'], .play-evaluation[grade='good'], .play-evaluation[grade='late'], .play-status[data-phase='ready'] { color: #315b3e; background: #edf4df; }
.play-evaluation[grade='early'], .play-evaluation[grade='far'], .play-evaluation[grade='close'], .play-evaluation[grade='blocked'], .play-evaluation[grade='route-needed'], .play-evaluation[grade='outside-goal'] { color: #84532a; background: #fbedd8; }
.play-progress { font-size: 13px; font-weight: 700; color: #486140; }
.play-footer { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.play-count { font-size: 14px; white-space: nowrap; }
.play-count strong { color: #9a5a30; font-size: 20px; }
.play-controls { display: flex; gap: 8px; }
.play-keyboard { margin: 12px 0 0; font-size: 12px; line-height: 1.5; color: #947451; }
.play-keyboard span { display: block; }
@media (min-width: 600px) {
  .play-stage { width: min(100%, max(320px, calc((100dvh - 440px) * 1.6))); }
}
@media (max-height: 500px) and (min-width: 600px) {
  .play-panel { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(240px, 1fr); gap: 8px 18px; padding: 12px 16px; }
  .play-header { grid-column: 1 / -1; }
  h2 { font-size: 19px; }
  .play-stage { grid-column: 1; grid-row: 2 / span 5; align-self: center; width: min(100%, calc((100dvh - 112px) * 1.6)); }
  .play-modes { grid-column: 2; grid-row: 2; margin: 0; }
  .play-help { grid-column: 2; grid-row: 3; margin: 0; font-size: 13px; }
  .play-status { grid-column: 2; grid-row: 4; margin: 0; padding: 6px 12px; font-size: 12px; height: 94px; min-height: 94px; }
  .play-evaluation-title { font-size: 20px; }
  .play-footer { grid-column: 2; grid-row: 5; gap: 4px; }
  .play-keyboard { grid-column: 2; grid-row: 6; margin: 0; font-size: 11px; }
  .play-keyboard span { display: none; }
}
@media (max-width: 599px) {
  .play-panel { padding: 10px; border-radius: 12px; }
  h2 { font-size: 19px; }
  .play-modes { margin-top: 6px; padding: 0; }
  .play-help { margin: 8px 0; font-size: 13px; }
  .play-status { height: 94px; min-height: 94px; margin: 8px 0; padding: 6px 10px; font-size: 12px; }
  .play-evaluation-title { font-size: 18px; }
  .play-controls { gap: 6px; }
  .play-controls button { padding: 8px 10px; }
  .play-footer { gap: 6px; }
  .play-mode-challenge .play-footer { flex-direction: column; align-items: stretch; }
  .play-mode-challenge .play-progress { min-height: 18px; }
  .play-mode-challenge .play-controls { justify-content: flex-end; }
  .play-keyboard { display: none; }
}
</style>
