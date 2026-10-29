<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { showDialog, showToast, showConfirmDialog } from 'vant'
import CatSprite from '../components/CatSprite.vue'
import PixelRoom from '../components/PixelRoom.vue'
import PixelItem from '../components/PixelItem.vue'
import GardenBackdrop from '../components/GardenBackdrop.vue'
import { decorArt, getSceneLighting, isWallpaper } from '../scene-art.js'
import TimerOverlay from '../components/TimerOverlay.vue'

const API = '/api'

// ===== 状态 =====
const adopted = ref(false)
const species = ref('orange')
const speciesList = ref([])
const adoptSel = ref('orange')
const petNameInput = ref('')
const familyCodeInput = ref('')
const kidNameInput = ref('')

// 领养/登录两步流：step1 输家庭码 → 有孩子则选择（多端登录同一孩子）；无孩子或点"新孩子" → 选猫领养
const adoptStep = ref('code') // code | pick | new
const enterFamily = ref('')
const enterKids = ref([])
async function doEnter(silent) {
  const code = familyCodeInput.value.trim()
  if (!code) return showToast('先输入家庭码哦～')
  const { data } = await jpost('/enter', { code })
  if (data.error) { if (!silent) showToast(data.error); return false }
  enterFamily.value = data.family
  enterKids.value = data.kids || []
  adoptStep.value = enterKids.value.length ? 'pick' : 'new'
  localStorage.setItem('familyCode', code) // 换人/新设备免再输码
  return true
}

// 换人：清本设备登录态，凭记忆的家庭码直接进"你是谁"
async function switchKid() {
  try {
    await showConfirmDialog({ title: '换一个小朋友？', message: '这只小猫会等你回来的～' })
  } catch (e) { return }
  document.cookie = 'kid_token=; Max-Age=0; path=/'
  adopted.value = false
  pet.value = null
  catAnim.value = 'sit'
  const code = localStorage.getItem('familyCode') || ''
  if (code) {
    familyCodeInput.value = code
    await doEnter(true)
  } else {
    adoptStep.value = 'code'
  }
}
async function pickKid(k) {
  if (!k.has_pet) {
    // 还没猫的孩子 → 进领养流程（kid_id 记住，领完猫直接归到这个孩子名下）
    pickedKidID = k.id
    kidNameInput.value = k.name
    adoptStep.value = 'new'
    return
  }
  const { data } = await jpost('/adopt', { code: familyCodeInput.value.trim(), kid_id: k.id })
  if (data.adopted) {
    adopted.value = true
    await loadAll()
    say('我等到你啦！')
    setTimeout(behaviorTick, 800)
  } else if (data.error) {
    showToast(data.error)
  }
}
let pickedKidID = 0 // 选择页进入领养的孩子（0=全新孩子）
function gotoNewKid() { pickedKidID = 0; adoptStep.value = 'new' }
const pet = ref(null)
const today = ref({ date: '', date_multiplier: 1, tasks: [] })
const wishes = ref([])
const shopItems = ref([])
const sceneItems = ref([])

// 猫行为
const catAnim = ref('sit')
const catPos = reactive({ x: 0, y: 0 })
const flipped = ref(false)
const bubble = ref('')
const showBubble = ref(false)
// 开心跳：WAAPI 动画（不销毁重建 DOM，保住位移 transition——修复吃完瞬移）
function hopFx() {
  const inner = catEl.value && catEl.value.querySelector('.cat-inner')
  if (!inner) return
  const fx = flipped.value ? 'scaleX(-1)' : 'none'
  inner.animate(
    [{ transform: fx }, { transform: fx + ' translateY(-22px)' }, { transform: fx }],
    { duration: 600, easing: 'ease' },
  )
}
const zzz = ref(false)

let stateTimer = null
let walking = false

// ===== API =====
async function jget(url) {
  const r = await fetch(API + url)
  if (r.status === 401) { adopted.value = false; return null }
  return r.json()
}
async function jpost(url, body) {
  const r = await fetch(API + url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) })
  return { ok: r.ok, data: await r.json().catch(() => ({})) }
}

const hadDuration = ref({}) // {taskID:bool} 计时任务曾达标（AI 退回后免重新计时）

// ===== 世界视口：固定逻辑世界 + 相机（窄屏看局部可拖，跟猫为默认焦点） =====
const WORLD_W = 1280, WORLD_H = 720
const cam = ref({ x: 0, y: 0 })
const camMode = ref('follow') // follow(跟猫) | free(自由拖动)
const camAnim = ref('') // 相机移动的 CSS transition（跟猫同步时设置）
const sceneLighting = ref(getSceneLighting())
let lightingTimer = null
function syncSceneLighting() { sceneLighting.value = getSceneLighting() }
function onDocumentVisibility() {
  if (!document.hidden) syncSceneLighting()
}
const activeWallpaper = computed(() => sceneItems.value.filter(it => isWallpaper(it.item_key)).at(-1)?.item_key || '')
const placedDecor = computed(() => sceneItems.value.filter(it => !isWallpaper(it.item_key)))
// 图层按地面锚点排序：未来新增装饰沿用同一坐标与注册表。
const decorDepth = it => decorArt(it.item_key).surface === 'wall' ? 2 : 10 + Math.floor(it.y)
const catDepth = computed(() => 10 + Math.floor(catPos.y))
const rootEl = ref(null)
const statusToggleEl = ref(null)
const statusPanelEl = ref(null)
const statusPosition = ref({})
function alignStatusPanel() {
  const button = statusToggleEl.value, root = rootEl.value
  if (!button || !root) return
  const anchor = button.getBoundingClientRect(), bounds = root.getBoundingClientRect()
  statusPosition.value = { top: `${anchor.bottom - bounds.top + 12}px`,
    right: `${bounds.right - anchor.right}px`, width: `${Math.min(340, bounds.width - 32)}px` }
}
async function toggleStatus() {
  hudOpen.value = !hudOpen.value
  tipShow.value = false
  toolsOpen.value = false
  if (hudOpen.value) { await nextTick(); alignStatusPanel() }
}
function dismissStatusOutside(e) {
  if (!statusPanelEl.value?.contains(e.target) && !statusToggleEl.value?.contains(e.target)) {
    hudOpen.value = false
    tipShow.value = false
  }
}
const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v))
// 房间世界坐标以猫脚底为锚点；视口边框不计入可见区域。
function cameraBounds() {
  const el = roomEl.value
  if (!el) return null
  const width = el.clientWidth, height = el.clientHeight
  const centerX = Math.max(0, (width - WORLD_W) / 2)
  // 手机允许少量底部留白，让猫身体中心避开导航，而非硬贴世界下沿。
  const minY = isNarrow.value ? Math.min(0, height - WORLD_H, -220) : Math.min(0, height - WORLD_H)
  return { width, height, minX: width >= WORLD_W ? centerX : width - WORLD_W,
    maxX: centerX, minY, maxY: Math.max(0, (height - WORLD_H) / 2) }
}
function setCamera(x, y) {
  const b = cameraBounds()
  if (b) cam.value = { x: clamp(x, b.minX, b.maxX), y: clamp(y, b.minY, b.maxY) }
}
function centerOn(x, y, durSec) {
  const b = cameraBounds()
  if (!b) return
  const focusY = b.height / 2 // 视口已避开标题与导航，无需再扣一次安全区
  camAnim.value = durSec != null ? `transform ${durSec}s linear` : 'transform .45s ease'
  setCamera(b.width / 2 - x, focusY - y)
}
function resetCam() { camMode.value = 'follow'; centerOn(catPos.x, catPos.y - 48) }

async function loadAll() {
  const [sp, p, t, w, s, sc] = await Promise.all([
    jget('/species'), jget('/pet'), jget('/tasks/today'), jget('/wish/list'), jget('/shop'), jget('/scene'),
  ])
  if (sp) speciesList.value = sp.species || []
  if (p && p.pet) { adopted.value = true; pet.value = p.pet; species.value = p.pet.species }
  if (t) today.value = t
  hadDuration.value = t?.had_duration || {}
  if (t && t.tasks) today.value.tasks = t.tasks.map(x => ({ ...x, snapshot: typeof x.snapshot === 'string' ? JSON.parse(x.snapshot) : x.snapshot }))
  if (w) wishes.value = w.wishes || []
  if (s) shopItems.value = s.items || []
  if (sc) sceneItems.value = sc.items || []
}

async function refreshPet() {
  const p = await jget('/pet')
  if (p && p.pet) pet.value = p.pet
}

// ===== 领养 =====
async function doAdopt() {
  const name = petNameInput.value.trim() || '橘子'
  const code = familyCodeInput.value.trim()
  const kidName = kidNameInput.value.trim() || '宝贝'
  const payload = { code, species: adoptSel.value, name }
  if (pickedKidID) payload.kid_id = pickedKidID // 已有孩子补领猫
  else payload.kid_name = kidName
  const { data } = await jpost('/adopt', payload)
  if (data.adopted) {
    adopted.value = true
    await loadAll()
    say(`我是${name}！以后请多多关照喵～`)
    setTimeout(behaviorTick, 1000)
  } else if (data.error) {
    showToast(data.error)
  }
}

// ===== 猫行为状态机（房间内走动/躺/睡） =====
function randTarget() {
  return { x: 80 + Math.random() * (WORLD_W - 200), y: (0.62 + Math.random() * 0.32) * WORLD_H }
}
// 校准：catPos ← 猫的实际渲染位置（DOM 和数据有偏差时原地归位，杜绝瞬移）
function syncCatPos() {
  if (!catEl.value || !roomEl.value) return
  const cr = catEl.value.getBoundingClientRect()
  const rr = roomEl.value.getBoundingClientRect()
  const worldRect = worldEl.value.getBoundingClientRect()
  catPos.x = cr.left - worldRect.left + 55
  catPos.y = cr.top - worldRect.top + 100
}
// 打断猫当前行为（喂食/触摸可抢占自主走动）
function interrupt() {
  clearTimeout(stateTimer)
  walking = false
}
async function walkTo(target, done, opts = {}) {
  interrupt() // 抢占
  const el = catEl.value
  if (!el) return
  syncCatPos() // 先校准到真实位置
  el.style.transition = 'none'
  await nextTick() // 等 Vue 把校准位置 patch 到 DOM
  void el.offsetWidth // 强制 reflow 固化
  walking = true
  catAnim.value = 'walk'
  // 像素帧头朝左：向右走才需要镜像翻转（修复屁股朝前）
  flipped.value = target.x > catPos.x
  const dist = Math.hypot(target.x - catPos.x, target.y - catPos.y)
  const speed = opts.speed || (pet.value && pet.value.hunger < 30 ? 55 : 100)
  const dur = Math.max(200, (dist / speed) * 1000)
  el.style.transition = `left ${dur / 1000}s linear, top ${dur / 1000}s linear`
  // 相机跟猫同步走（follow 模式）
  if (camMode.value === 'follow') centerOn(target.x, target.y - 48, dur / 1000)
  requestAnimationFrame(() => { catPos.x = target.x; catPos.y = target.y })
  clearTimeout(stateTimer)
  stateTimer = setTimeout(() => { catAnim.value = 'sit'; walking = false; done && done() }, dur + 60)
}
function lieDown() { zzz.value = false; catAnim.value = 'sleep' }
function behaviorNext(delay) {
  stateTimer = setTimeout(behaviorTick, delay || (600 + Math.random() * 1200))
}
function behaviorTick() {
  if (!adopted.value) return
  const hungry = pet.value && pet.value.hunger < 30
  const r = Math.random()
  // 道具互动：饱腹时 35% 概率找植物/毛线球/猫窝玩（饿了没力气玩）
  const playables = sceneItems.value.filter((it) => ['plant', 'yarn', 'nest'].includes(it.item_key))
  if (!hungry && playables.length && r < 0.35) {
    interactWith(playables[Math.floor(Math.random() * playables.length)])
    return
  }
  if (r < 0.55) {
    walkTo(randTarget(), () => behaviorNext())
  } else if (r < 0.8) {
    // 有猫窝 → 走到窝里睡；否则原地睡
    const nest = sceneItems.value.find((it) => it.item_key === 'nest')
    if (nest) {
      walkTo({ x: nest.x, y: nest.y }, () => { lieDown(); behaviorNext(3500 + Math.random() * 4000) })
    } else {
      lieDown()
      behaviorNext(3000 + Math.random() * 4000)
    }
  } else {
    if (Math.random() < 0.4) say()
    behaviorNext(2000)
  }
}

