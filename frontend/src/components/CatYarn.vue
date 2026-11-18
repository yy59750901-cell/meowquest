<script setup>
// 毛球合成：把毛线球丢进盒子，两个一样的碰在一起合成更大的一级，合出大花猫。
// 规则与物理在 cat-yarn.js（已单测）；这里只负责输入与渲染。不花星星、不产星星、不存成绩。
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, useId } from 'vue'
import CatSprite from './CatSprite.vue'
import { YARN_CONFIG as W, YARN_MAX, createYarnEngine } from '../cat-yarn.js'

const props = defineProps({
  species: { type: String, default: 'orange' },
  catName: { type: String, default: '小猫' },
})
const emit = defineEmits(['close'])
const titleId = useId()
const helpId = useId()
const dialog = ref(null)
const stage = ref(null)
const closeButton = ref(null)
const engine = createYarnEngine()
const state = shallowRef(engine.snapshot())
const bestEver = ref(-1) // 仅本次打开期间的最高等级，不保存
let raf = null, last = null, closing = false, mounted = false
let capturedId = null
let removeGuard = null, previousFocus = null

// 每一级的配色：线团底色、线纹色、高光
const PAL = [
  ['#f4b6c2', '#d9758b'], ['#f7c98b', '#d99a3f'], ['#f1e18a', '#c9b132'], ['#b7dc9a', '#6fa65a'],
  ['#9fd5d0', '#4f9f9a'], ['#9fc2ee', '#5a86c8'], ['#c9b0ea', '#8d6cc4'], ['#f2a47f', '#c9663a'],
]
const levelsShown = computed(() => W.radii.map((r, i) => ({ lv: i, name: W.names[i], r })))
const aimLeft = computed(() => `${state.value.aimX / W.width * 100}%`)
const heldR = computed(() => W.radii[state.value.held])
const lineTop = `${W.lineY / W.height * 100}%`
const over = computed(() => state.value.phase === 'over')
const dangerPct = computed(() => Math.round(state.value.danger * 100))
const bestName = computed(() => state.value.best >= 0 ? W.names[state.value.best] : '还没有')
const ballStyle = b => ({ left: `${(b.x - b.r) / W.width * 100}%`, top: `${(b.y - b.r) / W.height * 100}%`, width: `${b.r * 2 / W.width * 100}%`, height: `${b.r * 2 / W.height * 100}%` })

function onSnapshot(snap) {
  state.value = snap
  if (snap.best > bestEver.value) bestEver.value = snap.best
}
const send = e => onSnapshot(engine.dispatch(e))
function clockRunning() { return mounted && !closing && !document.hidden }
function frame(ts) {
  raf = null
  if (!clockRunning()) { last = null; return }
  const dt = last === null ? 0 : Math.min((ts - last) / 1000, 0.05)
  last = ts
  onSnapshot(engine.tick(dt))
  raf = requestAnimationFrame(frame)
}
function startFrames() { if (raf === null && clockRunning()) { last = null; raf = requestAnimationFrame(frame) } }
function stopFrames() { if (raf !== null) cancelAnimationFrame(raf); raf = null; last = null }
function visibility() { if (document.hidden) { stopFrames(); cancelAim() } else startFrames() }

