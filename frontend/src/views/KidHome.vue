<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { showDialog, showToast, showConfirmDialog } from 'vant'
import CatSprite from '../components/CatSprite.vue'
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

async function loadAll() {
  const [sp, p, t, w, s, sc] = await Promise.all([
    jget('/species'), jget('/pet'), jget('/tasks/today'), jget('/wish/list'), jget('/shop'), jget('/scene'),
  ])
  if (sp) speciesList.value = sp.species || []
  if (p && p.pet) { adopted.value = true; pet.value = p.pet; species.value = p.pet.species }
  if (t) today.value = t
  hadDuration.value = t.had_duration || {}
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
  const W = roomEl.value?.clientWidth || 800, H = roomEl.value?.clientHeight || 600
  return { x: 80 + Math.random() * (W - 200), y: (0.62 + Math.random() * 0.32) * H }
}
// 校准：catPos ← 猫的实际渲染位置（DOM 和数据有偏差时原地归位，杜绝瞬移）
function syncCatPos() {
  if (!catEl.value || !roomEl.value) return
  const cr = catEl.value.getBoundingClientRect()
  const rr = roomEl.value.getBoundingClientRect()
  catPos.x = cr.left - rr.left + 55
  catPos.y = cr.top - rr.top + 100
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
  const W = roomEl.value.clientWidth
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

const roomEl = ref(null)
const catEl = ref(null)

// 窄屏（手机竖屏）：面板收进底部抽屉，点导航切换
const isNarrow = ref(false)
const sheet = ref('') // '' / 'task' / 'wish' / 'shop'
function openSheet(name) { sheet.value = sheet.value === name ? '' : name }
function checkNarrow() { isNarrow.value = (roomEl.value ? roomEl.value.clientWidth : 1024) < 900 }
window.addEventListener('resize', checkNarrow)

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
function onRoomTap(e) {
  if (!adopted.value) return
  if (e.target.closest('.panel, .chip, .cat-wrap, .pbtn, .inv-bar, .tip-bubble, .adopt-box')) return
  const r = roomEl.value.getBoundingClientRect()
  const x = Math.max(70, Math.min(r.width - 90, e.clientX - r.left))
  const y = Math.max(r.height * 0.58, Math.min(r.height - 20, e.clientY - r.top))
  if (placingFeed.value) { placeBowlAndGo(x, y); return } // 放置猫粮
  say(['来啦来啦！', '等等我～', '马上去！'][Math.floor(Math.random() * 3)])
  walkTo({ x, y }, () => behaviorNext(1500), { speed: 220 }) // 快速跑
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
  shopItems.value.filter((i) => i.owned && !sceneItems.value.find((s) => s.item_key === i.key)))

const ITEM_ICON = { nest: '🛏️', yarn: '🧶', plant: '🪴', fish: '🐟' }

// 拖拽：物品栏拖入房间（新摆放）；场景内拖动改位置；拖到边缘收回
let dragItem = null // {id?, key, el}
function onItemPointerDown(e, it, sceneId) {
  e.preventDefault()
  e.stopPropagation()
  dragItem = { id: sceneId || 0, key: it.key || it.item_key, moved: false }
  const ghost = document.createElement('div')
  ghost.className = 'drag-ghost'
  ghost.textContent = ITEM_ICON[dragItem.key] || '🎁'
  document.body.appendChild(ghost)
  const move = (ev) => {
    dragItem.moved = true
    ghost.style.left = ev.clientX - 20 + 'px'
    ghost.style.top = ev.clientY - 20 + 'px'
    ghost.style.opacity = 1
  }
  const up = async (ev) => {
    document.removeEventListener('pointermove', move)
    document.removeEventListener('pointerup', up)
    ghost.remove()
    if (!dragItem.moved) {
      // 点击（非拖拽）→ 猫跑过去和物品互动
      if (adopted.value) {
        say('我去玩那个！')
        interactWith({ ...it, x: it.x, y: it.y })
      }
      return
    }
    const r = roomEl.value.getBoundingClientRect()
    const x = ev.clientX - r.left, y = ev.clientY - r.top
    // 拖到边缘（上/左/右）→ 收回物品栏
    if (x < 40 || x > r.width - 40 || y < 40) {
      if (dragItem.id) {
        await jpost('/scene/remove', { id: dragItem.id })
        say('收起来啦')
      }
    } else {
      await jpost('/scene/place', { id: dragItem.id || 0, key: dragItem.key, x, y })
    }
    await loadAll()
  }
  document.addEventListener('pointermove', move)
  document.addEventListener('pointerup', up)
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
  catPos.x = roomEl.value.clientWidth * 0.55
  catPos.y = roomEl.value.clientHeight * 0.8
  stateTimer = setTimeout(behaviorTick, 1200)
}
onMounted(async () => {
  await loadAll()
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
onBeforeUnmount(() => clearTimeout(stateTimer))
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
  <div v-else class="room" ref="roomEl" :class="[wpClass, { 'drawer-open': isNarrow && !!sheet }]" @pointerdown="onRoomTap">
    <div class="wall-deco">
      <div class="window"><i class="cloud" /><i class="cloud c2" /></div>
      <div class="baseboard"></div>
      <div class="floorline"></div>
    </div>

    <!-- 场景道具（可拖动改位置，拖到边缘收回；被猫咬会晃） -->
    <div v-for="it in sceneItems" :key="it.id" class="scene-item"
      :class="{ shake: shakeId === it.id }"
      :style="{ left: it.x + 'px', top: it.y + 'px', transition: it.item_key === 'yarn' ? 'left .8s ease' : '' }"
      @pointerdown="(e) => onItemPointerDown(e, it, it.id)">
      {{ ITEM_ICON[it.item_key] || '🎁' }}
    </div>

    <!-- 放置猫粮提示 -->
    <div v-if="placingFeed" class="place-banner">🥣 点一下地面，把猫粮碗放那儿（再点"喂猫粮"可取消）</div>

    <!-- 食物碗（放大，猫低头吃时可见） -->
    <svg v-if="showBowl" class="bowl" :style="{ left: bowlPos.x + 'px', top: bowlPos.y + 'px' }" width="72" height="42" viewBox="0 0 44 26">
      <ellipse cx="22" cy="9" rx="17" ry="6" fill="#C76A84" stroke="#2C2C2A" stroke-width="2" />
      <ellipse cx="22" cy="8" rx="12" ry="4" fill="#A24E72" />
      <path d="M5 9 L9 22 L35 22 L39 9" fill="#E89AB4" stroke="#2C2C2A" stroke-width="2" stroke-linejoin="round" />
    </svg>

    <!-- 猫 -->
    <div class="cat-wrap" ref="catEl" :class="{ sad: moodColor === 'sad' }"
      :style="{ left: catPos.x + 'px', top: catPos.y + 'px' }" @click="petCat">
      <div class="bubble" v-if="showBubble">{{ bubble }}</div>
      <span class="zzz" v-if="catAnim === 'sleep' && zzz">💤</span>
      <div class="cat-inner" :class="{ flip: flipped }">
        <CatSprite :species="species" :anim="catAnim" :size="110" />
      </div>
    </div>

    <!-- HUD（带文字说明 + 悬停/长按提示） -->
    <div class="hud-top">
      <div class="chip">🐱 {{ pet?.name }} <small>{{ today.date }} {{ today.date_multiplier > 1 ? '· 奖励日×' + today.date_multiplier : '' }}</small></div>
      <div class="chip tippable" @mouseenter="showTip('stars')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('stars', e)" @pointerup="tipUp" @pointerleave="tipUp">星星 ⭐{{ pet?.balance }}</div>
      <div class="chip tippable" @mouseenter="showTip('level')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('level', e)" @pointerup="tipUp" @pointerleave="tipUp">等级 Lv{{ pet?.level }}</div>
      <div class="chip tippable" @mouseenter="showTip('streak')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('streak', e)" @pointerup="tipUp" @pointerleave="tipUp">连击 ×{{ pet?.streak }}</div>
      <div class="chip mini tippable" @mouseenter="showTip('hunger')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('hunger', e)" @pointerup="tipUp" @pointerleave="tipUp">饱腹<div class="bar"><i :style="{ width: (pet?.hunger || 0) + '%' }" /></div><small>{{ Math.round(pet?.hunger || 0) }}</small></div>
      <div class="chip mini tippable" @mouseenter="showTip('mood')" @mouseleave="hideTip" @pointerdown="(e) => tipDown('mood', e)" @pointerup="tipUp" @pointerleave="tipUp">心情<div class="bar"><i class="mood" :style="{ width: (pet?.mood || 0) + '%' }" /></div><small>{{ Math.round(pet?.mood || 0) }}</small></div>
      <div class="chip switch" @click="switchKid" title="换一个小朋友登录">👤 换人</div>
    </div>

    <!-- 提示气泡（触屏长按后松手/移开消失） -->
    <div v-if="tipShow" class="tip-bubble" @pointerdown="hideTip">{{ tipText }}</div>

    <!-- 侧栏容器：桌面=右侧自适应列；窄屏=底部抽屉 -->
    <div class="sidebar" :class="{ open: !!sheet, narrow: isNarrow }">
    <div class="panel" id="taskPanel" v-show="!isNarrow || sheet === 'task'">
      <div class="panel-bar">📋 今日任务（{{ today.tasks.filter(t => t.status === 'approved').length }}/{{ today.tasks.length }}）</div>
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
      <div class="panel-bar">🌠 我的愿望 <a @click="newWish">+ 许愿</a></div>
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
      <div class="panel-bar">🛒 像素商店</div>
      <div class="panel-body">
        <div v-for="it in shopItems" :key="it.key" class="shop-row">
          <span>{{ it.name }}</span><em>{{ it.price }}⭐</em>
          <button :class="{ owned: it.owned }" @click="buyItem(it)">{{ it.owned ? '已购' : '购买' }}</button>
        </div>
      </div>
    </div>

    </div><!-- /sidebar -->

    <!-- 窄屏底部导航 -->
    <div v-if="isNarrow" class="sheet-nav">
      <button :class="{ on: sheet === 'task' }" @click="openSheet('task')">📋 任务</button>
      <button :class="{ on: sheet === 'wish' }" @click="openSheet('wish')">🌠 愿望</button>
      <button :class="{ on: sheet === 'shop' }" @click="openSheet('shop')">🛒 商店</button>
    </div>

    <!-- 物品栏：已购未摆放的道具，拖入房间 -->
    <div v-if="inventory.length" class="inv-bar">
      <span class="lbl">拖我进房间→</span>
      <div v-for="it in inventory" :key="it.key" class="inv-item"
        @pointerdown="(e) => onItemPointerDown(e, it, 0)">{{ ITEM_ICON[it.key] || '🎁' }}</div>
    </div>

    <div class="actions">
      <button class="pbtn feed" @click="feed">🍖 喂猫粮<small>10⭐{{ placingFeed ? ' · 放置中' : '' }}</small></button>
      <button class="pbtn hint" @click="petCat">👆 摸摸头</button>
      <button class="pbtn chatbtn" @click="openChat">💬 聊天<small>剩{{ chatLeft }}条</small></button>
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
.adopt-mask { position: fixed; inset: 0; z-index: 100; background: rgba(250, 243, 227, .97); display: flex; align-items: center; justify-content: center; overflow-y: auto; }
.adopt-box { max-width: 620px; width: 92vw; background: #fff; border: 2px solid #2C2C2A; border-radius: 14px; box-shadow: 6px 6px 0 rgba(44, 44, 42, .85); padding: 24px; margin: 20px; }
.adopt-box h2 { font-size: 22px; text-align: center; margin-bottom: 4px; }
.adopt-box .sub { text-align: center; font-size: 13px; color: #888780; margin-bottom: 18px; }
.cats { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 12px; margin-bottom: 18px; }
.cat-card { border: 2px solid #2C2C2A; border-radius: 10px; padding: 12px 8px; text-align: center; cursor: pointer; box-shadow: 3px 3px 0 rgba(44, 44, 42, .6); background: #FAF3E3; }
.cat-card:hover { transform: translateY(-4px); }
.cat-card.sel { background: #EEEDFE; border-color: #534AB7; border-width: 3px; }
.cat-card .nm { font-size: 14px; font-weight: 700; margin-top: 6px; }
.cat-card .ds { font-size: 11px; color: #888780; margin-top: 2px; }
.code-input { width: 100%; font-size: 22px; text-align: center; letter-spacing: 8px; border: 2px solid #2C2C2A; border-radius: 10px; padding: 12px; outline: none; margin-bottom: 12px; }
.enter-btn { width: 100%; font-size: 16px; font-weight: 700; color: #fff; background: #534AB7; border: 2px solid #2C2C2A; border-radius: 10px; padding: 12px; cursor: pointer; box-shadow: 3px 3px 0 rgba(44,44,42,.7); }
.tiny { font-size: 11px; color: #888780; margin-top: 12px; text-align: center; }
.pick-list { display: flex; flex-direction: column; gap: 10px; margin: 16px 0; }
.pick-btn small { font-size: 12px; color: #888780; font-weight: 400; margin-left: 8px; }
.pick-btn { font-size: 17px; font-weight: 700; background: #fff; border: 2px solid #2C2C2A; border-radius: 12px; padding: 14px; cursor: pointer; box-shadow: 3px 3px 0 rgba(44,44,42,.6); }
.pick-btn:active { transform: translate(2px, 2px); box-shadow: none; }
.new-kid { display: block; text-align: center; color: #534AB7; font-size: 13px; font-weight: 600; cursor: pointer; padding: 8px; }
.adopt-row { display: flex; gap: 10px; }
.adopt-row input { flex: 1; font-size: 14px; border: 2px solid #2C2C2A; border-radius: 8px; padding: 9px 12px; outline: none; }
.adopt-row button { font-size: 15px; font-weight: 700; cursor: pointer; color: #fff; background: #534AB7; border: 2px solid #2C2C2A; border-radius: 8px; padding: 9px 22px; box-shadow: 3px 3px 0 rgba(44, 44, 42, .85); }

.room { position: fixed; inset: 0; overflow: hidden; background: linear-gradient(#FDF6E3 0%, #FBEFD1 55%, #EFE3BC 56%, #E9C58F 57%, #E3BB78 100%); }
.wall-deco, .window { position: absolute; inset: 0; pointer-events: none; }
.window { left: 10%; top: 7%; width: 150px; height: 110px; border: 6px solid #B48A5A; border-radius: 10px; background: linear-gradient(#AED9F2 60%, #C9E8F7); }
.cloud { position: absolute; width: 34px; height: 12px; background: #fff; border-radius: 8px; top: 18px; animation: drift 14s linear infinite; }
.cloud.c2 { top: 40px; animation-delay: 6s; width: 24px; }
@keyframes drift { 0% { left: -40px; } 100% { left: 130px; } }
.baseboard { position: absolute; left: 0; right: 0; top: calc(56% + 1px); height: 10px; background: #D8B183; border-bottom: 3px solid #C4A06C; }
.floorline { position: absolute; left: 0; right: 0; top: 57%; bottom: 0; background: repeating-linear-gradient(0deg, transparent 0 46px, rgba(44, 44, 42, .07) 46px 49px); }

.cat-wrap { position: absolute; width: 110px; margin-left: -55px; margin-top: -100px; cursor: pointer; z-index: 5; }
.cat-inner { transition: transform .3s; transform-origin: 50% 90%; }
.cat-inner.flip { transform: scaleX(-1); }
.cat-wrap.sad { filter: grayscale(.75); }
.bubble { position: absolute; left: 50%; top: -46px; transform: translateX(-50%); background: #fff; border: 2px solid #2C2C2A; border-radius: 10px; padding: 6px 12px; font-size: 12px; font-weight: 600; white-space: nowrap; box-shadow: 2px 2px 0 rgba(44, 44, 42, .7); }

.hud-top { position: absolute; top: 12px; left: 12px; display: flex; gap: 8px; flex-wrap: wrap; z-index: 10; max-width: 62%; }
.chip { display: flex; align-items: center; gap: 6px; background: #fff; border: 2px solid #2C2C2A; border-radius: 8px; box-shadow: 3px 3px 0 rgba(44, 44, 42, .85); padding: 5px 10px; font-weight: 600; font-size: 13px; }
.chip small { font-size: 10px; font-weight: 400; color: #888780; }
.chip.mini .bar { width: 60px; height: 10px; background: #F1EFE8; border: 2px solid #2C2C2A; border-radius: 5px; overflow: hidden; }
.chip.mini .bar i { display: block; height: 100%; background: linear-gradient(90deg, #F0997B, #D85A30); transition: width .5s; }
.chip.mini .bar i.mood { background: linear-gradient(90deg, #97C459, #3B6D11); }

.panel { background: #fff; border: 2px solid #2C2C2A; border-radius: 10px; box-shadow: 4px 4px 0 rgba(44, 44, 42, .85); z-index: 10; width: 248px; }
.panel-bar { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-bottom: 2px solid #2C2C2A; font-size: 13px; font-weight: 600; background: #E1F5EE; }
.panel.wish .panel-bar { background: #EEEDFE; }
.panel.shop .panel-bar { background: #FAEEDA; }
.panel-bar a { margin-left: auto; color: #534AB7; font-size: 12px; cursor: pointer; }
.panel-body { padding: 10px; }
/* 侧栏：桌面=右侧自适应列（永不互相遮挡）；窄屏=底部抽屉 */
.sidebar { position: absolute; top: 12px; right: 12px; bottom: 84px; width: 248px; display: flex; flex-direction: column; gap: 12px; z-index: 10; }
.sidebar .panel { position: static; flex: 0 1 auto; min-height: 0; display: flex; flex-direction: column; }
.sidebar .panel .panel-body { flex: 1 1 auto; min-height: 0; overflow-y: auto; max-height: none; }
#taskPanel { flex-grow: 2; }
#wishPanel, #shopPanel { flex-grow: 1; }
/* 窄屏：底部抽屉（隐藏在屏幕外，open 弹出） */
.sidebar.narrow { top: auto; left: 0; right: 0; bottom: 112px; width: auto; max-height: 56vh; transform: translateY(calc(100% + 130px)); transition: transform .28s ease; gap: 0; }
.sidebar.narrow.open { transform: none; }
.sidebar.narrow .panel { width: 100%; max-height: 56vh; }
.sheet-nav { position: absolute; left: 12px; right: 12px; bottom: 12px; z-index: 30; display: flex; gap: 8px; }
.sheet-nav button { flex: 1; font-size: 13px; font-weight: 700; cursor: pointer; padding: 10px 0; background: #fff; border: 2px solid #2C2C2A; border-radius: 10px; box-shadow: 3px 3px 0 rgba(44,44,42,.85); }
.sheet-nav button.on { background: #534AB7; color: #fff; }
.task { display: flex; align-items: center; gap: 8px; border: 2px solid #2C2C2A; border-radius: 8px; padding: 7px 8px; background: #fff; margin-bottom: 8px; }
.task.done { background: #E1F5EE; }
.task.wait { background: #EEEDFE; }
.task .ic { width: 30px; height: 30px; display: flex; align-items: center; justify-content: center; font-size: 17px; border: 2px solid #2C2C2A; border-radius: 6px; background: #FAEEDA; }
.task .info { flex: 1; min-width: 0; }
.task .nm { font-size: 12.5px; font-weight: 600; }
.task .sub { font-size: 10.5px; color: #888780; }
.task button, .shop-row button { font-size: 11px; font-weight: 600; cursor: pointer; border: 2px solid #2C2C2A; border-radius: 6px; padding: 4px 10px; background: #EEEDFE; box-shadow: 2px 2px 0 rgba(44, 44, 42, .85); }
.task button:active { transform: translate(2px, 2px); box-shadow: none; }
.st.ok { color: #1D9E75; font-weight: 700; }
.st.wait-t { color: #534AB7; font-weight: 700; font-size: 12px; }
.st.skip { color: #888780; }
.empty { font-size: 12px; color: #888780; text-align: center; padding: 8px; }
.wish-row { margin-bottom: 10px; }
.wish-row .wt { font-size: 13px; font-weight: 700; margin-bottom: 4px; }
.wish-row .track { height: 16px; background: #F1EFE8; border: 2px solid #2C2C2A; border-radius: 8px; overflow: hidden; position: relative; }
.wish-row .track i { display: block; height: 100%; background: linear-gradient(90deg, #FAC775, #EF9F27); transition: width .6s; }
.wish-row .track span { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font-size: 10.5px; font-weight: 700; }
.wsub { font-size: 11px; color: #888780; }
.wsub.done { color: #EF9F27; font-weight: 700; }
.shop-row { display: flex; align-items: center; gap: 8px; font-size: 12px; margin-bottom: 6px; }
.shop-row span { flex: 1; font-weight: 600; }
.shop-row em { font-style: normal; color: #854F0B; font-weight: 700; }
.shop-row button.owned { background: #E1F5EE; color: #1D9E75; }

.modal-mask { position: absolute; inset: 0; z-index: 95; background: rgba(44,44,42,.35); display: flex; align-items: center; justify-content: center; }
.modal-box { width: min(360px, 88vw); background: #fff; border: 2px solid #2C2C2A; border-radius: 14px; box-shadow: 6px 6px 0 rgba(44,44,42,.6); padding: 20px; animation: tipin .2s ease; }
.modal-box h3 { font-size: 17px; margin-bottom: 12px; }
.modal-box input { width: 100%; font-size: 14px; border: 2px solid #2C2C2A; border-radius: 8px; padding: 10px 12px; outline: none; margin-bottom: 10px; }
.modal-btns { display: flex; gap: 10px; margin-top: 6px; }
.modal-btns button { flex: 1; font-size: 14px; font-weight: 700; cursor: pointer; border: 2px solid #2C2C2A; border-radius: 8px; padding: 10px; box-shadow: 3px 3px 0 rgba(44,44,42,.6); }
.modal-btns .primary { background: #534AB7; color: #fff; }
.modal-btns .ghost { background: #F1EFE8; }
.wish-row .cancel { float: right; color: #A32D2D; font-weight: 700; cursor: pointer; margin-left: 8px; font-size: 12px; padding: 2px 6px; }

.place-banner { position: absolute; left: 50%; top: 18px; transform: translateX(-50%); z-index: 90; background: #EF9F27; color: #fff; border: 2px solid #2C2C2A; border-radius: 10px; padding: 8px 16px; font-size: 13px; font-weight: 700; box-shadow: 3px 3px 0 rgba(44,44,42,.5); animation: tipin .2s ease; }
.room { cursor: default; }
.room:has(.place-banner) { cursor: crosshair; }
.bowl { position: absolute; transform: translate(-50%, -100%); z-index: 6; } /* z 高于道具低于猫：低头吃时可见 */

/* 猫聊天 */
.pbtn.chatbtn { background: #EF9F27; font-size: 13px; }
/* AI 分析状态层 */
.think-mask { position: absolute; inset: 0; z-index: 98; background: rgba(44,44,42,.55); display: flex; align-items: center; justify-content: center; }
.think-box { background: #FFF9EE; border: 2px solid #2C2C2A; border-radius: 18px; padding: 28px 32px; text-align: center; box-shadow: 4px 4px 0 rgba(44,44,42,.6); }
.think-cat { font-size: 44px; animation: bouncecat 1.2s infinite; }
@keyframes bouncecat { 0%,100% { transform: translateY(0); } 50% { transform: translateY(-8px); } }
.think-box b { display: block; margin: 10px 0 8px; font-size: 16px; }
.think-dots { display: flex; gap: 5px; justify-content: center; margin-bottom: 8px; }
.think-dots i { width: 7px; height: 7px; background: #EF9F27; border-radius: 50%; animation: dotp 1s infinite; }
.think-dots i:nth-child(2) { animation-delay: .2s; }
.think-dots i:nth-child(3) { animation-delay: .4s; }
@keyframes dotp { 40% { opacity: .25; transform: scale(.8); } }
.think-box small { color: #888780; font-size: 12px; }

/* 向导内证据预览 */
.wprev { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 8px; }
.wprev-item { position: relative; }
.wprev-item img { width: 74px; height: 74px; object-fit: cover; border: 2px solid #2C2C2A; border-radius: 8px; cursor: zoom-in; }
.wprev-item audio { height: 36px; max-width: 200px; }
.wprev-del { position: absolute; top: -8px; right: -8px; width: 20px; height: 20px; background: #A32D2D; color: #fff; border-radius: 50%; display: flex; align-items: center; justify-content: center; font-size: 11px; cursor: pointer; border: 1.5px solid #fff; }

/* 照片大图 */
.pprev-mask { position: absolute; inset: 0; z-index: 99; background: rgba(0,0,0,.85); display: flex; align-items: center; justify-content: center; }
.pprev-mask img { max-width: 94%; max-height: 94%; border-radius: 10px; }

/* 提交向导 */
.wizard-box { width: min(460px, 94vw); max-height: 70vh; background: #FFF9EE; border: 2px solid #2C2C2A; border-radius: 18px; box-shadow: 0 -4px 0 rgba(44,44,42,.5); display: flex; flex-direction: column; animation: chatin .22s ease; }
.wizard-body { flex: 1; overflow-y: auto; padding: 14px; }
.wstep { background: #fff; border: 2px solid #2C2C2A; border-radius: 12px; padding: 12px; margin-bottom: 10px; }
.wstep.done { background: #E8F5E9; border-color: #A5D6A7; }
.wstep-head { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.wstep-head b { font-size: 14px; }
.wstep-head small { font-size: 11px; color: #888780; }
.wstep-head em { font-style: normal; margin-left: auto; color: #2E7D32; font-weight: 700; font-size: 13px; }
.wstep .pbtn { width: 100%; background: #EF9F27; color: #fff; border: 2px solid #2C2C2A; border-radius: 10px; padding: 10px; font-weight: 700; cursor: pointer; box-shadow: 2px 2px 0 rgba(44,44,42,.6); }
.wizard-foot { padding: 10px 14px; border-top: 2px solid #2C2C2A; }
.ok-btn { width: 100%; font-size: 16px; font-weight: 800; color: #fff; background: #534AB7; border: 2px solid #2C2C2A; border-radius: 12px; padding: 12px; cursor: pointer; box-shadow: 3px 3px 0 rgba(44,44,42,.7); }
.ok-btn:disabled { opacity: .4; }
.chat-mask { position: absolute; inset: 0; z-index: 96; background: rgba(44,44,42,.3); display: flex; align-items: flex-end; justify-content: center; }
.chat-box { width: min(460px, 94vw); height: 62vh; background: #FFF9EE; border: 2px solid #2C2C2A; border-radius: 18px 18px 0 0; box-shadow: 0 -4px 0 rgba(44,44,42,.5); display: flex; flex-direction: column; animation: chatin .22s ease; }
@keyframes chatin { from { transform: translateY(40px); opacity: 0; } }
.chat-head { display: flex; align-items: center; gap: 10px; padding: 12px 16px; border-bottom: 2px solid #2C2C2A; background: #FAEEDA; border-radius: 18px 18px 0 0; }
.chat-head b { font-size: 15px; }
.chat-left { font-size: 11px; color: #888780; margin-left: auto; }
.chat-head a { cursor: pointer; font-size: 15px; padding: 2px 8px; }
.chat-body { flex: 1; overflow-y: auto; padding: 14px; }
.msg { display: flex; gap: 8px; margin-bottom: 12px; }
.msg.user { flex-direction: row-reverse; }
.msg .avatar { width: 34px; height: 34px; flex: 0 0 34px; display: flex; align-items: center; justify-content: center; background: #fff; border: 2px solid #2C2C2A; border-radius: 50%; font-size: 17px; }
.msg .bubble-txt { max-width: 74%; background: #fff; border: 2px solid #2C2C2A; border-radius: 12px; padding: 8px 12px; font-size: 14px; line-height: 1.6; white-space: pre-wrap; word-break: break-word; }
.msg.user .bubble-txt { background: #EEEDFE; }
.cursor { animation: blink .8s infinite; font-style: normal; }
@keyframes blink { 50% { opacity: 0; } }
.chat-input { display: flex; gap: 8px; padding: 10px 12px; border-top: 2px solid #2C2C2A; background: #fff; }
.chat-input input { flex: 1; border: 2px solid #2C2C2A; border-radius: 10px; padding: 10px 12px; font-size: 14px; outline: none; }
.rec-btn { flex: 0 0 46px; height: 44px; font-size: 20px; background: #fff; border: 2px solid #2C2C2A; border-radius: 10px; cursor: pointer; position: relative; box-shadow: 2px 2px 0 rgba(44,44,42,.6); }
.rec-btn.recording { background: #A32D2D; color: #fff; border-color: #6B1F1F; }
.rec-btn.recording i { display: inline-block; width: 6px; height: 6px; background: #fff; border-radius: 50%; margin-left: 4px; animation: pulse 0.8s infinite; }
.rec-btn.uploading { background: #FAEEDA; }
@keyframes pulse { 50% { opacity: .2; } }
.chat-input button { font-size: 14px; font-weight: 700; color: #fff; background: #EF9F27; border: 2px solid #2C2C2A; border-radius: 10px; padding: 8px 16px; cursor: pointer; box-shadow: 2px 2px 0 rgba(44,44,42,.7); }
.chat-input button:disabled { opacity: .5; }

.inv-bar { position: absolute; bottom: 12px; left: 50%; transform: translateX(-50%); z-index: 11; background: #fff; border: 2px solid #2C2C2A; border-radius: 12px; box-shadow: 4px 4px 0 rgba(44,44,42,.85); padding: 6px 10px; display: flex; gap: 8px; align-items: center; }
.inv-bar .lbl { font-size: 11px; color: #888780; }
.inv-item { width: 40px; height: 40px; display: flex; align-items: center; justify-content: center; border: 2px dashed #2C2C2A; border-radius: 8px; cursor: grab; user-select: none; background: #FAF3E3; font-size: 22px; touch-action: none; }
.scene-item { position: absolute; transform: translate(-50%, -100%); font-size: 40px; z-index: 4; cursor: grab; touch-action: none; }
.drag-ghost { position: fixed; width: 40px; height: 40px; display: flex; align-items: center; justify-content: center; font-size: 22px; opacity: 0; pointer-events: none; z-index: 999; transition: opacity .15s; }

.actions { position: absolute; bottom: 12px; left: 12px; display: flex; gap: 10px; z-index: 10; }
.pbtn { font-size: 15px; font-weight: 600; cursor: pointer; padding: 10px 18px; color: #fff; border: 2px solid #2C2C2A; border-radius: 10px; box-shadow: 3px 3px 0 rgba(44, 44, 42, .85); }
.pbtn:active { transform: translate(3px, 3px); box-shadow: none; }
.pbtn.feed { background: #F0997B; }
.pbtn.hint { background: #534AB7; font-size: 13px; }
.pbtn small { display: block; font-size: 10px; font-weight: 400; opacity: .9; }
.chip.switch { cursor: pointer; background: #EEEDFE; color: #534AB7; }
.chip.switch:active { transform: translate(1px, 1px); }
.chip.tippable { cursor: help; -webkit-touch-callout: none; user-select: none; }
.chip.mini small { font-size: 10px; color: #5F5E5A; font-weight: 700; }
.tip-bubble { position: absolute; left: 12px; top: 54px; z-index: 90; max-width: 300px; background: #2C2C2A; color: #FDF6E3; border: 2px solid #FDF6E3; border-radius: 10px; padding: 10px 14px; font-size: 12px; line-height: 1.65; box-shadow: 3px 3px 0 rgba(44,44,42,.4); animation: tipin .18s ease; }
@keyframes tipin { from { opacity: 0; transform: translateY(-6px); } to { opacity: 1; transform: none; } }

.scene-item.shake { animation: shake .5s ease-in-out 3; }
@keyframes shake { 0%, 100% { transform: translate(-50%, -100%) rotate(0); } 30% { transform: translate(-50%, -100%) rotate(-8deg); } 60% { transform: translate(-50%, -100%) rotate(6deg); } }

@media (max-width: 900px) {
  .inv-bar { bottom: 112px; }
  .actions { bottom: 112px; }
  .task .sub { display: none; }
}
/* 抽屉打开时隐藏底部操作区（喂猫/摸头/物品栏让位给任务卡） */
.room.drawer-open .actions, .room.drawer-open .inv-bar { display: none; }
@media (min-width: 901px) and (max-width: 760px) { }
</style>