// ===== 道具互动 =====
const shakeId = ref(0) // 被咬的植物晃动
function facePoint(px) { flipped.value = px > catPos.x } // 面向目标
function interactWith(it) {
  if (!it || !it.item_key) return
  const beside = it.item_key === 'yarn' ? 44 : 52
  walkTo({ x: it.x + (catPos.x < it.x ? beside : -beside), y: it.y }, () => {
    facePoint(it.x)
    if (it.item_key === 'plant') {
      // 咬植物：吃帧 + 植物晃动
      catAnim.value = 'eat'
      say('咔嚓咔嚓…这盆草真好吃！')
      shakeId.value = it.id
      stateTimer = setTimeout(() => {
        shakeId.value = 0
        catAnim.value = 'sit'
        hearts(2)
        say('真新鲜！')
        behaviorNext()
      }, 2600)
    } else if (it.item_key === 'yarn') {
      playYarn(it, 0)
    } else if (it.item_key === 'nest') {
      catAnim.value = 'sleep'
      say('呼…呼…')
      stateTimer = setTimeout(() => { catAnim.value = 'sit'; behaviorNext() }, 4000)
    }
  })
}
// 追毛线球：球滚开（本地位置动画）→ 猫追上 → 最多 3 轮 → 保存新位置
function playYarn(it, round) {
  say('推球球！咕噜咕噜～')
  const W = WORLD_W
  const dir = Math.random() < 0.5 ? -1 : 1
  const nx = Math.max(60, Math.min(W - 60, it.x + dir * (80 + Math.random() * 60)))
  // 球滚走（CSS transition 平滑）
  const local = sceneItems.value.find((x) => x.id === it.id)
  if (local) local.x = nx
  catAnim.value = 'walk'
  flipped.value = nx > catPos.x
  walkTo({ x: nx + (dir > 0 ? -40 : 40), y: it.y }, () => {
    catAnim.value = 'sit'
    hearts(2)
    if (round < 2 && Math.random() < 0.6) {
      stateTimer = setTimeout(() => playYarn({ ...it, x: nx }, round + 1), 600)
    } else {
      // 玩完：保存毛线球新位置
      jpost('/scene/place', { id: it.id, key: 'yarn', x: nx, y: it.y }).then(() => loadAll())
      say('抓到你啦！'
      )
      behaviorNext(1800)
    }
  })
}

const roomEl = ref(null) // 可拖动的场景视口，不包含导航与侧栏
const worldEl = ref(null)
const catEl = ref(null)

// CSS 和 JS 使用同一断点，首帧同步判断；不能用场景宽度判断设备宽度。
const narrowMedia = window.matchMedia('(max-width: 900px)')
const isNarrow = ref(narrowMedia.matches)
const hudOpen = ref(false)
const toolsOpen = ref(false)
const sheet = ref('') // '' / 'task' / 'wish' / 'shop'
function openSheet(name) {
  sheet.value = sheet.value === name ? '' : name
  hudOpen.value = false
  toolsOpen.value = false
}
function toggleCompanion() {
  toolsOpen.value = !toolsOpen.value
  sheet.value = ''
  hudOpen.value = false
}
function checkNarrow() { isNarrow.value = narrowMedia.matches }
function onViewportResize() {
  checkNarrow()
  nextTick(() => {
    if (hudOpen.value) alignStatusPanel()
    if (camMode.value === 'follow' && adopted.value) centerOn(catPos.x, catPos.y - 48, 0)
    else setCamera(cam.value.x, cam.value.y)
  })
}
let viewportObserver = null

// ===== 语音输入：按住录音 → 转码 16k WAV → /api/asr → 文字填框 =====
const recState = ref('idle') // idle | recording | uploading
let mediaRec = null, recChunks = [], recStream = null

async function recStart() {
  if (recState.value !== 'idle' || chatStreaming.value) return
  try {
    recStream = await navigator.mediaDevices.getUserMedia({ audio: true })
  } catch (e) { return showToast('麦克风没开权限哦，去设置里打开～') }
  recChunks = []
  mediaRec = new MediaRecorder(recStream)
  mediaRec.ondataavailable = (e) => recChunks.push(e.data)
  mediaRec.onstop = onRecStop
  mediaRec.start()
  recState.value = 'recording'
}
function recStop() {
  if (recState.value !== 'recording') return
  recState.value = 'uploading'
  try { mediaRec.stop() } catch (e) { recState.value = 'idle' }
}

// 录音 Blob → 16kHz 单声道 WAV（全浏览器格式归一：Safari mp4 / Chrome webm / Firefox ogg 都解进来重编）
async function blobToWav16k(blob) {
  const arr = await blob.arrayBuffer()
  const ctx = new AudioContext({ sampleRate: 16000 })
  const audio = await ctx.decodeAudioData(arr) // 浏览器解码自己录的格式
  // 混单声道
  let ch = audio.getChannelData(0)
  if (audio.numberOfChannels > 1) {
    const ch2 = audio.getChannelData(1)
    for (let i = 0; i < ch.length; i++) ch[i] = (ch[i] + ch2[i]) / 2
  }
  const buf = ctx.createBuffer(1, ch.length, 16000)
  buf.copyToChannel ? buf.copyToChannel(ch, 0) : buf.getChannelData(0).set(ch)
  // 上采样重采样：用 OfflineAudioContext 保证 16k
  const off = new OfflineAudioContext(1, Math.ceil(audio.duration * 16000), 16000)
  const src = off.createBufferSource()
  src.buffer = audio
  src.connect(off.destination)
  src.start()
  const rendered = await off.startRendering()
  const data = rendered.getChannelData(0)
  // WAV 编码（44 字节头 + PCM16）
  const len = data.length
  const wav = new ArrayBuffer(44 + len * 2)
  const view = new DataView(wav)
  const ws = (o, str) => { for (let i = 0; i < str.length; i++) view.setUint8(o + i, str.charCodeAt(i)) }
  ws(0, 'RIFF'); view.setUint32(4, 36 + len * 2, true); ws(8, 'WAVE')
  ws(12, 'fmt '); view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true)
  view.setUint32(24, 16000, true); view.setUint32(28, 32000, true); view.setUint16(32, 2, true); view.setUint16(34, 16, true)
  ws(36, 'data'); view.setUint32(40, len * 2, true)
  let o = 44
  for (let i = 0; i < len; i++, o += 2) {
    const v = Math.max(-1, Math.min(1, data[i]))
    view.setInt16(o, v < 0 ? v * 0x8000 : v * 0x7FFF, true)
  }
  ctx.close()
  return new Blob([wav], { type: 'audio/wav' })
}

async function onRecStop() {
  recStream.getTracks().forEach((t) => t.stop())
  const blob = new Blob(recChunks, { type: recChunks[0]?.type || 'audio/webm' })
  if (blob.size < 2000) { // 太短（<~0.1s）忽略
    recState.value = 'idle'
    return
  }
  try {
    const wav = await blobToWav16k(blob)
    const fd = new FormData()
    fd.append('file', wav, 'rec.wav')
    const r = await fetch('/api/asr', { method: 'POST', body: fd })
    const d = await r.json()
    if (d.text) {
      chatInput.value = (chatInput.value ? chatInput.value + ' ' : '') + d.text
      if (navigator.vibrate) navigator.vibrate(60) // 转写完成轻震
    } else showToast(d.error || '猫没听清，再说一次？')
  } catch (e) {
    showToast('网络卡了喵…')
  }
  recState.value = 'idle'
}

// ===== 猫聊天（SSE 流式打字机） =====
const chatOpen = ref(false)
const chatMessages = ref([]) // {role, content}
const chatInput = ref('')
const chatStreaming = ref(false)
const chatLeft = ref(30)

async function openChat() {
  toolsOpen.value = false
  hudOpen.value = false
  sheet.value = ''
  chatOpen.value = true
  if (chatMessages.value.length === 0) {
    const d = await jget('/chat/history')
    chatMessages.value = d.messages || []
    chatLeft.value = d.left_today ?? 30
    if (!chatMessages.value.length) {
      chatMessages.value = [{ role: 'assistant', content: '喵～想和我聊什么呀？' }]
    }
  }
  if (adopted.value) say('聊点什么呀～')
}

async function sendChat() {
  const text = chatInput.value.trim()
  if (!text || chatStreaming.value) return
  if (chatLeft.value <= 0) return showToast('今天聊得够多啦，猫要睡觉了喵～明天再来')
  chatInput.value = ''
  chatMessages.value.push({ role: 'user', content: text })
  chatLeft.value--
  chatStreaming.value = true
  const reply = { role: 'assistant', content: '' }
  chatMessages.value.push(reply)

  // fetch + ReadableStream 解析 SSE（EventSource 不支持 POST）
  try {
    const r = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: text }),
    })
    if (r.status === 429) {
      const e = await r.json()
      reply.content = e.error || '今天聊够啦～'
      chatStreaming.value = false
      return
    }
    const reader = r.body.getReader()
    const dec = new TextDecoder()
    let buf = ''
    catAnim.value = 'eat' // 说话时张嘴动画
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.startsWith('data:')) continue
        try {
          const d = JSON.parse(line.slice(5).trim())
          if (d.d) { reply.content += d.d; chatMessages.value = [...chatMessages.value] }
          if (d.error) { reply.content = d.error }
        } catch (e) { /* 半行 */ }
      }
    }
    catAnim.value = 'sit'
  } catch (e) {
    reply.content = reply.content || '喵呜…卡住了，再试一次？'
    catAnim.value = 'sit'
  }
  chatStreaming.value = false
}

// 触摸交互：点地面 → 猫快速跑过去；点物品 → 跑过去互动
let dragCam = null // 视角拖动状态
function onRoomTap(e) {
  if (!adopted.value || e.button !== 0) return
  if (e.target.closest('button, input, .cat-wrap, .inv-bar')) return
  hudOpen.value = false
  toolsOpen.value = false
  const r = roomEl.value.getBoundingClientRect()
  // 视角拖动：按下后移动超阈值 → 拖动视角（不点地）
  dragCam = { sx: e.clientX, sy: e.clientY, camx: cam.value.x, camy: cam.value.y, moved: false }
  const move = (ev) => {
    if (!dragCam) return
    const dx = ev.clientX - dragCam.sx, dy = ev.clientY - dragCam.sy
    if (!dragCam.moved && Math.hypot(dx, dy) < 12) return
    dragCam.moved = true
    camMode.value = 'free'
    camAnim.value = 'none'
    setCamera(dragCam.camx + dx, dragCam.camy + dy)
  }
  const up = (ev) => {
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', up)
    document.removeEventListener('pointercancel', up)
    const wasDrag = dragCam && dragCam.moved
    dragCam = null
    if (wasDrag) return // 拖了视角就不点地
    // 点地（世界坐标 = 屏幕 - 视口原点 - 相机平移）
    if (ev.type === 'pointercancel') return
    const worldRect = worldEl.value.getBoundingClientRect()
    const x = clamp(ev.clientX - worldRect.left, 70, WORLD_W - 90)
    const y = clamp(ev.clientY - worldRect.top, WORLD_H * 0.62, WORLD_H - 20)
    if (placingFeed.value) { placeBowlAndGo(x, y); return } // 放置猫粮
    say(['来啦来啦！', '等等我～', '马上去！'][Math.floor(Math.random() * 3)])
    walkTo({ x, y }, () => behaviorNext(1500), { speed: 220 }) // 快速跑
  }
  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', up)
  document.addEventListener('pointercancel', up)
}

const TALK = ['喵～陪我玩！', '今天任务做完了吗？', '我在守护这个房间！', '你是最棒的主人！']
function say(t) {
  bubble.value = t || TALK[Math.floor(Math.random() * TALK.length)]
  showBubble.value = true
  setTimeout(() => (showBubble.value = false), 2200)
}
function hearts(n) { hopFx() }

// ===== 交互 =====
async function petCat() {
  if (!adopted.value) return
  const { data } = await jpost('/pet/touch')
  if (data.touch_left_today === 0 && data.touch_left_today !== undefined) showToast('今天摸够啦，小猫要被摸秃了～')
  hearts(3)
  say(['好舒服呀～', '喵呜！', '最喜欢你啦！'][Math.floor(Math.random() * 3)])
  await refreshPet()
}