function worldX(ev) {
  const r = stage.value.getBoundingClientRect()
  return (ev.clientX - r.left) / r.width * W.width
}
function releaseCapture() {
  const id = capturedId; capturedId = null
  if (id !== null && stage.value?.hasPointerCapture?.(id)) stage.value.releasePointerCapture(id)
}
function cancelAim() { releaseCapture() }
function pointerDown(ev) {
  if (ev.button !== 0 || ev.isPrimary === false || closing || over.value || capturedId !== null) return
  ev.preventDefault()
  send({ type: 'aim', x: worldX(ev) })
  try { stage.value.setPointerCapture(ev.pointerId); capturedId = ev.pointerId } catch { capturedId = null }
}
function pointerMove(ev) {
  if (capturedId === ev.pointerId) send({ type: 'aim', x: worldX(ev) })
  else if (ev.pointerType === 'mouse' && capturedId === null && !over.value) send({ type: 'aim', x: worldX(ev) })
}
function pointerUp(ev) {
  if (capturedId !== ev.pointerId) return
  send({ type: 'aim', x: worldX(ev) })
  send({ type: 'drop' })
  releaseCapture()
}
function pointerCancel(ev) { if (capturedId === ev.pointerId) releaseCapture() }
function lostCapture(ev) { if (capturedId === ev.pointerId) capturedId = null }
function keyboardDown(ev) {
  if (ev.key === 'ArrowLeft') { ev.preventDefault(); send({ type: 'nudge', dir: -1 }) }
  else if (ev.key === 'ArrowRight') { ev.preventDefault(); send({ type: 'nudge', dir: 1 }) }
  else if (ev.key === ' ' || ev.key === 'Enter' || ev.key === 'ArrowDown') {
    // 在按钮上按空格/回车是在点击按钮，不是丢球
    if (ev.target.closest?.('button') && ev.target !== stage.value) return
    ev.preventDefault(); if (!ev.repeat) send({ type: 'drop' })
  }
}
function restart() { send({ type: 'restart' }) }
function requestClose() {
  if (closing) return
  closing = true
  cancelAim(); stopFrames()
  emit('close')
}
function dialogKeydown(ev) {
  if (ev.key === 'Escape') { ev.preventDefault(); ev.stopPropagation(); requestClose(); return }
  if (ev.key !== 'Tab') return
  const els = [...dialog.value.querySelectorAll('button:not(:disabled), [tabindex="0"]')]
  const first = els[0], lastEl = els.at(-1)
  if (ev.shiftKey && (document.activeElement === first || !els.includes(document.activeElement))) { ev.preventDefault(); lastEl?.focus() }
  else if (!ev.shiftKey && (document.activeElement === lastEl || !els.includes(document.activeElement))) { ev.preventDefault(); first?.focus() }
}
function retainFocus(ev) { if (!closing && dialog.value && !dialog.value.contains(ev.target)) closeButton.value?.focus({ preventScroll: true }) }
function installZoomGuard() {
  const block = e => e.preventDefault()
  const multi = e => { if (e.touches?.length > 1) e.preventDefault() }
  const g = [['gesturestart', block], ['gesturechange', block], ['touchmove', multi]]
  for (const [t, f] of g) document.addEventListener(t, f, { passive: false })
  return () => { for (const [t, f] of g) document.removeEventListener(t, f) }
}
onMounted(() => {
  mounted = true
  previousFocus = document.activeElement
  closeButton.value.focus({ preventScroll: true })
  document.addEventListener('visibilitychange', visibility)
  document.addEventListener('focusin', retainFocus)
  removeGuard = installZoomGuard()
  startFrames()
})
onBeforeUnmount(() => {
  mounted = false
  stopFrames(); cancelAim()
  document.removeEventListener('visibilitychange', visibility)
  document.removeEventListener('focusin', retainFocus)
  removeGuard?.()
  if (previousFocus?.isConnected) previousFocus.focus({ preventScroll: true })
})
</script>