// 喂食流程：点按钮 → 放置模式（点地面放碗）→ 猫跑过去吃 → 吃完才调后端结算
const placingFeed = ref(false)
let eating = false
function feed() {
  if (!adopted.value || eating) return
  if (placingFeed.value) { placingFeed.value = false; say('不喂了吗？'); return } // 再点一次=取消
  placingFeed.value = true
  toolsOpen.value = false
  say('猫粮放哪儿？点一下地面～')
}
function placeBowlAndGo(x, y) {
  placingFeed.value = false
  eating = true
  bowlPos.x = x
  bowlPos.y = y
  showBowl.value = true
  say('闻到香味啦！')
  // 猫小跑到碗边（停左侧，头朝碗）
  walkTo({ x: x - 58, y: y }, () => {
    flipped.value = true // 面向右侧的碗
    catAnim.value = 'eat'
    say('开动咯！')
    setTimeout(async () => {
      // 吃完才请求后端（星星不够 → 猫失望）
      const { ok, data } = await jpost('/pet/feed')
      showBowl.value = false
      catAnim.value = 'sit'
      eating = false
      if (!ok) {
        say(data.error || '星星不够啦…')
      } else {
        pet.value = data.pet
        hearts(6)
        say('真好吃！谢谢你！')
      }
      behaviorNext(1500)
    }, 2300)
  }, { speed: 170 })
}
const showBowl = ref(false)
const bowlPos = reactive({ x: 0, y: 0 })

// ===== 任务 =====
const submitting = ref(0)
async function submitTask(t, body = {}) {
  if (submitting.value) return
  submitting.value = t.id
  const snap = t.snapshot
  const payload = {
    duration_sec: body.duration_sec ?? (snap.goal_type === 'duration' ? snap.goal_value : 0) * 60,
    extra_sec: body.duration_sec || 0,
    evidence: body.evidence || [],
  }
  // b 模式：显示"猫在看作业"状态层（后端判定 ~20 秒，期间锁操作）
  const isB = (t.snapshot && t.snapshot.confirm_mode || '').includes('b')
  if (isB) aiThinking.value = true

  const { ok, data } = await jpost(`/tasks/${t.id}/submit`, payload)
  submitting.value = 0
  aiThinking.value = false

  // AI 反馈写入猫聊天面板（可回看，不再一闪而过）
  const aiFeedback = data && (data.kid_feedback || data.ai_reason)
  if (isB && aiFeedback) {
    const stars = data.ai_score ? '⭐'.repeat(Math.max(1, Math.min(5, data.ai_score))) + ' ' : ''
    const content = stars + aiFeedback
    chatMessages.value.push({ role: 'assistant', content })
    chatOpen.value = true // 自动打开对话框看反馈
    setTimeout(() => {
      const el = document.getElementById('chatBody')
      if (el) el.scrollTop = el.scrollHeight
    }, 100)
  }
  if (data.settled) {
    showToast('任务完成！')
    hearts(6)
    say('主人真棒！')
    await loadAll()
  } else if (data.waiting && data.ai_pass) {
    // b 模式：AI 初审通过 → 等家长终审（score 模式带星级与猫反馈）
    const stars = data.ai_score ? '⭐'.repeat(Math.max(1, Math.min(5, data.ai_score))) + ' ' : ''
    showToast(stars + '猫觉得你做得很好！等妈妈最后确认～')
    hearts(4)
    say(data.kid_feedback || `猫看了你的作业：${data.ai_reason || '做得不错'}！等妈妈最后看一眼～`)
    await loadAll()
  } else if (data.waiting) {
    showToast('提交成功，等妈妈确认哦～')
    say('等妈妈看看～')
    await loadAll()
  } else if (data.redo) {
    // b 模式：AI 觉得不够 → 温柔退回（无限重试）
    showToast('猫觉得还可以再试一次喵～')
    say(data.kid_feedback || `咦…${data.ai_reason || '再认真一点'}。再试一次，我在这里等你喵！`)
    await loadAll()
  } else {
    showToast(data.error || '提交失败')
  }
}

// ===== 任务入口分流：计时 / 拍照 / 录音 / 直接打卡 =====
const timerTask = ref(null) // 打开全屏计时页的任务
function startTask(t) {
  const snap = t.snapshot
  if (snap.goal_type === 'duration' && snap.goal_value > 0) {
    // 计时曾达标（材料被 AI 退回重做）→ 免重新计时，直接补材料
    if (!hadDuration.value[t.id]) {
      timerTask.value = t
      return
    }
  }
  // 新流程：配置了提交步骤 → 向导（逐步引导拍照/录音）
  const steps = parseSteps(snap)
  if (steps.length > 0) {
    openWizard(t, steps)
    return
  }
  const ev = snap.evidence_type || ''
  if (ev.includes('photo') || ev.includes('audio') || ev.includes('video')) {
    evidenceTask = t
    evidenceInput.value && evidenceInput.value.click()
    return
  }
  submitTask(t)
}

// ===== 提交向导（多步骤证据） =====
const wizard = ref(null) // { task, steps, done: {} }
function parseSteps(snap) {
  try {
    const arr = typeof snap.evidence_steps === 'string' ? JSON.parse(snap.evidence_steps) : (snap.evidence_steps || [])
    return Array.isArray(arr) && arr.length ? arr : []
  } catch (e) { return [] }
}
function openWizard(t, steps, durationSec) {
  wizard.value = { task: t, steps, done: {}, duration: durationSec || 0 } // done[i] = [{type,path},...]（每步可多文件）
  say('跟着提示一步步来哦～')
}
function wizardDoneCount(w) {
  return w.steps.filter((st) => w.done[st.kind + '_' + w.steps.indexOf(st)]).length
}
const wizardInput = ref(null)
let wizardStepIdx = -1
function wizardPick(st, idx) {
  wizardStepIdx = idx
  if (wizardInput.value) {
    wizardInput.value.accept = st.kind === 'photo' ? 'image/*' : 'audio/*'
    wizardInput.value.multiple = true // 支持多张（如多页口算）
    wizardInput.value.click()
  }
}
// 任何格式图片 → 压缩 JPEG（HEIC 归一 + 长边 2048 + 质量 0.85，AI vision 只认 jpg/png/webp）
async function normalizeImage(file) {
  if (!file.type.startsWith('image')) return file
  try {
    const bitmap = await createImageBitmap(file)
    const MAX = 2048
    const scale = Math.min(1, MAX / Math.max(bitmap.width, bitmap.height))
    const w = Math.round(bitmap.width * scale), h = Math.round(bitmap.height * scale)
    const cv = document.createElement('canvas')
    cv.width = w; cv.height = h
    cv.getContext('2d').drawImage(bitmap, 0, 0, w, h)
    bitmap.close()
    const blob = await new Promise((res) => cv.toBlob(res, 'image/jpeg', 0.85))
    if (blob && blob.size > 0 && blob.size < file.size) {
      return new File([blob], (file.name || 'photo').replace(/\.[^.]+$/, '') + '.jpg', { type: 'image/jpeg' })
    }
  } catch (e) { /* 解码失败（如 SVG 动图）→ 原样返回 */ }
  return file
}

async function onWizardFile(e) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  if (!files.length || !wizard.value) return
  const w = wizard.value
  const idx = wizardStepIdx
  if (!w.steps[idx]) return
  showToast(files.length > 1 ? `上传 ${files.length} 个文件中…` : '上传中…')
  for (const f0 of files) {
    const file = await normalizeImage(f0)
    const fd = new FormData()
    fd.append('file', file)
    try {
      const r = await fetch('/api/upload', { method: 'POST', body: fd })
      const d = await r.json()
      if (!d.path) continue
      const type = (file.type || '').startsWith('image') ? 'photo' : (file.type || '').startsWith('video') ? 'video' : 'audio'
      if (!w.done[idx]) w.done[idx] = []
      w.done[idx].push({ type, path: d.path })
    } catch (err) { /* 单个失败不阻塞 */ }
  }
  if (!w.done[idx] || !w.done[idx].length) { showToast('上传失败，再试一次？'); return }
  wizard.value = { ...w, done: { ...w.done } }
}
function wizardRemoveFile(idx, fi) {
  const w = wizard.value
  if (!w) return
  w.done[idx].splice(fi, 1)
  if (!w.done[idx].length) delete w.done[idx]
  wizard.value = { ...w, done: { ...w.done } }
}
function wizardReady(w) {
  return !w.steps.some((st, i) => st.required && !(w.done[i] && w.done[i].length))
}
const aiThinking = ref(false) // b 模式：AI 分析中（全屏状态层 + 锁操作）
let aiThinkTimer = null
function previewPhoto(url) {
  photoPreview.value = url
}
const photoPreview = ref(null)

async function wizardSubmit() {
  const w = wizard.value
  const evs = w.steps.map((st, i) => w.done[i] || []).flat()
  const t = w.task
  const dur = w.duration || (t.snapshot.goal_type === 'duration' && t.snapshot.goal_value > 0 ? t.snapshot.goal_value * 60 : 0)
  wizard.value = null
  await submitTask(t, { evidence: evs, duration_sec: dur })
}
async function onTimerDone(sec) {
  const t = timerTask.value
  timerTask.value = null
  if (!t) return
  // 计时达标 + 配置了证据步骤 → 转向导补材料（duration 暂存随提交带上）
  const steps = parseSteps(t.snapshot)
  if (steps.length > 0) {
    openWizard(t, steps, sec)
    return
  }
  const ev = t.snapshot.evidence_type || ''
  if (ev.includes('photo') || ev.includes('audio') || ev.includes('video')) {
    evidenceTask = t
    timerSec = sec
    evidenceInput.value && evidenceInput.value.click()
    return
  }
  await submitTask(t, { duration_sec: sec })
}
let timerSec = 0
function onTimerGiveup() { timerTask.value = null }

// 拍照/录音（file input：HTTP 局域网下摄像头 API 不可用，iPad 会调起相机/录音）
const evidenceInput = ref(null)
let evidenceTask = null
async function onEvidenceFile(e) {
  const f0 = e.target.files && e.target.files[0]
  e.target.value = ''
  if (!f0 || !evidenceTask) return
  const file = await normalizeImage(f0)
  const fd = new FormData()
  fd.append('file', file)
  showToast('上传中…')
  const r = await fetch('/api/upload', { method: 'POST', body: fd })
  const d = await r.json()
  if (!d.path) { showToast('上传失败，再试一次？'); return }
  const type = (file.type || '').startsWith('image') ? 'photo' : (file.type || '').startsWith('video') ? 'video' : 'audio'
  const t = evidenceTask
  evidenceTask = null
  const dur = timerSec || (t.snapshot.goal_type === 'duration' ? t.snapshot.goal_value * 60 : 0)
  timerSec = 0
  await submitTask(t, { evidence: [{ type, path: d.path }], duration_sec: dur })
}
function taskBtnLabel(snap) {
  if (snap.goal_type === 'duration' && snap.goal_value > 0) return '计时开始'
  const ev = snap.evidence_type || ''
  if (ev.includes('photo')) return '拍照提交'
  if (ev.includes('audio')) return '录音提交'
  if (ev.includes('video')) return '录像提交'
  return '打卡'
}

// ===== 愿望 =====
const wishModal = ref(false)
const wishForm = reactive({ title: '', hoped: '' })
function newWish() {
  wishForm.title = ''
  wishForm.hoped = ''
  wishModal.value = true
  say('说说你想要什么～')
}
async function submitWish() {
  if (!wishForm.title.trim()) return showToast('先写上想要什么哦')
  const hoped = parseInt(wishForm.hoped) || 0
  await jpost('/wish', { title: wishForm.title.trim(), hoped_price: hoped })
  wishModal.value = false
  showToast('许愿成功！等妈妈定价～')
  await loadAll()
}
async function cancelWish(w) {
  await jpost(`/wish/${w.id}/cancel`)
  showToast('撤回了这个愿望')
  await loadAll()
}

// ===== 商店/场景 =====
async function buyItem(it) {
  if (it.owned) return showToast('已经买过啦')
  const { ok, data } = await jpost('/shop/buy', { key: it.key })
  if (!ok) return showToast(data.error || '购买失败')
  showToast(`买到${it.name}啦！从下面的物品栏拖进房间～`)
  await loadAll()
}

// 已购未摆放的道具（物品栏）
const inventory = computed(() =>
  shopItems.value.filter((i) => i.owned && !isWallpaper(i.key) && !sceneItems.value.find((s) => s.item_key === i.key)))
const wallpaperSaving = ref(false)
async function applyWallpaper(it) {
  if (!it.owned || wallpaperSaving.value || activeWallpaper.value === it.key) return
  wallpaperSaving.value = true
  try {
    // 沿用场景持久化接口，壁纸是主题层，不伪装成可拖动家具。
    const { ok, data } = await jpost('/scene/place', { key: it.key, x: 1, y: 1 })
    if (!ok) return showToast(data.error || '更换壁纸失败')
    const previous = sceneItems.value.filter(item => isWallpaper(item.item_key))
    for (const item of previous) {
      const removed = await jpost('/scene/remove', { id: item.id })
      if (!removed.ok) { showToast('壁纸已换好，旧记录清理未完成'); break }
    }
    await loadAll()
    showToast('新壁纸换好啦')
  } catch {
    showToast('网络暂时不通，稍后再换壁纸')
  } finally { wallpaperSaving.value = false }
}
function shopAction(it) {
  if (!it.owned) return buyItem(it)
  if (isWallpaper(it.key)) return applyWallpaper(it)
  if (isNarrow.value) { sheet.value = ''; toolsOpen.value = true }
  showToast('从物品栏拖进房间；拖到边缘可以收回')
}

// 拖拽：物品栏拖入房间（新摆放）；场景内拖动改位置；拖到边缘收回
let cancelDecorDrag = null
function onItemPointerDown(e, it, sceneId) {
  if (e.button !== 0) return
  e.preventDefault()
  e.stopPropagation()
  cancelDecorDrag?.()
  const drag = { id: sceneId || 0, key: it.key || it.item_key, moved: false, pointerId: e.pointerId }
  const art = decorArt(drag.key)
  const ghost = document.createElement('div')
  ghost.className = 'drag-ghost'
  const sprite = e.currentTarget.querySelector('svg')?.cloneNode(true)
  if (sprite) { sprite.style.width = '72px'; sprite.style.height = '72px'; ghost.appendChild(sprite) }
  document.body.appendChild(ghost)
  const cleanup = () => {
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', up)
    document.removeEventListener('pointercancel', up)
    ghost.remove()
    cancelDecorDrag = null
  }
  const move = ev => {
    if (ev.pointerId !== drag.pointerId || Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY) < 8) return
    drag.moved = true
    ghost.style.left = `${ev.clientX - 36}px`
    ghost.style.top = `${ev.clientY - 72}px`
    ghost.style.opacity = 1
  }
  const up = async ev => {
    if (ev.pointerId !== drag.pointerId) return
    cleanup()
    if (ev.type === 'pointercancel') return
    if (!drag.moved) {
      if (drag.id && ['plant', 'nest', 'yarn'].includes(drag.key)) interactWith(it)
      else showToast('按住拖动来摆放，拖到房间边缘可以收回')
      return
    }
    const viewport = roomEl.value, r = viewport.getBoundingClientRect()
    const vx = ev.clientX - r.left - viewport.clientLeft, vy = ev.clientY - r.top - viewport.clientTop
    const worldRect = worldEl.value.getBoundingClientRect()
    const x = clamp(ev.clientX - worldRect.left, art.size / 2, WORLD_W - art.size / 2)
    const y = art.surface === 'wall'
      ? clamp(ev.clientY - worldRect.top, art.size + 20, 380)
      : clamp(ev.clientY - worldRect.top, WORLD_H * 0.62, WORLD_H - 20)
    try {
      let result
      if (vx < 16 || vx > viewport.clientWidth - 16 || vy < 16 || vy > viewport.clientHeight - 16) {
        if (!drag.id) return
        result = await jpost('/scene/remove', { id: drag.id })
      } else result = await jpost('/scene/place', { id: drag.id, key: drag.key, x, y })
      if (!result.ok) return showToast(result.data.error || '摆放没有保存，请再试一次')
      await loadAll()
    } catch { showToast('网络暂时不通，原来的装饰没有丢失') }
  }
  cancelDecorDrag = cleanup
  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', up)
  document.addEventListener('pointercancel', up)
}

const moodColor = computed(() => (!pet.value ? '' : pet.value.hunger < 30 || pet.value.mood < 30 ? 'sad' : ''))

// ===== HUD 提示（桌面悬停 / 触屏长按） =====
const TIPS = {
  stars: '⭐ 星星币：完成任务获得。喂猫、买道具、兑换愿望都花它。花的星星另有隐藏账本（累计获得），只涨不掉，决定猫的等级。',
  level: '🏆 等级：由「累计获得的星星」驱动，只涨不掉。升级会解锁新玩法（装扮、新动作）。',
  streak: '🔥 连击：连续完成任务的周天数。奖励加成 = 1 + 0.1×连击（封顶1.5倍）。当天任务没完成会清零，请假不断。',
  hunger: '🍽 饱腹度：猫会随时间慢慢变饿（约两天见底）。喂猫粮一次 +30。低于 30 猫会蔫蔫的、不爱动。',
  mood: '💚 心情：完成任务 +10，摸摸头 +5（每天限 3 次）。低于 30 猫趴着不理人。',
}
const tipText = ref('')
const tipShow = ref(false)
let tipTimer = null
function showTip(key) {
  tipText.value = TIPS[key] || ''
  tipShow.value = true
}
function hideTip() {
  clearTimeout(tipTimer)
  tipShow.value = false
}
// 桌面鼠标：悬停即显；触屏：按住 600ms 弹出
function tipDown(key, ev) {
  if (ev.pointerType === 'mouse') return // 走 hover
  tipTimer = setTimeout(() => showTip(key), 600)
}
function tipUp() { clearTimeout(tipTimer) }

function initCat() {
  if (!roomEl.value) return
  catPos.x = WORLD_W * 0.55
  catPos.y = WORLD_H * 0.82
  camMode.value = 'follow'
  centerOn(catPos.x, catPos.y - 48, 0)
  viewportObserver?.disconnect()
  viewportObserver = new ResizeObserver(onViewportResize)
  viewportObserver.observe(roomEl.value)
  clearTimeout(stateTimer)
  stateTimer = setTimeout(behaviorTick, 1200)
}
onMounted(async () => {
  window.addEventListener('resize', onViewportResize)
  document.addEventListener('pointerdown', dismissStatusOutside)
  document.addEventListener('visibilitychange', onDocumentVisibility)
  syncSceneLighting()
  lightingTimer = setInterval(syncSceneLighting, 60000)
  await loadAll()
  await nextTick()
  if (adopted.value) {
    initCat()
  } else {
    // 换设备/换人回来：记住过家庭码则免输码直达"你是谁"
    const code = localStorage.getItem('familyCode')
    if (code) {
      familyCodeInput.value = code
      await doEnter(true)
    }
  }
  checkNarrow()
})
// 领养完成 → 初始化猫位置（修复：猫从左上角 0,0 起步的问题）
watch(adopted, (v) => { if (v) setTimeout(initCat, 100) })
onBeforeUnmount(() => {
  clearTimeout(stateTimer)
  viewportObserver?.disconnect()
  window.removeEventListener('resize', onViewportResize)
  document.removeEventListener('pointerdown', dismissStatusOutside)
  document.removeEventListener('visibilitychange', onDocumentVisibility)
  clearInterval(lightingTimer)
  cancelDecorDrag?.()
})
</script>