<template>
  <section ref="dialog" class="cat-yarn" role="dialog" aria-modal="true" :aria-labelledby="titleId" :aria-describedby="helpId"
    :data-phase="state.phase" @keydown="dialogKeydown" @pointerdown.stop @pointermove.stop @pointerup.stop @click.stop @wheel.stop>
    <div class="yarn-panel">
      <header class="yarn-header">
        <div>
          <span class="yarn-eyebrow">毛球合成 · 免费玩</span>
          <h2 :id="titleId">{{ catName }}的毛线球盒</h2>
        </div>
        <button ref="closeButton" type="button" class="yarn-close" aria-label="退出毛球合成" @click="requestClose">退出</button>
      </header>
      <div class="yarn-body">
        <div class="yarn-side">
          <div class="yarn-score" role="status" aria-live="polite">
            <span>得分</span><strong>{{ state.score }}</strong>
          </div>
          <div class="yarn-next" aria-label="下一个毛线球">
            <span>下一个</span>
            <svg viewBox="-1 -1 2 2" class="next-ball" aria-hidden="true"><circle r=".92" :fill="PAL[state.next][0]" :stroke="PAL[state.next][1]" stroke-width=".12" /></svg>
          </div>
          <ol class="yarn-ladder" aria-label="合成顺序">
            <li v-for="l in levelsShown" :key="l.lv" :class="{ got: bestEver >= l.lv }" :title="l.name">
              <svg viewBox="-1 -1 2 2" aria-hidden="true"><circle r=".9" :fill="PAL[l.lv][0]" :stroke="PAL[l.lv][1]" stroke-width=".14" /></svg>
              <span>{{ l.name }}</span>
            </li>
          </ol>
        </div>

        <div ref="stage" class="yarn-stage" tabindex="0" role="application" :id="helpId"
          aria-label="毛线球盒：左右移动，松手丢球；键盘左右键移动，空格丢球"
          :data-phase="state.phase" :data-balls="state.balls.length"
          @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerCancel"
          @lostpointercapture="lostCapture" @keydown="keyboardDown">
          <div class="yarn-wall"></div>
          <div class="yarn-line" :style="{ top: lineTop }" :class="{ hot: state.danger > 0 }"><span>危险线</span></div>
          <!-- 瞄准线 + 猫手里的球 -->
          <div v-if="!over" class="yarn-aim" :style="{ left: aimLeft }" aria-hidden="true"></div>
          <div v-if="!over" class="yarn-held" :class="{ cooling: state.cooldown > 0 }" aria-hidden="true"
            :style="{ left: aimLeft, top: `${(W.spawnY - heldR) / W.height * 100}%`, width: `${heldR * 2 / W.width * 100}%`, height: `${heldR * 2 / W.height * 100}%` }">
            <svg viewBox="-1 -1 2 2"><circle r=".92" :fill="PAL[state.held][0]" :stroke="PAL[state.held][1]" stroke-width=".1" /><path d="M-.6 -.1Q0 -.6 .6 -.1M-.7 .25Q0 -.2 .7 .25" fill="none" :stroke="PAL[state.held][1]" stroke-width=".07" /></svg>
          </div>
          <div v-for="b in state.balls" :key="b.id" class="yarn-ball" :data-lv="b.lv" :style="ballStyle(b)">
            <svg viewBox="-1 -1 2 2">
              <circle r=".94" :fill="PAL[b.lv][0]" :stroke="PAL[b.lv][1]" stroke-width=".1" />
              <path v-if="b.lv < YARN_MAX" d="M-.65 -.2Q0 -.7 .65 -.2M-.78 .2Q0 -.3 .78 .2M-.6 .55Q0 .05 .6 .55" fill="none" :stroke="PAL[b.lv][1]" stroke-width=".08" stroke-linecap="round" />
              <circle cx="-.3" cy="-.4" r=".16" fill="#fff" opacity=".5" />
            </svg>
            <CatSprite v-if="b.lv === YARN_MAX" class="ball-cat" :species="species" anim="sit" :size="Math.max(10, b.r * 1.0)" />
          </div>
          <div v-if="state.danger > 0 && !over" class="yarn-warn" role="alert">快满啦！别让毛线球越过线</div>
          <div v-if="over" class="yarn-over">
            <b>毛线球堆满啦</b>
            <span>这局最高合成到「{{ bestName }}」</span>
            <button type="button" class="yarn-again" @click="restart">再来一局</button>
          </div>
        </div>
      </div>

      <div class="yarn-status" role="status" aria-live="polite" aria-atomic="true">
        <span>{{ state.message.text }}</span>
        <div class="yarn-danger" v-if="state.danger > 0" aria-hidden="true"><i :style="{ width: dangerPct + '%' }"></i></div>
      </div>
      <footer class="yarn-footer">
        <span class="yarn-count">本局最高：<strong>{{ bestName }}</strong></span>
        <div class="yarn-controls"><button type="button" class="yarn-restart" @click="restart">重新开始</button></div>
      </footer>
      <p class="yarn-keyboard">键盘：左右键移动，空格丢球。<span>只在这里玩，不消耗、不产生星星，也不保存成绩。</span></p>
    </div>
  </section>