<template>
  <!-- 领养页 -->
  <div v-if="!adopted" class="adopt-mask">
    <div class="adopt-box">
      <!-- 第一步：家庭码 -->
      <template v-if="adoptStep === 'code'">
        <h2>🐱 喵星成长屋</h2>
        <p class="sub">输入家庭码开始（问问爸爸妈妈）</p>
        <input v-model="familyCodeInput" maxlength="6" inputmode="numeric" class="code-input"
          placeholder="家庭码（6 位数字）" @keyup.enter="doEnter">
        <button class="enter-btn" @click="doEnter">进入</button>
        <p class="tiny">换新设备也能进：输同样的家庭码，选择"我是谁"即可</p>
      </template>

      <!-- 第二步：选择孩子（已有孩子的家庭 = 多端登录入口） -->
      <template v-else-if="adoptStep === 'pick'">
        <h2>你是谁呀？</h2>
        <p class="sub">{{ enterFamily }} 的小朋友，选自己吧～</p>
        <div class="pick-list">
          <button v-for="k in enterKids" :key="k.id" class="pick-btn" @click="pickKid(k)">
            {{ k.name }}<small v-if="!k.has_pet">（还没猫，先去领一只）</small>
          </button>
        </div>
        <a class="new-kid" @click="gotoNewKid">我是新来的 ➕</a>
      </template>

      <!-- 第三步：新孩子领养 -->
      <template v-else>
        <h2>🐱 领养你的小猫</h2>
        <p class="sub">选一只喜欢的猫咪，它会陪你完成每天的任务</p>
      <div class="cats">
        <div v-for="sp in speciesList" :key="sp.species" class="cat-card" :class="{ sel: adoptSel === sp.species }"
          @click="adoptSel = sp.species">
          <div class="thumb"><CatSprite :species="sp.species" anim="sit" :size="96" /></div>
          <div class="nm">{{ sp.display_name }}</div>
          <div class="ds">{{ sp.description }}</div>
        </div>
      </div>
      <div class="adopt-row">
        <input v-model="kidNameInput" maxlength="8" placeholder="你的名字" style="width:46%" />
        <input v-model="petNameInput" maxlength="8" placeholder="给小猫取名字（比如：橘子）" style="width:54%" />
      </div>
      <button class="enter-btn" @click="doAdopt">领养！</button>
      </template>
    </div>
  </div>

  <!-- 房间 -->
  <div v-else class="room" ref="rootEl" :class="{ 'drawer-open': isNarrow && !!sheet }" :data-time-phase="sceneLighting.label">
    <svg class="ui-icons" aria-hidden="true" width="0" height="0">
      <defs>
        <symbol id="room-star" viewBox="0 0 24 24"><path d="m12 3 2.8 5.7 6.2.9-4.5 4.4 1.1 6.2-5.6-3-5.6 3 1.1-6.2L3 9.6l6.2-.9Z" /></symbol>
        <symbol id="room-flame" viewBox="0 0 24 24"><path d="M12 3c1 5-4 6-4 9 0 1 1 2 2 2 0-2 2-3 3-5 3 3 5 5 5 8a6 6 0 0 1-12 0C3 10 8 6 12 3Z" /></symbol>
        <symbol id="room-target" viewBox="0 0 24 24"><circle cx="12" cy="12" r="7" /><circle cx="12" cy="12" r="2" /><path d="M12 2v3m0 14v3M2 12h3m14 0h3" /></symbol>
        <symbol id="room-paw" viewBox="0 0 24 24"><ellipse cx="5.5" cy="8" rx="2" ry="2.5" /><ellipse cx="10" cy="5" rx="2" ry="2.5" /><ellipse cx="15" cy="5" rx="2" ry="2.5" /><ellipse cx="19" cy="8" rx="2" ry="2.5" /><path d="M7 15c1-5 9-5 10 0l2 3c1 4-5 2-7 2s-8 2-7-2Z" /></symbol>
        <symbol id="room-task" viewBox="0 0 24 24"><rect x="5" y="4" width="14" height="17" rx="3" /><path d="M9 3h6v4H9zm0 9h6m-6 4h4" /></symbol>
        <symbol id="room-heart" viewBox="0 0 24 24"><path d="M20 5c-3-3-6-1-8 1-2-2-5-4-8-1s-1 6 2 9l6 6 6-6c3-3 5-6 2-9Z" /></symbol>
        <symbol id="room-shop" viewBox="0 0 24 24"><path d="M4 8h16l-1 13H5Zm4 0V6a4 4 0 0 1 8 0v2" /></symbol>
        <symbol id="room-chat" viewBox="0 0 24 24"><path d="M4 4h16v13H9l-5 4Z" /><path d="M8 8h8m-8 4h5" /></symbol>
        <symbol id="room-food" viewBox="0 0 24 24"><path d="M3 12h18l-3 8H6ZM6 8c-2-2 2-3 0-5m6 5c-2-2 2-3 0-5m6 5c-2-2 2-3 0-5" /></symbol>
        <symbol id="room-chevron" viewBox="0 0 24 24"><path d="m7 10 5 5 5-5" /></symbol>
        <symbol id="room-close" viewBox="0 0 24 24"><path d="m6 6 12 12M6 18 18 6" /></symbol>
      </defs>
    </svg>
    <GardenBackdrop :lighting="sceneLighting" />
    <header class="room-header">
      <div class="room-identity">
        <CatSprite :species="species" anim="sit" :size="40" />
        <div class="room-name"><h1>{{ pet?.name }}的小屋</h1><small>喵星成长屋 · {{ sceneLighting.label }} {{ sceneLighting.time }}</small></div>
      </div>
      <div class="header-stats">
        <button class="star-stat" @click="showTip('stars')" aria-label="查看星星说明"><svg class="ui-icon"><use href="#room-star" /></svg>{{ pet?.balance }}</button>
        <button class="streak-stat" @click="showTip('streak')" aria-label="查看连击说明"><svg class="ui-icon"><use href="#room-flame" /></svg>{{ pet?.streak }} 天</button>
        <button class="status-toggle" ref="statusToggleEl" :class="{ open: hudOpen }" :aria-expanded="hudOpen" aria-controls="cat-status" aria-label="查看小猫状态" @click="toggleStatus"><svg class="ui-icon"><use href="#room-chevron" /></svg></button>
      </div>
    </header>
    <!-- 房间独立取景框：世界坐标固定，控件不跟着镜头移动。 -->
    <div class="scene-viewport" ref="roomEl" @pointerdown="onRoomTap">
    <div class="world" ref="worldEl" :style="{ transform: 'translate(' + cam.x + 'px,' + cam.y + 'px)', transition: camAnim }">
    <PixelRoom :wallpaper-key="activeWallpaper" />

    <!-- 场景道具（可拖动改位置，拖到边缘收回；被猫咬会晃） -->
    <div v-for="it in placedDecor" :key="it.id" class="scene-item"
      :class="{ shake: shakeId === it.id }"
      :style="{ left: it.x + 'px', top: it.y + 'px', zIndex: decorDepth(it), transition: it.item_key === 'yarn' ? 'left .8s ease' : '' }"
      :title="decorArt(it.item_key).name"
      @pointerdown="(e) => onItemPointerDown(e, it, it.id)">
      <PixelItem :item-key="it.item_key" :size="decorArt(it.item_key).size" />
    </div>

    <!-- 食物碗（放大，猫低头吃时可见） -->
    <svg v-if="showBowl" class="bowl" :style="{ left: bowlPos.x + 'px', top: bowlPos.y + 'px', zIndex: 10 + Math.floor(bowlPos.y) }" width="72" height="42" viewBox="0 0 48 28" shape-rendering="crispEdges">
      <path d="M8 4H40V6H44V10H42V14H40V20H36V24H12V20H8V14H6V10H4V6H8Z" fill="#765449" />
      <path d="M8 6H40V8H42V12H40V16H37V21H11V16H8V12H6V8H8Z" fill="#C78776" />
      <path d="M12 7H36V9H40V12H36V14H12V12H8V9H12Z" fill="#744C3E" />
      <path d="M14 9H18V11H14ZM22 8H27V11H22ZM30 10H34V12H30Z" fill="#D9A571" />
      <path d="M11 16H37V19H34V21H14V19H11Z" fill="#E7B296" />
    </svg>

    <!-- 猫 -->
    <div class="cat-wrap" ref="catEl" :class="{ sad: moodColor === 'sad' }"
      :style="{ left: catPos.x + 'px', top: catPos.y + 'px', zIndex: catDepth }" @click="petCat">
      <div class="bubble" v-if="showBubble">{{ bubble }}</div>
      <span class="zzz" v-if="catAnim === 'sleep' && zzz">💤</span>
      <div class="cat-inner" :class="{ flip: flipped }">
        <CatSprite :species="species" :anim="catAnim" :size="110" />
      </div>
    </div>
    </div><!-- /world -->
    <div v-if="placingFeed" class="place-banner">点一下地面放猫粮，点“陪伴 → 取消喂食”可取消</div>
    </div><!-- /scene-viewport -->
    <div class="room-tools" v-show="!sheet">
      <button class="mobile-companion" :class="{ on: toolsOpen }" :aria-expanded="toolsOpen" @click="toggleCompanion"><svg class="ui-icon"><use href="#room-paw" /></svg>陪伴</button>
      <span class="camera-hint">{{ camMode === 'free' ? '自由视角' : '拖动画面，逛逛小屋' }}</span>
      <button class="camera-reset" :class="{ free: camMode === 'free' }" @click="resetCam" aria-label="视角回到小猫"><svg class="ui-icon"><use href="#room-target" /></svg>回猫</button>
    </div>

    <!-- 完整状态锚定开关下方，独立于游戏相机。 -->
    <span v-show="hudOpen" class="status-pointer" :style="{ top: (parseFloat(statusPosition.top || 0) - 8) + 'px', right: (parseFloat(statusPosition.right || 0) + 10) + 'px' }"></span>
    <div class="hud-top" id="cat-status" ref="statusPanelEl" :style="statusPosition" v-show="hudOpen">
      <div class="chip">🐱 {{ pet?.name }} <small>{{ today.date }} {{ today.date_multiplier > 1 ? '· 奖励日×' + today.date_multiplier : '' }}</small></div>
      <div class="chip tippable" @mouseenter="showTip('stars')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('stars', e)" @pointerup="tipUp" @pointerleave="tipUp">星星 ⭐{{ pet?.balance }}</div>
      <div class="chip tippable" @mouseenter="showTip('level')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('level', e)" @pointerup="tipUp" @pointerleave="tipUp">等级 Lv{{ pet?.level }}</div>
      <div class="chip tippable" @mouseenter="showTip('streak')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('streak', e)" @pointerup="tipUp" @pointerleave="tipUp">连击 ×{{ pet?.streak }}</div>
      <div class="chip mini tippable" @mouseenter="showTip('hunger')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('hunger', e)" @pointerup="tipUp" @pointerleave="tipUp">饱腹<div class="bar"><i :style="{ width: (pet?.hunger || 0) + '%' }" /></div><small>{{ Math.round(pet?.hunger || 0) }}</small></div>
      <div class="chip mini tippable" @mouseenter="showTip('mood')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('mood', e)" @pointerup="tipUp" @pointerleave="tipUp">心情<div class="bar"><i class="mood" :style="{ width: (pet?.mood || 0) + '%' }" /></div><small>{{ Math.round(pet?.mood || 0) }}</small></div>
      <div class="chip switch" @click="switchKid" title="换一个小朋友登录">👤 换人</div>
      <button class="hud-fold" @click="hudOpen = false">收起状态</button>
    </div>

    <!-- 提示气泡（触屏长按后松手/移开消失） -->
    <div v-if="tipShow" class="tip-bubble" @pointerdown="hideTip">{{ tipText }}</div>

    <div v-if="isNarrow && sheet" class="sheet-scrim" @click="sheet = ''"></div>
    <!-- 侧栏容器：桌面=右侧自适应列；窄屏=底部抽屉 -->
    <div class="sidebar" :class="{ open: !!sheet, narrow: isNarrow }">
    <div class="panel" id="taskPanel" v-show="!isNarrow || sheet === 'task'">
      <div class="panel-bar">📋 今日任务（{{ today.tasks.filter(t => t.status === 'approved').length }}/{{ today.tasks.length }}）<button class="sheet-close" @click="sheet = ''" aria-label="关闭任务"><svg class="ui-icon"><use href="#room-close" /></svg></button></div>
      <div class="panel-body">
        <div v-for="t in today.tasks" :key="t.id" class="task" :class="t.status === 'approved' ? 'done' : t.status === 'submitted' ? 'wait' : ''">
          <div class="ic">{{ t.snapshot?.icon }}</div>
          <div class="info">
            <div class="nm">{{ t.snapshot?.name }}</div>
            <div class="sub">{{ t.snapshot?.goal_type === 'duration' ? t.snapshot.goal_value + ' 分钟' : '自由完成' }} · {{ t.snapshot?.base_score }}⭐{{ today.date_multiplier > 1 ? '×' + today.date_multiplier : '' }}</div>
          </div>
          <span v-if="t.status === 'approved'" class="st ok">✓</span>
          <span v-else-if="t.status === 'submitted'" class="st wait-t">等妈妈…</span>
          <span v-else-if="t.status === 'skipped'" class="st skip">请假</span>
          <button v-else @click="startTask(t)">{{ taskBtnLabel(t.snapshot) }}</button>
        </div>
        <div v-if="!today.tasks.length" class="empty">今天没有任务，和猫玩吧～</div>
      </div>
    </div>

    <div class="panel wish" id="wishPanel" v-show="!isNarrow || sheet === 'wish'">
      <div class="panel-bar">🌠 我的愿望 <a @click="newWish">+ 许愿</a><button class="sheet-close" @click="sheet = ''" aria-label="关闭愿望"><svg class="ui-icon"><use href="#room-close" /></svg></button></div>
      <div class="panel-body">
        <div v-for="w in wishes.filter(x => x.status !== 'rejected')" :key="w.id" class="wish-row">
          <div class="wt">✨ {{ w.title }} <a v-if="['pending', 'saving'].includes(w.status)" class="cancel" @click="cancelWish(w)">✕</a></div>
          <div v-if="w.status === 'saving'" class="track"><i :style="{ width: (w.progress || 0) + '%' }" /><span>{{ w.progress }}%</span></div>
          <div v-else-if="w.status === 'pending'" class="wsub">等妈妈定价…</div>
          <div v-else-if="w.status === 'redeem_requested'" class="wsub">等妈妈兑现～</div>
          <div v-else-if="w.status === 'fulfilled'" class="wsub done">🌟 已实现！</div>
        </div>
        <div v-if="!wishes.length" class="empty">还没有愿望，点上面许一个～</div>
      </div>
    </div>

    <div class="panel shop" id="shopPanel" v-show="!isNarrow || sheet === 'shop'">
      <div class="panel-bar">🛒 像素商店<button class="sheet-close" @click="sheet = ''" aria-label="关闭商店"><svg class="ui-icon"><use href="#room-close" /></svg></button></div>
      <div class="panel-body">
        <div v-for="it in shopItems" :key="it.key" class="shop-row">
          <PixelItem v-if="!isWallpaper(it.key)" :item-key="it.key" :size="32" />
          <i v-else class="wallpaper-swatch" :style="{ backgroundColor: decorArt(it.key).wallColor }" aria-hidden="true"></i>
          <span>{{ it.name }}</span><em>{{ it.price }}⭐</em>
          <button :class="{ owned: it.owned }" :disabled="wallpaperSaving" @click="shopAction(it)">{{ !it.owned ? '购买' : isWallpaper(it.key) ? (activeWallpaper === it.key ? '使用中' : '使用') : '摆放' }}</button>
        </div>
      </div>
    </div>

    </div><!-- /sidebar -->

    <!-- 窄屏底部导航 -->
    <nav v-if="isNarrow" class="sheet-nav" aria-label="小屋导航">
      <button :class="{ on: sheet === 'task' }" @click="openSheet('task')"><span class="nav-icon"><svg class="ui-icon"><use href="#room-task" /></svg></span><span>任务</span></button>
      <button :class="{ on: sheet === 'wish' }" @click="openSheet('wish')"><span class="nav-icon"><svg class="ui-icon"><use href="#room-heart" /></svg></span><span>愿望</span></button>
      <button :class="{ on: sheet === 'shop' }" @click="openSheet('shop')"><span class="nav-icon"><svg class="ui-icon"><use href="#room-shop" /></svg></span><span>商店</span></button>
      <button :class="{ on: chatOpen }" @click="openChat"><span class="nav-icon"><svg class="ui-icon"><use href="#room-chat" /></svg></span><span>聊天</span></button>
    </nav>

    <!-- 物品栏：已购未摆放的道具，拖入房间 -->
    <div v-if="inventory.length && (!isNarrow || toolsOpen)" class="inv-bar">
      <span class="lbl">拖我进房间→</span>
      <div v-for="it in inventory" :key="it.key" class="inv-item" :title="it.name"
        @pointerdown="(e) => onItemPointerDown(e, it, 0)"><PixelItem :item-key="it.key" :size="40" /></div>
    </div>

    <div class="actions" v-show="!isNarrow || toolsOpen">
      <button class="pbtn feed" @click="feed"><span class="action-icon"><svg class="ui-icon"><use href="#room-food" /></svg></span><span class="action-copy"><b>{{ placingFeed ? '取消喂食' : '喂猫粮' }}</b><small>10 星星 / 次</small></span></button>
      <button class="pbtn hint" @click="petCat"><span class="action-icon"><svg class="ui-icon"><use href="#room-paw" /></svg></span><span class="action-copy"><b>摸摸头</b><small>陪它开心一下</small></span></button>
      <button class="pbtn chatbtn" @click="openChat"><span class="action-icon"><svg class="ui-icon"><use href="#room-chat" /></svg></span><span class="action-copy"><b>聊聊天</b><small>今日还可聊 {{ chatLeft }} 句</small></span></button>
    </div>

    <!-- AI 分析状态层（b 模式提交后 ~20 秒） -->
    <div v-if="aiThinking" class="think-mask">
      <div class="think-box">
        <div class="think-cat">🐱</div>
        <b>猫正在认真看你的作业…</b>
        <div class="think-dots"><i></i><i></i><i></i></div>
        <small>别走开，马上就好喵～</small>
      </div>
    </div>

    <!-- 照片大图预览 -->
    <div v-if="photoPreview" class="pprev-mask" @click.self="photoPreview = null">
      <img :src="photoPreview" @click="photoPreview = null" />
    </div>

    <!-- 提交向导（多步骤证据） -->
    <div v-if="wizard" class="chat-mask">
      <div class="wizard-box">
        <div class="chat-head">
          <b>{{ wizard.task.snapshot.icon }} {{ wizard.task.snapshot.name }}</b>
          <span class="chat-left">{{ wizard.steps.length }} 步</span>
          <a @click="wizard = null">✕</a>
        </div>
        <div class="wizard-body">
          <div v-for="(st, i) in wizard.steps" :key="i" class="wstep" :class="{ done: wizardReady(wizard) && wizard.done[i] }">
            <div class="wstep-head">
              <b>{{ st.kind === 'photo' ? '📷' : '🎙' }} {{ st.label }}</b>
              <small v-if="!st.required">可选</small>
              <em v-if="wizard.done[i] && wizard.done[i].length">✓ {{ wizard.done[i].length }} 个</em>
            </div>
            <button class="pbtn" @click="wizardPick(st, i)">
              {{ wizard.done[i] && wizard.done[i].length ? '再传一张 / 重新选' : st.kind === 'photo' ? '拍照 / 相册（可多张）' : '录音 / 音频文件' }}
            </button>
            <div v-if="wizard.done[i] && wizard.done[i].length" class="wprev">
              <div v-for="(f, fi) in wizard.done[i]" :key="fi" class="wprev-item">
                <img v-if="f.type === 'photo'" :src="f.path" @click="previewPhoto(f.path)" />
                <audio v-else-if="f.type === 'audio'" :src="f.path" controls preload="none" />
                <a class="wprev-del" @click="wizardRemoveFile(i, fi)">✕</a>
              </div>
            </div>
          </div>
        </div>
        <div class="wizard-foot">
          <button class="ok-btn" :disabled="!wizardReady(wizard)" @click="wizardSubmit">
            交给猫看 🐱
          </button>
        </div>
      </div>
    </div>
    <input ref="wizardInput" type="file" style="display: none" @change="onWizardFile" />

    <!-- 猫聊天面板（SSE 打字机） -->
    <div v-if="chatOpen" class="chat-mask">
      <div class="chat-box">
        <div class="chat-head">
          <b>🐱 {{ pet?.name }}</b>
          <span class="chat-left">今日还能聊 {{ chatLeft }} 句</span>
          <a @click="chatOpen = false">✕</a>
        </div>
        <div class="chat-body" id="chatBody">
          <div v-for="(m, i) in chatMessages" :key="i" class="msg" :class="m.role">
            <div class="avatar">{{ m.role === 'user' ? '🧒' : '🐱' }}</div>
            <div class="bubble-txt">{{ m.content }}<i v-if="chatStreaming && i === chatMessages.length - 1 && m.role === 'assistant'" class="cursor">▌</i></div>
          </div>
        </div>
        <div class="chat-input">
          <button class="rec-btn" :class="recState" @pointerdown.prevent="recStart" @pointerup.prevent="recStop" @pointerleave="recStop" :disabled="recState === 'uploading'">
            <template v-if="recState === 'recording'">🎙<i></i></template>
            <template v-else-if="recState === 'uploading'">⏳</template>
            <template v-else>🎤</template>
          </button>
          <input v-model="chatInput" maxlength="200" :placeholder="recState === 'recording' ? '松开结束，在听呢…' : recState === 'uploading' ? '猫在听写…' : '跟它说点什么（按🎤说话）'"
            @keyup.enter="sendChat" :disabled="chatStreaming" />
          <button @click="sendChat" :disabled="chatStreaming || !chatInput.trim()">喵！</button>
        </div>
      </div>
    </div>

    <!-- 全屏计时页 -->
    <TimerOverlay v-if="timerTask" :minutes="timerTask.snapshot.goal_value"
      :task-name="timerTask.snapshot.name" :icon="timerTask.snapshot.icon" :species="species"
      @done="onTimerDone" @giveup="onTimerGiveup" />

    <!-- 拍照/录音（调起相机或语音备忘录） -->
    <input ref="evidenceInput" type="file" accept="image/*,audio/*,video/*" capture="environment" style="display:none" @change="onEvidenceFile" />

    <!-- 许愿输入框 -->
    <div v-if="wishModal" class="modal-mask" @click.self="wishModal = false">
      <div class="modal-box">
        <h3>🌠 许个新愿</h3>
        <input v-model="wishForm.title" maxlength="30" placeholder="想要什么呢？（如：乐高梦幻花园）" />
        <input v-model="wishForm.hoped" type="number" placeholder="觉得值多少星星？（可以不填，妈妈来定）" />
        <div class="modal-btns">
          <button class="ghost" @click="wishModal = false">取消</button>
          <button class="primary" @click="submitWish">许愿！</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
* { box-sizing: border-box; font-family: 'PingFang SC', 'Hiragino Sans GB', system-ui, sans-serif; }
button { font: inherit; -webkit-tap-highlight-color: transparent; touch-action: manipulation; }
button:focus-visible, a:focus-visible, input:focus-visible { outline: 3px solid #9C8BC5; outline-offset: 3px; }

/* 领养与登录 */
.adopt-mask { position: fixed; inset: 0; z-index: 100; background: rgba(250, 243, 227, .97); display: flex; align-items: center; justify-content: center; padding: 20px 0; overflow-y: auto; touch-action: pan-y; }
.adopt-box { max-width: 620px; width: 92vw; background: #FFFCF6; color: #5D493B; border: 2px solid #CBB49A; border-radius: 18px; box-shadow: 0 12px 36px rgba(100, 73, 47, .14); padding: 24px; margin: auto; }
.adopt-box h2 { font-size: 22px; text-align: center; margin-bottom: 4px; }
.adopt-box .sub { text-align: center; font-size: 13px; color: #88786A; margin-bottom: 18px; }
.cats { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 12px; margin-bottom: 18px; }
.cat-card { border: 2px solid #D9C6AD; border-radius: 12px; padding: 12px 8px; text-align: center; cursor: pointer; box-shadow: 0 3px 0 #E8DCCB; background: #FAF3E3; transition: transform .18s ease; }
.cat-card:hover { transform: translateY(-3px); }
.cat-card.sel { background: #EEEBFA; border-color: #9C8BC5; }
.cat-card .nm { font-size: 14px; font-weight: 700; margin-top: 6px; }
.cat-card .ds { font-size: 11px; color: #88786A; margin-top: 2px; }
.code-input { width: 100%; font-size: 22px; text-align: center; letter-spacing: 8px; border: 1.5px solid #CBB49A; border-radius: 10px; padding: 12px; margin-bottom: 12px; }
.enter-btn { width: 100%; font-size: 16px; font-weight: 700; color: #fff; background: #8370AF; border: 1.5px solid #706098; border-radius: 10px; padding: 12px; cursor: pointer; box-shadow: 0 3px 0 #D8CFE8; }
.tiny { font-size: 11px; color: #88786A; margin-top: 12px; text-align: center; }
.pick-list { display: flex; flex-direction: column; gap: 10px; margin: 16px 0; }
.pick-btn { font-size: 17px; font-weight: 700; color: #5D493B; background: #fff; border: 1.5px solid #D9C6AD; border-radius: 12px; padding: 14px; cursor: pointer; box-shadow: 0 3px 0 #E8DCCB; }
.pick-btn small { font-size: 12px; color: #88786A; font-weight: 400; margin-left: 8px; }
.pick-btn:active { transform: translateY(1px); box-shadow: none; }
.new-kid { display: block; text-align: center; color: #75609D; font-size: 13px; font-weight: 600; cursor: pointer; padding: 8px; }
.adopt-row { display: flex; gap: 10px; margin-bottom: 12px; }
.adopt-row input { flex: 1; min-width: 0; font-size: 14px; border: 1.5px solid #CBB49A; border-radius: 8px; padding: 9px 12px; }
.adopt-row button { font-size: 15px; font-weight: 700; cursor: pointer; color: #fff; background: #8370AF; border: 1.5px solid #706098; border-radius: 8px; padding: 9px 22px; }

.ui-icons { position: absolute; pointer-events: none; }
.ui-icon { width: 24px; height: 24px; fill: none; stroke: currentColor; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }
.camera-hint { font-size: 11px; color: #88786A; white-space: nowrap; }
/* 美术分层：庭院在视口外；固定世界内仅基础墙地纹理，装饰独立持久化。 */
.room { position: fixed; inset: 0; overflow: hidden; color: #5D493B; background: #617a4f; cursor: default; }
.scene-viewport { position: absolute; top: clamp(112px, 14vh, 152px); bottom: 132px; left: clamp(68px, 7vw, 132px); right: clamp(344px, 25vw, 420px); max-width: 1280px; max-height: 720px; overflow: hidden; z-index: 1; border: 8px solid #8b6547; border-top-color: #c8a778; border-left-color: #b18b61; border-radius: 6px; background: #dbb27b url('/art/wood-floor.png') center bottom / 1280px 320px repeat; image-rendering: pixelated; box-shadow: 0 0 0 4px #e0d1b4, 0 8px 0 4px #74644d, 0 14px 0 4px rgb(48 58 34 / 35%); touch-action: none; }
.world { position: absolute; left: 0; top: 0; width: 1280px; height: 720px; background: #e9c891; will-change: transform; }
.cat-wrap { position: absolute; width: 110px; margin-left: -55px; margin-top: -100px; cursor: pointer; z-index: 5; }
.cat-inner { transition: transform .3s; transform-origin: 50% 90%; }
.cat-inner.flip { transform: scaleX(-1); }
.cat-wrap.sad { filter: grayscale(.75); }
.bubble { position: absolute; left: 50%; bottom: calc(100% + 8px); transform: translateX(-50%); width: max-content; max-width: 300px; background: #FFFCF6; border: 1.5px solid #CBB49A; border-radius: 12px; padding: 8px 12px; font-size: 12px; line-height: 1.5; font-weight: 600; white-space: normal; overflow-wrap: anywhere; box-shadow: 0 4px 12px rgba(103, 75, 47, .12); pointer-events: none; }
.zzz { position: absolute; top: -20px; right: 0; }
.scene-item { position: absolute; transform: translate(-50%, -100%); line-height: 0; cursor: grab; user-select: none; touch-action: none; }
.scene-item.shake { animation: shake .5s ease-in-out 3; }
@keyframes shake { 0%, 100% { transform: translate(-50%, -100%) rotate(0); } 30% { transform: translate(-50%, -100%) rotate(-8deg); } 60% { transform: translate(-50%, -100%) rotate(6deg); } }
.bowl { position: absolute; transform: translate(-50%, -100%); z-index: 6; }
.place-banner { position: absolute; left: 50%; top: 18px; transform: translateX(-50%); z-index: 8; width: max-content; max-width: calc(100% - 32px); background: #FFF1D6; color: #805A32; border: 1.5px solid #D6B58A; border-radius: 12px; padding: 8px 14px; font-size: 12px; line-height: 1.5; font-weight: 700; box-shadow: 0 4px 12px rgba(103, 75, 47, .12); pointer-events: none; }
.room:has(.place-banner) .scene-viewport { cursor: crosshair; }

/* 标题与默认收起的状态浮层，开关交给 v-show */
.room-header { position: absolute; top: 20px; left: clamp(68px, 7vw, 132px); right: clamp(344px, 25vw, 420px); min-height: 64px; z-index: 16; display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 16px; background: #fff8e8; border: 2px solid #b29870; border-radius: 6px; box-shadow: 0 4px 0 #766b4b; }
.room-identity { display: flex; align-items: center; gap: 10px; min-width: 0; }
.room-identity > :first-child { flex-shrink: 0; }
.room-name { min-width: 0; }
.room-name h1 { margin: 0; font-size: 22px; line-height: 1.3; letter-spacing: .5px; font-weight: 800; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.room-name small { display: block; margin-top: 4px; font-size: 11px; line-height: 1.4; color: #88786A; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.header-stats { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
.header-stats button { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-height: 36px; padding: 7px 11px; border: 1px solid #DCCCB6; border-radius: 12px; background: rgba(255, 252, 246, .92); color: #70543C; font-size: 12px; font-weight: 700; white-space: nowrap; cursor: pointer; box-shadow: 0 2px 6px rgba(103, 75, 47, .05); }
.header-stats svg { width: 18px; height: 18px; flex-shrink: 0; }
.header-stats .star-stat { background: #FFF1D5; color: #94692F; }
.header-stats .streak-stat { background: #F8E9DF; color: #9A6450; }
.header-stats .status-toggle { color: #75609D; background: #F0ECF8; border-color: #D9CFE9; }
.status-toggle svg { transition: transform .18s; }
.status-toggle.open svg { transform: rotate(180deg); }
.hud-top { position: absolute; left: auto; max-height: calc(100dvh - 190px); display: grid; grid-template-columns: 1fr 1fr; gap: 8px; padding: 14px; z-index: 32; background: #fff9ed; border: 2px solid #b09b78; border-radius: 6px; box-shadow: 0 4px 0 #766b4b; overflow-y: auto; touch-action: pan-y; }
.hud-top::before { content: ''; position: absolute; top: 0; right: 8px; width: 12px; height: 4px; background: #8370af; }
.hud-top .chip:first-child { grid-column: 1 / -1; }
.hud-top .chip.mini { flex-direction: column; align-items: start; gap: 6px; }
.hud-top .chip.mini .bar { width: 100%; }
.hud-top .chip.mini small { align-self: flex-end; }
.chip { display: flex; align-items: center; gap: 6px; min-width: 0; max-width: 100%; background: #F7F0E5; border: 1px solid #E6D8C5; border-radius: 10px; padding: 8px 10px; font-weight: 600; font-size: 12px; flex-wrap: wrap; }
.chip small { font-size: 10px; font-weight: 400; color: #88786A; }
.chip.mini .bar { width: 60px; height: 8px; background: #EAE1D4; border-radius: 5px; overflow: hidden; }
.chip.mini .bar i { display: block; height: 100%; background: #D99B7D; transition: width .5s; }
.chip.mini .bar i.mood { background: #99AE89; }
.chip.mini small { font-size: 10px; color: #705F50; font-weight: 700; }
.chip.switch { cursor: pointer; background: #EEEBF7; color: #75609D; border-color: #DCD2EB; }
.chip.tippable { cursor: help; -webkit-touch-callout: none; user-select: none; }
.hud-fold { margin-left: auto; border: 0; background: transparent; font-size: 12px; color: #75609D; font-weight: 700; cursor: pointer; padding: 8px; }
.tip-bubble { position: absolute; left: 24px; top: 82px; z-index: 90; max-width: min(320px, calc(100% - 48px)); background: #6C5849; color: #FFF9EE; border: 1px solid #A28D78; border-radius: 12px; padding: 10px 14px; font-size: 12px; line-height: 1.65; overflow-wrap: anywhere; box-shadow: 0 6px 18px rgba(103, 75, 47, .16); animation: tipin .18s ease; }
@keyframes tipin { from { opacity: 0; transform: translateY(-6px); } to { opacity: 1; transform: none; } }

/* 侧栏与任务、愿望、商店 */
.sidebar { position: absolute; top: 106px; right: 24px; bottom: 100px; width: 272px; display: flex; flex-direction: column; gap: 12px; z-index: 25; min-height: 0; }
.panel { width: 100%; min-width: 0; background: #fff9eb; border: 2px solid #b59d76; border-radius: 6px; box-shadow: 0 4px 0 #766b4b; overflow: hidden; }
.sidebar .panel { position: static; flex: 1 1 0; min-height: 0; display: flex; flex-direction: column; }
#taskPanel { flex-grow: 2; }
.panel-bar { display: flex; align-items: center; flex-shrink: 0; gap: 8px; min-height: 44px; padding: 10px 12px; border-bottom: 1px solid #E6D8C5; font-size: 13px; font-weight: 700; background: #EAF0E3; }
.panel.wish .panel-bar { background: #EFEBF7; }
.panel.shop .panel-bar { background: #FAEEDB; }
.panel-bar a { margin-left: auto; color: #75609D; font-size: 12px; cursor: pointer; white-space: nowrap; }
.sheet-close { display: none; margin-left: auto; flex: 0 0 28px; width: 28px; height: 28px; padding: 4px; align-items: center; justify-content: center; background: rgba(255, 255, 255, .7); color: #796653; border: 1px solid #D9C7B1; border-radius: 8px; cursor: pointer; }
.sheet-close svg { width: 16px; height: 16px; }
.panel-body { padding: 10px; }
.sidebar .panel .panel-body { flex: 1 1 auto; min-height: 0; overflow-y: auto; overscroll-behavior: contain; touch-action: pan-y; }
.sheet-scrim { position: fixed; inset: 0; z-index: 19; background: rgba(88, 69, 51, .2); }
.sheet-nav { display: none; }
.task { display: flex; align-items: center; gap: 7px; border: 1px solid #E4D6C4; border-radius: 10px; padding: 8px; background: #fff; margin-bottom: 8px; }
.task.done { background: #EDF3E8; border-color: #D4E0CC; }
.task.wait { background: #F1EDF9; border-color: #DED6ED; }
.task .ic { flex: 0 0 30px; width: 30px; height: 30px; display: flex; align-items: center; justify-content: center; font-size: 17px; border: 1px solid #E3CFB1; border-radius: 8px; background: #FAEEDA; }
.task .info { flex: 1; min-width: 0; }
.task .nm { font-size: 12.5px; line-height: 1.45; font-weight: 600; overflow-wrap: anywhere; }
.task .sub { font-size: 10.5px; line-height: 1.5; color: #88786A; }
.task button, .shop-row button { flex-shrink: 0; font-size: 11px; font-weight: 600; cursor: pointer; color: #75609D; border: 1px solid #D5C8E7; border-radius: 8px; padding: 6px 8px; background: #EEEBF7; white-space: nowrap; box-shadow: 0 2px 0 #E5DDEE; }
.task button:active, .shop-row button:active { transform: translateY(1px); box-shadow: none; }
.st.ok { color: #719462; font-weight: 700; }
.st.wait-t { color: #75609D; font-weight: 700; font-size: 11px; white-space: nowrap; }
.st.skip { color: #88786A; font-size: 11px; white-space: nowrap; }
.empty { font-size: 12px; color: #88786A; text-align: center; padding: 12px 8px; }
.wish-row { margin-bottom: 10px; }
.wish-row .wt { font-size: 13px; font-weight: 700; margin-bottom: 4px; overflow-wrap: anywhere; }
.wish-row .track { height: 16px; background: #EFE6D7; border: 1px solid #D7C3A9; border-radius: 8px; overflow: hidden; position: relative; }
.wish-row .track i { display: block; height: 100%; background: linear-gradient(90deg, #EBC995, #DDA761); transition: width .6s; }
.wish-row .track span { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font-size: 10.5px; font-weight: 700; }
.wsub { font-size: 11px; color: #88786A; }
.wsub.done { color: #A67B3F; font-weight: 700; }
.wish-row .cancel { float: right; color: #A56860; font-weight: 700; cursor: pointer; margin-left: 8px; font-size: 12px; padding: 2px 6px; }
.shop-row { display: flex; align-items: center; gap: 8px; font-size: 12px; margin-bottom: 8px; }
.shop-row span { flex: 1; min-width: 0; font-weight: 600; overflow-wrap: anywhere; }
.shop-row em { font-style: normal; color: #94692F; font-weight: 700; white-space: nowrap; }
.shop-row button.owned { background: #EDF3E8; color: #719462; border-color: #D4E0CC; }

/* 互动操作、独立相机工具与可横滚物品栏 */
.pbtn { font-size: 14px; font-weight: 600; cursor: pointer; padding: 10px 14px; color: #70543C; background: #FFF1DB; border: 1.5px solid #D8BFA1; border-radius: 12px; box-shadow: 0 3px 8px rgba(103, 75, 47, .08); }
.pbtn:active { transform: translateY(1px); box-shadow: none; }
.pbtn small { display: block; font-size: 10px; line-height: 1.4; font-weight: 400; color: #88786A; }
.actions { position: absolute; bottom: 30px; left: 24px; max-width: calc(100% - 470px); display: flex; gap: 10px; z-index: 17; }
.actions .pbtn { display: flex; align-items: center; gap: 10px; min-width: 0; height: 52px; padding: 8px 14px; }
.actions .feed { background: #FFF0D9; border-color: #DABF9B; }
.actions .hint { background: #EEE9F8; color: #75609D; border-color: #CFC2E3; }
.actions .chatbtn { background: #F9EAE3; border-color: #DFC1B4; }
.action-icon { display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
.action-icon svg { width: 24px; height: 24px; }
.action-copy { min-width: 0; text-align: left; }
.action-copy b { display: block; font-size: 13px; line-height: 1.4; white-space: nowrap; }
.action-copy small { margin-top: 2px; white-space: nowrap; }
.room-tools { position: absolute; right: 320px; bottom: 38px; z-index: 18; display: flex; align-items: center; gap: 10px; }
.mobile-companion { display: none; }
.camera-reset { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 38px; padding: 0 12px; color: #796653; background: rgba(255, 252, 246, .9); border: 1px solid #D9C7B1; border-radius: 20px; box-shadow: 0 3px 10px rgba(103, 75, 47, .08); font-size: 12px; font-weight: 600; white-space: nowrap; cursor: pointer; }
.camera-reset svg, .mobile-companion svg { flex-shrink: 0; width: 18px; height: 18px; }
.camera-reset.free, .camera-reset.on { background: #EEEBF7; color: #75609D; border-color: #CFC2E3; }
.inv-bar { position: absolute; bottom: 126px; left: 40px; max-width: min(580px, calc(100% - 376px)); z-index: 17; background: rgba(255, 252, 246, .94); border: 1px solid #D5C1A8; border-radius: 14px; box-shadow: 0 4px 12px rgba(103, 75, 47, .1); padding: 6px 10px; display: flex; gap: 8px; align-items: center; overflow-x: auto; overscroll-behavior-x: contain; touch-action: pan-x; scrollbar-width: thin; }
.scene-viewport .inv-bar { bottom: 16px; left: 16px; max-width: min(580px, calc(100% - 32px)); }
.inv-bar .lbl { flex-shrink: 0; font-size: 11px; color: #88786A; white-space: nowrap; }
.inv-item { flex: 0 0 44px; width: 44px; height: 44px; display: flex; align-items: center; justify-content: center; border: 2px dashed #b39b73; border-radius: 4px; cursor: grab; user-select: none; touch-action: none; background: #fff1d8; }
.wallpaper-swatch { display: block; flex: 0 0 24px; width: 24px; height: 24px; border: 2px solid #b09b78; background: #edccd0 url('/art/plaster.png') center / 128px; background-blend-mode: multiply; image-rendering: pixelated; }
:global(.drag-ghost) { position: fixed; width: 72px; height: 72px; opacity: 0; pointer-events: none; z-index: 999; image-rendering: pixelated; }
.status-pointer { position: absolute; width: 14px; height: 10px; background: #b09b78; clip-path: polygon(50% 0, 100% 100%, 0 100%); z-index: 33; pointer-events: none; }

/* 功能遮罩都在导航之上，滚动区域不接管场景拖动 */
.modal-mask, .chat-mask, .think-mask, .pprev-mask { position: fixed; inset: 0; display: flex; justify-content: center; background: rgba(77, 60, 45, .32); }
.modal-mask { z-index: 96; align-items: center; padding: 16px; overflow-y: auto; touch-action: pan-y; }
.modal-box { width: min(360px, 100%); max-height: calc(100dvh - 32px); overflow-y: auto; background: #FFFCF6; border: 1.5px solid #CBB49A; border-radius: 18px; box-shadow: 0 16px 48px rgba(77, 60, 45, .2); padding: 20px; animation: tipin .2s ease; touch-action: pan-y; }
.modal-box h3 { font-size: 17px; margin-bottom: 12px; }
.modal-box input { width: 100%; font-size: 16px; border: 1.5px solid #D5C1A8; border-radius: 10px; padding: 10px 12px; margin-bottom: 10px; }
.modal-btns { display: flex; gap: 10px; margin-top: 6px; }
.modal-btns button { flex: 1; font-size: 14px; font-weight: 700; cursor: pointer; border: 1.5px solid #D5C1A8; border-radius: 10px; padding: 10px; }
.modal-btns .primary { background: #8370AF; border-color: #706098; color: #fff; }
.modal-btns .ghost { background: #F3EDE3; color: #796653; }
.think-mask { z-index: 98; align-items: center; background: rgba(77, 60, 45, .48); padding: 16px; }
.think-box { max-width: 100%; background: #FFF9EE; border: 1.5px solid #CBB49A; border-radius: 20px; padding: 28px 24px; text-align: center; box-shadow: 0 16px 48px rgba(77, 60, 45, .2); }
.think-cat { font-size: 44px; animation: bouncecat 1.2s infinite; }
@keyframes bouncecat { 0%,100% { transform: translateY(0); } 50% { transform: translateY(-8px); } }
.think-box b { display: block; margin: 10px 0 8px; font-size: 16px; }
.think-dots { display: flex; gap: 5px; justify-content: center; margin-bottom: 8px; }
.think-dots i { width: 7px; height: 7px; background: #DDA761; border-radius: 50%; animation: dotp 1s infinite; }
.think-dots i:nth-child(2) { animation-delay: .2s; }
.think-dots i:nth-child(3) { animation-delay: .4s; }
@keyframes dotp { 40% { opacity: .25; transform: scale(.8); } }
.think-box small { color: #88786A; font-size: 12px; }
.pprev-mask { z-index: 99; align-items: center; background: rgba(38, 31, 26, .88); }
.pprev-mask img { max-width: 94%; max-height: 94dvh; object-fit: contain; border-radius: 10px; }

/* 聊天与提交向导 */
.chat-mask { z-index: 97; align-items: flex-end; }
.chat-box, .wizard-box { width: min(460px, 94vw); min-height: 0; background: #FFF9EE; border: 1.5px solid #CBB49A; border-radius: 18px 18px 0 0; box-shadow: 0 -8px 32px rgba(77, 60, 45, .16); display: flex; flex-direction: column; overflow: hidden; animation: chatin .22s ease; }
.chat-box { height: min(70dvh, 600px); }
.wizard-box { max-height: min(80dvh, 700px); }
@keyframes chatin { from { transform: translateY(32px); opacity: 0; } }
.chat-head { display: flex; align-items: center; flex-shrink: 0; gap: 8px; padding: 12px 14px; border-bottom: 1px solid #E6D8C5; background: #FAEEDA; }
.chat-head b { min-width: 0; font-size: 15px; overflow-wrap: anywhere; }
.chat-left { flex-shrink: 0; font-size: 11px; color: #88786A; margin-left: auto; }
.chat-head a { flex-shrink: 0; cursor: pointer; font-size: 15px; padding: 4px 8px; }
.chat-body, .wizard-body { flex: 1 1 auto; min-height: 0; overflow-y: auto; padding: 14px; overscroll-behavior: contain; touch-action: pan-y; }
.msg { display: flex; gap: 8px; margin-bottom: 12px; }
.msg.user { flex-direction: row-reverse; }
.msg .avatar { width: 34px; height: 34px; flex: 0 0 34px; display: flex; align-items: center; justify-content: center; background: #fff; border: 1px solid #D9C7B1; border-radius: 10px; font-size: 17px; }
.msg .bubble-txt { min-width: 0; max-width: calc(100% - 42px); background: #fff; border: 1px solid #E4D6C4; border-radius: 12px; padding: 8px 12px; font-size: 14px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.msg.user .bubble-txt { background: #EEEBF7; border-color: #DCD2EB; }
.cursor { animation: blink .8s infinite; font-style: normal; }
@keyframes blink { 50% { opacity: 0; } }
.chat-input { display: flex; align-items: center; flex-shrink: 0; gap: 8px; padding: 10px 12px calc(10px + env(safe-area-inset-bottom, 0px)); border-top: 1px solid #E6D8C5; background: #FFFCF6; }
.chat-input input { flex: 1 1 0; min-width: 0; width: 0; height: 44px; border: 1.5px solid #D5C1A8; border-radius: 10px; padding: 10px; font-size: 16px; background: #fff; }
.chat-input button { flex-shrink: 0; height: 44px; font-size: 14px; font-weight: 700; color: #805A32; background: #FFF0D5; border: 1.5px solid #D8BFA1; border-radius: 10px; padding: 8px 12px; cursor: pointer; }
.chat-input .rec-btn { position: static; flex: 0 0 44px; width: 44px; height: 44px; padding: 0; font-size: 20px; color: #75609D; background: #EEEBF7; border-color: #CFC2E3; box-shadow: 0 2px 6px rgba(103, 75, 47, .06); opacity: 1; touch-action: manipulation; user-select: none; }
.chat-input .rec-btn.recording { background: #AF6D66; color: #fff; border-color: #975B55; }
.chat-input .rec-btn.recording i { display: inline-block; width: 6px; height: 6px; background: #fff; border-radius: 50%; margin-left: 2px; animation: pulse .8s infinite; }
.chat-input .rec-btn.uploading { background: #FAEEDA; }
@keyframes pulse { 50% { opacity: .2; } }
.chat-input button:disabled { opacity: .55; cursor: default; }
.wstep { background: #fff; border: 1px solid #E4D6C4; border-radius: 12px; padding: 12px; margin-bottom: 10px; }
.wstep.done { background: #EDF3E8; border-color: #D4E0CC; }
.wstep-head { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 8px; }
.wstep-head b { font-size: 14px; overflow-wrap: anywhere; }
.wstep-head small { font-size: 11px; color: #88786A; }
.wstep-head em { font-style: normal; margin-left: auto; color: #719462; font-weight: 700; font-size: 13px; }
.wstep .pbtn { width: 100%; background: #FFF0D5; color: #805A32; }
.wizard-foot { flex-shrink: 0; padding: 10px 14px calc(10px + env(safe-area-inset-bottom, 0px)); border-top: 1px solid #E6D8C5; }
.ok-btn { width: 100%; font-size: 16px; font-weight: 800; color: #fff; background: #8370AF; border: 1.5px solid #706098; border-radius: 12px; padding: 12px; cursor: pointer; }
.ok-btn:disabled { opacity: .4; cursor: default; }
.wprev { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 8px; }
.wprev-item { position: relative; min-width: 0; max-width: 100%; }
.wprev-item img { width: 74px; height: 74px; object-fit: cover; border: 1.5px solid #D5C1A8; border-radius: 8px; cursor: zoom-in; }
.wprev-item audio { height: 36px; width: 200px; max-width: 100%; }
.wprev-del { position: absolute; top: -8px; right: -8px; width: 20px; height: 20px; background: #AF6D66; color: #fff; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 11px; cursor: pointer; border: 1.5px solid #fff; }

/* 与 JS matchMedia 的 900px 断点一致 */
@media (max-width: 900px) {
  .scene-viewport { inset: max(82px, calc(env(safe-area-inset-top, 0px) + 70px)) 10px calc(142px + env(safe-area-inset-bottom, 0px)); max-width: none; max-height: none; border-width: 4px; border-radius: 4px; box-shadow: 0 0 0 2px #e0d1b4, 0 6px 0 2px #74644d; }
  .camera-hint { font-size: 10px; padding: 4px 8px; background: #fff8e8; border-radius: 4px; }
  .room-tools { gap: 6px; }
  .room-header { top: max(env(safe-area-inset-top, 0px), 14px); left: 16px; right: 16px; min-height: 44px; gap: 8px; padding: 6px 8px; border: 1px solid rgba(217, 199, 177, .65); border-radius: 16px; background: rgba(255, 252, 246, .92); box-shadow: 0 4px 14px rgba(103, 75, 47, .08); }
  .room-identity { gap: 6px; flex: 1; }
  .room-name h1 { font-size: 16px; letter-spacing: 0; }
  .room-name small { font-size: 10px; margin-top: 2px; }
  .header-stats { gap: 4px; }
  .header-stats button { min-height: 32px; padding: 5px 7px; gap: 4px; border-radius: 10px; font-size: 11px; box-shadow: none; }
  .header-stats svg { width: 16px; height: 16px; }
  .header-stats .streak-stat { display: none; }
  .hud-top { left: auto; max-height: calc(100dvh - 190px - env(safe-area-inset-top, 0px) - env(safe-area-inset-bottom, 0px)); padding: 10px; gap: 6px; }
  .chip { padding: 7px 8px; font-size: 11px; gap: 5px; }
  .chip.mini .bar { width: 48px; }
  .tip-bubble { top: max(86px, calc(env(safe-area-inset-top, 0px) + 70px)); left: 16px; max-width: calc(100% - 32px); }
  .place-banner { top: max(86px, calc(env(safe-area-inset-top, 0px) + 70px)); }
  .sheet-nav { position: absolute; left: 12px; right: 12px; bottom: max(12px, calc(env(safe-area-inset-bottom, 0px) + 8px)); height: 66px; z-index: 40; display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 4px; padding: 5px; border: 1px solid #DCCCB6; border-radius: 20px; background: rgba(255, 252, 246, .98); box-shadow: 0 6px 22px rgba(103, 75, 47, .13); }
  .sheet-nav button { min-width: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px; padding: 4px; border: 0; border-radius: 14px; background: transparent; color: #88786A; font-size: 11px; line-height: 1.3; font-weight: 600; cursor: pointer; }
  .sheet-nav button.on, .sheet-nav button.active { background: #EEEBF7; color: #75609D; }
  .nav-icon { display: flex; align-items: center; justify-content: center; }
  .nav-icon svg { width: 24px; height: 24px; }
  .room-tools { left: 16px; right: 16px; bottom: calc(92px + env(safe-area-inset-bottom, 0px)); justify-content: space-between; }
  .mobile-companion { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 38px; padding: 0 12px; background: rgba(255, 252, 246, .94); color: #796653; border: 1px solid #D9C7B1; border-radius: 20px; box-shadow: 0 3px 10px rgba(103, 75, 47, .08); font-size: 12px; font-weight: 600; white-space: nowrap; cursor: pointer; }
  .mobile-companion.on, .mobile-companion.active { background: #EEEBF7; color: #75609D; border-color: #CFC2E3; }
  .actions { bottom: calc(142px + env(safe-area-inset-bottom, 0px)); left: 16px; right: 16px; max-width: none; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; padding: 4px; background: rgba(255, 252, 246, .9); border: 1px solid #E4D6C4; border-radius: 18px; box-shadow: 0 5px 16px rgba(103, 75, 47, .09); }
  .actions .pbtn { width: 100%; height: 58px; justify-content: center; gap: 8px; padding: 8px; }
  .action-icon svg { width: 22px; height: 22px; }
  .action-copy b { font-size: 13px; }
  .action-copy small { font-size: 10px; }
  .actions .chatbtn { display: none; }
  .inv-bar, .scene-viewport .inv-bar { bottom: calc(214px + env(safe-area-inset-bottom, 0px)); left: 16px; right: 16px; width: max-content; max-width: calc(100% - 32px); }
  .sidebar.narrow { top: auto; bottom: calc(90px + env(safe-area-inset-bottom, 0px)); left: 12px; right: 12px; width: auto; max-height: 60dvh; display: none; gap: 0; }
  .sidebar.narrow.open { display: flex; }
  .sidebar.narrow .panel { flex: 0 1 auto; max-height: 60dvh; }
  .sidebar.narrow .sheet-close { display: inline-flex; }
  .chat-box, .wizard-box { width: 100%; border-bottom: 0; }
  .chat-head { padding: 12px; }
  .chat-head b { font-size: 14px; }
  .chat-left { font-size: 10px; }
  .chat-input { gap: 6px; padding-left: 10px; padding-right: 10px; }
  .bubble { max-width: min(260px, calc(100vw - 32px)); font-size: 12px; white-space: normal; }
  .scene-item { font-size: 34px; }
}

/* 抽屉打开时让操作区与物品栏退场 */
.room.drawer-open .actions, .room.drawer-open .inv-bar { display: none; }
@media (prefers-reduced-motion: reduce) {
  .scene-item.shake, .think-cat, .think-dots i, .cursor, .chat-input .rec-btn.recording i { animation: none; }
  .cat-card, .cat-inner, .chip.mini .bar i, .wish-row .track i, :global(.drag-ghost) { transition: none; }
  .modal-box, .chat-box, .wizard-box, .tip-bubble { animation: none; }
}
</style>