</template>

<style scoped>
.cat-yarn { position: fixed; inset: 0; z-index: 3000; display: flex; align-items: center; justify-content: center; padding: 12px; box-sizing: border-box; background: rgb(85 64 47 / 32%); color: #624833; font-family: inherit; overscroll-behavior: contain; }
.yarn-panel { width: min(760px, 100%); max-height: calc(100vh - 24px); max-height: calc(100dvh - 24px); overflow: auto; box-sizing: border-box; padding: 18px; border: 3px solid #c99d78; border-radius: 16px; background: #fffaf0; box-shadow: 0 5px 0 #dab68e, 0 16px 48px rgb(74 51 32 / 12%); }
.yarn-header { position: sticky; top: 0; z-index: 3; display: flex; justify-content: space-between; align-items: center; gap: 12px; background: #fffaf0; }
.yarn-eyebrow { display: block; font-size: 12px; color: #946d4c; letter-spacing: .04em; }
h2 { margin: 5px 0 0; font-size: 22px; line-height: 1.25; }
button { font: inherit; color: inherit; cursor: pointer; -webkit-tap-highlight-color: transparent; touch-action: manipulation; }
button:focus-visible, .yarn-stage:focus-visible { outline: 3px solid #527b66; outline-offset: 3px; }
.yarn-close, .yarn-restart, .yarn-again { min-height: 44px; padding: 8px 14px; border: 2px solid #c99d78; border-radius: 9px; background: #ffedd8; font-size: 14px; font-weight: 700; white-space: nowrap; }
.yarn-close { flex-shrink: 0; background: #fff6e8; }
.yarn-body { display: grid; grid-template-columns: 138px minmax(0, 1fr); gap: 12px; margin-top: 10px; align-items: start; }
.yarn-side { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.yarn-score, .yarn-next { display: flex; align-items: center; justify-content: space-between; gap: 6px; padding: 6px 10px; border-radius: 8px; background: #f7eedf; font-size: 13px; }
.yarn-score strong { font-size: 22px; color: #9a5a30; }
.next-ball { width: 34px; height: 34px; }
.yarn-ladder { list-style: none; margin: 0; padding: 6px 8px; display: grid; gap: 3px; border-radius: 8px; background: #f7eedf; }
.yarn-ladder li { display: flex; align-items: center; gap: 6px; font-size: 11px; opacity: .5; }
.yarn-ladder li.got { opacity: 1; font-weight: 700; }
.yarn-ladder svg { width: 16px; height: 16px; flex: none; }
.yarn-stage { position: relative; width: 100%; max-width: 360px; justify-self: center; aspect-ratio: 360 / 540; box-sizing: border-box; overflow: hidden; border-radius: 10px; border: 3px solid #b88e69; background: #fff2d9; touch-action: none; user-select: none; -webkit-user-select: none; cursor: crosshair; }
.yarn-wall { position: absolute; inset: 0; background: linear-gradient(#fff7e6 0 22%, #fde9c4 22% 100%); }
.yarn-line { position: absolute; left: 0; right: 0; height: 0; border-top: 3px dashed #d9a27c; z-index: 1; pointer-events: none; }
.yarn-line span { position: absolute; right: 6px; top: -18px; font-size: 11px; color: #b0764f; }
.yarn-line.hot { border-top-color: #d9534f; }
.yarn-aim { position: absolute; top: 0; bottom: 0; width: 0; border-left: 2px dotted rgb(120 90 60 / 35%); transform: translateX(-1px); pointer-events: none; }
.yarn-held { position: absolute; transform: translateX(-50%); z-index: 2; pointer-events: none; }
.yarn-held.cooling { opacity: .35; }
.yarn-held svg, .yarn-ball svg { width: 100%; height: 100%; display: block; }
.yarn-ball { position: absolute; z-index: 1; pointer-events: none; }
.ball-cat { position: absolute; left: 50%; top: 50%; transform: translate(-50%, -48%); pointer-events: none; }
.yarn-ball :deep(svg.ball-cat), .ball-cat { max-width: 70%; height: auto; }
.yarn-warn { position: absolute; left: 50%; top: 6px; transform: translateX(-50%); padding: 4px 10px; border-radius: 6px; background: #fbe3dc; border: 1px solid #d9534f; color: #a2362f; font-size: 12px; font-weight: 700; white-space: nowrap; z-index: 4; pointer-events: none; }
.yarn-over { position: absolute; inset: 0; z-index: 5; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; background: rgb(255 246 230 / 88%); text-align: center; padding: 0 14px; }
.yarn-over b { font-size: 22px; }
.yarn-over span { font-size: 14px; }
.yarn-status { display: flex; flex-direction: column; justify-content: center; gap: 6px; min-height: 58px; margin: 10px 0; padding: 8px 12px; box-sizing: border-box; border-radius: 8px; background: #f7eedf; font-size: 14px; font-weight: 700; line-height: 1.4; }
.yarn-danger { height: 6px; border-radius: 3px; background: #ecdab3; overflow: hidden; }
.yarn-danger i { display: block; height: 100%; background: #d9534f; }
.yarn-footer { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; }
.yarn-count { font-size: 14px; }
.yarn-count strong { color: #9a5a30; }
.yarn-keyboard { margin: 10px 0 0; font-size: 12px; line-height: 1.5; color: #947451; }
.yarn-keyboard span { display: block; }
/* 中等高度（如 1024×768）：盒子按可用高度缩，面板不出现滚动 */
@media (max-height: 860px) and (min-height: 561px) and (min-width: 600px) {
  .yarn-stage { max-width: calc((100dvh - 312px) * 360 / 540); }
}
/* 横屏矮屏（如手机横着拿）：左边大盒子，右边信息，盒子高度吃满 */
@media (max-height: 560px) and (min-width: 600px) {
  .cat-yarn { padding: 6px; }
  .yarn-panel { display: grid; grid-template-columns: auto minmax(0, 1fr); grid-template-rows: auto auto auto auto 1fr; gap: 6px 14px; padding: 8px 12px; width: min(760px, 100%); align-content: start; }
  .yarn-header { grid-column: 1 / -1; }
  h2 { font-size: 17px; margin-top: 2px; }
  .yarn-eyebrow { display: none; }
  .yarn-body { display: contents; }
  .yarn-stage { grid-column: 1; grid-row: 2 / span 5; width: auto; height: calc(100dvh - 108px); max-width: none; aspect-ratio: 360 / 540; }
  .yarn-side { grid-column: 2; grid-row: 2; flex-direction: row; gap: 6px; }
  .yarn-score, .yarn-next { flex: 1 1 0; }
  .yarn-ladder { display: none; }
  .yarn-status { grid-column: 2; grid-row: 3; margin: 0; min-height: 0; padding: 6px 10px; font-size: 13px; }
  .yarn-footer { grid-column: 2; grid-row: 4; }
  .yarn-keyboard { display: none; }
}
@media (max-width: 599px) {
  .yarn-panel { padding: 10px; border-radius: 12px; }
  h2 { font-size: 19px; }
  .yarn-body { grid-template-columns: 1fr; gap: 8px; }
  .yarn-side { flex-direction: row; align-items: stretch; flex-wrap: wrap; }
  .yarn-score, .yarn-next { flex: 1 1 0; }
  .yarn-ladder { display: none; }
  .yarn-stage { max-width: min(100%, calc((100dvh - 330px) * 360 / 540)); }
  .yarn-status { min-height: 52px; margin: 8px 0; padding: 6px 10px; font-size: 13px; }
  .yarn-keyboard { display: none; }
}
</style>
