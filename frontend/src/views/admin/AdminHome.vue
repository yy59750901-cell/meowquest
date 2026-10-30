<script setup>
// 家长后台（Vant 移动组件库）：确认队列 / 任务配置 / 愿望审批 / 流水
function evText(t) {
  const m = { photo: '📷', audio: '🎙', video: '🎬', timer: '⏱' }
  return t.split(',').map((x) => m[x] || x).join('')
}
function parseEvidence(ev) {
  if (!ev) return []
  if (Array.isArray(ev)) return ev // API 已是数组
  try {
    const p = JSON.parse(ev)
    return Array.isArray(p) ? p : []
  } catch (e) { return [] }
}
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import TaskReviewCard from '../../components/TaskReviewCard.vue'
import { reviewPending } from '../../ai-review.js'
import { useRouter } from 'vue-router'
import { showToast, showConfirmDialog } from 'vant'
import { evidenceSteps } from '../../task-evidence.js'

const router = useRouter()
const tab = ref('confirm')
// 切 tab 即拉最新数据（tabs 不销毁组件，onMounted 只跑一次会留旧数据）
watch(tab, () => { if (tab.value === 'history') loadHistory(1); else load() })
const confirms = ref([])
const rejectedItems = ref([])
const historyItems = ref([])
const historyPage = ref(1)
const historyTotal = ref(0)
const historyLoading = ref(false)
const historyError = ref('')
const busyReview = ref(0)
const now = ref(Date.now())
let statusTicker = null
let loadingReviews = false
const itemKey = it => it.completion_id || it.id
async function loadReviews() {
  if (loadingReviews) return
  loadingReviews = true
  try {
    const c = await jget('/api/admin/confirm/list')
    if (Array.isArray(c.items)) confirms.value = c.items
    if (Array.isArray(c.rejected)) rejectedItems.value = c.rejected
  } catch { /* 保留原数据，下一次轮询恢复 */ }
  finally { loadingReviews = false }
}
async function loadHistory(page = 1) {
  if (historyLoading.value) return
  historyLoading.value = true; historyError.value = ''
  try {
    const data = await jget(`/api/admin/confirm/history?page=${page}&page_size=20`)
    if (!Array.isArray(data.items)) throw new Error(data.error || '历史加载失败')
    historyItems.value = page === 1 ? data.items : [...historyItems.value, ...data.items]
    historyPage.value = page; historyTotal.value = data.total || 0
  } catch (e) { historyError.value = e.message }
  finally { historyLoading.value = false }
}
const expanded = ref(0) // 待确认卡片展开的 id（默认全收起）
function toggleExpand(id) { expanded.value = expanded.value === id ? 0 : id }
function snapOf(it) {
  try { return typeof it.snapshot === 'string' ? JSON.parse(it.snapshot) : (it.snapshot || {}) } catch (e) { return {} }
}
function safeReview(raw) {
  try {
    const v = typeof raw === 'string' ? JSON.parse(raw) : raw
    return { pass: !!v.pass, score: v.score || 0, reason: v.reason || 'AI 已检查', kid_feedback: v.kid_feedback || '' }
  } catch (e) { return { pass: false, score: 0, reason: String(raw).slice(0, 50), kid_feedback: '' } }
}
const tasks = ref([])
const wishes = ref([])
const wishHealth = ref(0)
const ledger = ref([])
const newTask = ref({ name: '', icon: '⭐', category: 'custom', goal_type: 'duration', goal_value: 30, confirm_mode: 'c', base_score: 20, date_mode: 'weekday_fixed', weekdays: '1,2,3,4,5', weekly_quota: 3, review_prompt: '' })
const evSteps = ref([]) // 提交步骤：[{kind,label,required}]
const ICONS = ['⭐', '🎹', '📖', '🔤', '🏃', '🧹', '✍️', '🎨', '🧠', '🦷']
const editingId = ref(0) // >0 = 编辑模式

function editTask(t) {
  let steps
  try { steps = evidenceSteps(t.evidence_steps, t.evidence_type || '') }
  catch { return showToast('原提交步骤配置无效，未打开编辑，避免覆盖材料要求') }
  editingId.value = t.id
  evSteps.value = steps
  newTask.value = {
    name: t.name, icon: t.icon || '⭐', category: t.category || 'custom',
    goal_type: t.goal_type || 'duration', goal_value: t.goal_value || 0, review_prompt: t.review_prompt || '', analysis_mode: t.analysis_mode || 'judge',
    confirm_mode: (t.confirm_mode || '').includes('b') ? 'b' : t.confirm_mode || 'c', base_score: t.base_score || 20,
    date_mode: t.schedule?.date_mode || 'weekday_fixed',
    weekdays: t.schedule?.weekdays || '1,2,3,4,5',
    weekly_quota: t.schedule?.weekly_quota || 3,
  }
}
function resetTaskForm() {
  editingId.value = 0
  evSteps.value = []
  newTask.value = { name: '', icon: '⭐', category: 'custom', goal_type: 'duration', goal_value: 30, confirm_mode: 'c', base_score: 20, date_mode: 'weekday_fixed', weekdays: '1,2,3,4,5', weekly_quota: 3, review_prompt: '' }
}

const admHeaders = () => ({ 'Content-Type': 'application/json', 'X-Admin-Token': localStorage.getItem('admin_token') || '' })

// 登录失效：明确提示 + 清 token + 跳登录页
function expired() {
  showToast('登录过期，请重新输 PIN')
  localStorage.removeItem('admin_token')
  router.push('/admin')
}
async function jget(url) {
  const r = await fetch(url, { headers: { 'X-Admin-Token': localStorage.getItem('admin_token') || '' } })
  if (r.status === 401) { expired(); return {} }
  return r.json()
}
async function jpost(url, body) {
  const r = await fetch(url, { method: 'POST', headers: admHeaders(), body: JSON.stringify(body || {}) })
  if (r.status === 401) { expired(); return { ok: false } }
  return { ok: r.ok, data: await r.json().catch(() => ({})) }
}
async function jput(url, body) {
  const r = await fetch(url, { method: 'PUT', headers: admHeaders(), body: JSON.stringify(body) })
  if (r.status === 401) { expired(); return { ok: false } }
  return { ok: r.ok, data: await r.json().catch(() => ({})) }
}

async function load() {
  const [c, t, w, l] = await Promise.all([
    jget('/api/admin/confirm/list'), jget('/api/admin/task'), jget('/api/admin/wish/list'), jget('/api/admin/ledger'),
  ])
  confirms.value = (c.items || []).map(x => ({ ...x, snapshot: typeof x.snapshot === 'string' ? JSON.parse(x.snapshot) : x.snapshot }))
  rejectedItems.value = c.rejected || []
  tasks.value = t.tasks || []
  wishes.value = w.wishes || []
  wishHealth.value = w.daily_stars_avg || 0
  ledger.value = l.items || []
}

async function decideReview(it, approve) {
  if (busyReview.value) return
  busyReview.value = itemKey(it)
  try {
    const incomplete = reviewPending(it.ai_review)
    if (!approve || incomplete) await showConfirmDialog({
      title: incomplete ? 'AI 尚未完成，提前审核？' : '驳回补交',
      message: incomplete ? '确认后将停止本次 AI 分析；你的决定不会被晚到的 AI 结果覆盖。' : '孩子可以补交材料，已达标计时不用重做。',
    })
    const { ok, data } = await jpost(`/api/admin/confirm/${it.daily_task_id || it.id}`, {
      approve, completion_id: it.completion_id, reason: approve ? '' : '请补充材料，再认真试一次哦～',
    })
    if (!ok) return showToast(data.error || '审核失败，请刷新后再试')
    showToast(approve ? '已通过，AI 评价可在审核历史回看' : '已退回，可补交材料')
    expanded.value = 0; await loadReviews()
  } catch (e) { if (e !== 'cancel' && e !== 'close') showToast('网络异常，请刷新核对任务状态') }
  finally { busyReview.value = 0 }
}
async function retryReview(it) {
  if (busyReview.value) return
  busyReview.value = itemKey(it)
  try {
    const { ok, data } = await jpost(`/api/admin/confirm/${it.completion_id}/ai-retry`, {})
    if (!ok) return showToast(data.error || '重试失败')
    showToast('已加入后台队列，无需让孩子重新提交'); await loadReviews()
  } catch { showToast('网络异常，请稍后查看执行状态') }
  finally { busyReview.value = 0 }
}

async function createTask() {
  if (!newTask.value.name) return showToast('任务名必填')
  let steps
  try { steps = evidenceSteps(evSteps.value) }
  catch { return showToast('请检查提交步骤的类型和必须选项') }
  const payload = { ...newTask.value,
    evidence_type: [...new Set(steps.map(st => st.kind))].join(',') || 'none',
    evidence_steps: JSON.stringify(steps),
  }
  if (editingId.value) {
    // 编辑模式：PUT 更新
    const { ok, data } = await jput(`/api/admin/task/${editingId.value}`, payload)
    if (ok) {
      showToast((data.today_hit || 0) > 0 ? '已保存，今天就会出现在孩子端 ✓' : '已保存（今天不在安排的周几）')
      resetTaskForm(); await load()
    }
    return
  }
  const { ok, data } = await jpost('/api/admin/task', payload)
  if (ok) {
    showToast((data.today_hit || 0) > 0 ? '已创建，今天就会出现在孩子端 ✓' : '已创建（今天不在安排的周几，命中日出现）')
    resetTaskForm(); await load()
  }
}

const priceModal = ref(null) // { wish, price }
async function wishAction(w, action) {
  if (action === 'approve') {
    priceModal.value = { wish: w, price: w.hoped_price || 200 }
    return
  } else if (action === 'reject') {
    await jpost(`/api/admin/wish/${w.id}`, { action, reason: '这个我们换个更值得的目标好吗？' })
  } else if (action === 'fulfill') {
    await jpost(`/api/admin/wish/${w.id}`, { action })
  }
  showToast('操作成功')
  await load()
}

async function confirmPrice() {
  const pm = priceModal.value
  if (!pm) return
  const price = parseInt(pm.price) || 0
  if (price <= 0) return showToast('星星数要大于 0')
  await jpost(`/api/admin/wish/${pm.wish.id}`, { action: 'approve', price })
  priceModal.value = null
  showToast(`已定价 ${price} 星`)
  await load()
}

const statusText = { pending: '待定价', saving: '攒星星中', redeem_requested: '待兑现', fulfilled: '已实现', rejected: '已取消' }

onMounted(() => {
  // 无 token / token 失效直接引导去登录（刷新/直链场景）
  if (!localStorage.getItem('admin_token')) {
    router.replace('/admin')
    return
  }
  load()
  statusTicker = setInterval(() => {
    now.value = Date.now()
    if (!document.hidden && tab.value === 'confirm') loadReviews()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(statusTicker))
</script>

<template>
  <div class="admin">
    <van-tabs v-model:active="tab" sticky :ellipsis="false">
      <van-tab title="✅ 待确认" name="confirm">
        <div v-if="!confirms.length" class="empty">没有待确认的任务</div>
        <TaskReviewCard v-for="it in confirms" :key="itemKey(it)" :item="it" :now="now"
          :expanded="expanded === itemKey(it)" :busy="!!busyReview"
          @toggle="toggleExpand(itemKey(it))" @approve="decideReview(it, true)" @reject="decideReview(it, false)" @retry="retryReview(it)" />
        <div v-if="rejectedItems.length" class="reject-block">
          <b>近期退回记录（{{ rejectedItems.length }}）</b>
          <small>孩子可以补交，原提交及 AI 评价仍保留。</small>
          <TaskReviewCard v-for="it in rejectedItems" :key="'r' + itemKey(it)" :item="it" history :now="now"
            :expanded="expanded === itemKey(it)" @toggle="toggleExpand(itemKey(it))" />
        </div>
      </van-tab>

      <van-tab title="审核历史" name="history">
        <div class="history-tools"><small>每次提交独立保留，审核通过后也能回看 AI 评价和材料。</small><button :disabled="historyLoading" @click="loadHistory(1)">刷新</button></div>
        <div v-if="historyError" class="history-tools">{{ historyError }}<button @click="loadHistory(historyPage)">重试</button></div>
        <div v-if="!historyItems.length && !historyLoading && !historyError" class="empty">还没有已处理的提交</div>
        <TaskReviewCard v-for="it in historyItems" :key="itemKey(it)" :item="it" history :now="now"
          :expanded="expanded === itemKey(it)" @toggle="toggleExpand(itemKey(it))" />
        <button v-if="historyItems.length < historyTotal" class="history-more" :disabled="historyLoading" @click="loadHistory(historyPage + 1)">{{ historyLoading ? '加载中…' : '加载更多' }}</button>
        <p v-if="historyItems.length" class="history-count">已显示 {{ historyItems.length }} / {{ historyTotal }} 条</p>
      </van-tab>

      <van-tab title="📋 任务配置" name="tasks">
        <div class="card">
          <div class="card-title">
            <b>{{ editingId ? '✏️ 修改任务 #' + editingId : '➕ 新建任务' }}</b>
            <a v-if="editingId" class="cancel-edit" @click="resetTaskForm">取消编辑</a>
          </div>
          <div class="f-row">
            <label>任务名</label>
            <input v-model="newTask.name" placeholder="如：琴键小挑战" />
          </div>
          <div class="grid2">
            <div class="f-cell">
              <label>执行日期</label>
              <select v-model="newTask.date_mode">
                <option value="weekday_fixed">固定每周几</option>
                <option value="weekly_quota">每周完成 N 次</option>
              </select>
            </div>
            <div class="f-cell" v-if="newTask.date_mode === 'weekday_fixed'">
              <label>哪几天</label>
              <input v-model="newTask.weekdays" placeholder="1=一 … 7=日，如 1,3,5" />
            </div>
            <div class="f-cell" v-else>
              <label>每周次数</label>
              <input v-model.number="newTask.weekly_quota" type="number" placeholder="如 4" />
            </div>
          </div>
          <div class="grid2">
            <div class="f-cell">
              <label>任务类型</label>
              <select v-model="newTask.goal_type">
                <option value="duration">⏱ 计时任务</option>
                <option value="free">自由完成</option>
              </select>
            </div>
            <div class="f-cell" v-if="newTask.goal_type === 'duration'">
              <label>时长（分钟）</label>
              <input v-model.number="newTask.goal_value" type="number" placeholder="如 30" />
            </div>
          </div>
          <div class="grid2">
            <div class="f-cell">
              <label>完成奖励（⭐）</label>
              <input v-model.number="newTask.base_score" type="number" placeholder="如 20" />
            </div>
            <div class="f-cell">
              <label>确认方式</label>
              <select v-model="newTask.confirm_mode">
                <option value="a">自动确认</option>
                <option value="c">需家长确认</option>
                <option value="b">AI 初审 + 家长终审</option>
              </select>
            </div>
          </div>
          <div v-if="newTask.confirm_mode === 'b'" class="f-row">
            <label>AI 分析方式</label>
            <select v-model="newTask.analysis_mode">
              <option value="judge">普通判定（做没做、对不对）</option>
              <option value="score">理解度分析（1-5 星 + 给孩子反馈）</option>
            </select>
          </div>
          <div v-if="newTask.confirm_mode === 'b'" class="f-row">
            <label>AI 验收要求</label>
            <textarea v-model="newTask.review_prompt" rows="3" maxlength="200"
              placeholder="AI 怎么验收这个任务？如：让孩子复述故事，检查是否讲出主要人物和两件以上情节，讲得通顺就算通过"></textarea>
          </div>
          <div class="icon-row">
            <small>任务图标</small>
            <button v-for="ic in ICONS" :key="ic" type="button" class="ic-btn" :class="{ on: newTask.icon === ic }" @click="newTask.icon = ic">{{ ic }}</button>
          </div>
          <div class="ev-block">
            <b>提交步骤（孩子按引导逐项上传）</b>
            <small>每步会显示给孩子的提示语；勾"必须"的缺了不能提交</small>
            <div v-for="(st, i) in evSteps" :key="i" class="step-card">
              <div class="step-top">
                <span class="step-no">{{ i + 1 }}</span>
                <select v-model="st.kind" class="ctl">
                  <option value="photo">📷 拍照</option>
                  <option value="audio">🎙 录音</option>
                  <option value="video">🎬 录像</option>
                </select>
                <label class="step-req"><input type="checkbox" v-model="st.required" />{{ st.required ? '必须' : '可选' }}</label>
                <a class="step-del" @click="evSteps.splice(i, 1)">删除</a>
              </div>
              <input v-model="st.label" class="ctl step-label" maxlength="20" placeholder="给孩子的提示语，如：拍下你今天读的内容" />
            </div>
            <button type="button" class="step-add" @click="evSteps.push({ kind: 'photo', label: '', required: true })">＋ 添加一步</button>
            <small class="hint-txt">不添加步骤 = 无需上传材料；提示语留空会使用默认提示，不会删除步骤。</small>
            <small v-if="editingId" class="hint-txt">修改材料要求会用于今天的待提交任务；已计时达标的不必重计时，新时长要求用于后续任务。</small>
          </div>
          <button class="primary" @click="createTask">{{ editingId ? '保存修改' : '创建' }}</button>
        </div>
        <div v-for="t in tasks" :key="t.id" class="task-line">
          {{ t.icon }} {{ t.name }} ·
          <em v-if="t.schedule?.date_mode === 'weekly_quota'">每周{{ t.schedule.weekly_quota }}次</em>
          <em v-else>周{{ t.schedule?.weekdays }}</em>
          · {{ t.base_score }}⭐
          <em v-if="t.evidence_type && t.evidence_type !== 'none'" class="ev-tag">{{ evText(t.evidence_type) }}</em>
          <a class="edit" @click="editTask(t)">✏️ 编辑</a>
        </div>
      </van-tab>

      <van-tab title="🌠 愿望审批" name="wish">
        <div v-if="!wishes.length" class="empty">还没有愿望</div>
        <div v-for="w in wishes" :key="w.id" class="card">
          <div class="row1"><b>✨ {{ w.kid_name ? w.kid_name + ' · ' : '' }}{{ w.title }}</b><em>{{ statusText[w.status] }}</em></div>
          <div class="row2" v-if="w.status === 'pending'">孩子期望：{{ w.hoped_price }}⭐ · 预计 {{ w.suggest_days || '?' }} 天</div>
          <div class="row2" v-else-if="w.status === 'saving'">定价 {{ w.price }}⭐</div>
          <div class="btns" v-if="['pending', 'saving', 'redeem_requested'].includes(w.status)">
            <button v-if="w.status === 'pending'" class="ok" @click="wishAction(w, 'approve')">定价通过</button>
            <button v-if="w.status === 'saving'" class="no" @click="wishAction(w, 'reject')">取消</button>
            <button v-if="w.status === 'redeem_requested'" class="ok" @click="wishAction(w, 'fulfill')">✓ 已兑现</button>
          </div>
        </div>
      </van-tab>

      <van-tab title="📊 流水" name="ledger">
        <div v-for="i in ledger" :key="i.id" class="task-line">
          {{ i.date }} {{ i.kid_name }} · {{ i.source }} · 基础{{ i.base }} × {{ i.date_multiplier }} × {{ i.streak_multiplier.toFixed ? i.streak_multiplier.toFixed(1) : i.streak_multiplier }} = <b>+{{ i.final }}⭐</b>
        </div>
      </van-tab>

    </van-tabs>

    <!-- 愿望定价输入（放 tabs 外：van-tabs 内非 van-tab 元素不渲染） -->
<div v-if="priceModal" class="pm-mask" @click.self="priceModal = null">
    <div class="pm-box">
    <b>🌠 给「{{ priceModal.wish.title }}」定价{{ priceModal.wish.kid_name ? '（' + priceModal.wish.kid_name + '）' : '' }}</b>
    <p class="hint">孩子期望 {{ priceModal.wish.hoped_price || '?' }} 星 · 近30天日均 {{ Math.round(wishHealth) }} 星 · 约 {{ priceModal.wish.suggest_days || '?' }} 天达成</p>
    <input v-model="priceModal.price" type="number" placeholder="定价多少颗星星？" />
    <div class="pm-btns">
      <button class="ghost" @click="priceModal = null">取消</button>
      <button class="ok" @click="confirmPrice">确认定价</button>
    </div>
    </div>
    </div>
  </div>
</template>

<style scoped>
.admin { max-width: 560px; margin: 0 auto; min-height: 100vh; background: #F7F5F0; }
.history-tools { display: flex; align-items: center; gap: 8px; padding: 12px; color: #6F6E68; font-size: 12px; line-height: 1.5; }
.history-tools small { flex: 1; }
.history-tools button { flex-shrink: 0; font-size: 12px; padding: 6px 10px; background: #fff; color: #534AB7; border: 1px solid #D3D1C7; box-shadow: none; }
.history-more { display: block; margin: 14px auto; color: #534AB7; background: #fff; }
.history-count { text-align: center; color: #6F6E68; font-size: 12px; padding-bottom: 12px; }
.admin :deep(.van-tabs__nav) { overflow-x: auto; }
.admin :deep(.van-tab) { flex: 0 0 auto !important; min-width: 86px; padding: 0 10px; }

/* ===== 统一控件 token（修复对齐：所有输入/下拉同高同边框） ===== */
.ctl {
  width: 100%;
  height: 42px;
  font-size: 14px;
  border: 1.5px solid #D3D1C7;
  border-radius: 10px;
  padding: 0 12px;
  outline: none;
  background: #fff;
  box-sizing: border-box;
  appearance: none;
  -webkit-appearance: none;
}
.ctl:focus { border-color: #534AB7; box-shadow: 0 0 0 3px rgba(83, 74, 183, 0.12); }
select.ctl { background: #fff url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23888780' stroke-width='1.5' fill='none' stroke-linecap='round'/%3E%3C/svg%3E") no-repeat right 12px center; padding-right: 32px; }
textarea.ctl { height: auto; padding: 10px 12px; line-height: 1.5; resize: vertical; }
/* 数字输入去掉原生 spinner（截图里贴边的小控件） */
input.ctl[type=number]::-webkit-inner-spin-button, input.ctl[type=number]::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
input.ctl[type=number] { -moz-appearance: textfield; }

/* 标签统一：上 12 下 6 */
.f-row label, .f-cell label, .lbl {
  display: block;
  font-size: 13px;
  font-weight: 700;
  color: #5F5E5A;
  margin: 12px 0 6px;
}
.f-row label:first-child, .f-cell label:first-child { margin-top: 4px; }

/* 两列 grid：start 对齐（标签+控件同高，修复基线错位） */
.grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; align-items: start; }
.f-row, .f-cell { display: flex; flex-direction: column; }

/* 说明文字统一（加深对比度） */
.hint-txt { display: block; font-size: 12px; color: #6F6E68; margin-top: 8px; line-height: 1.5; }
.empty { text-align: center; color: #888780; padding: 40px 0; font-size: 13px; }
.van-tabs :deep(.van-tab) { flex: 1; }
.ev-block { background: #F7F5F0; border: 1.5px solid #E5E2D9; border-radius: 12px; padding: 12px; margin-top: 12px; }
.ev-block b { display: block; font-size: 14px; margin-bottom: 2px; }
.card-title { display: flex; align-items: center; justify-content: space-between; padding-bottom: 10px; margin-bottom: 4px; border-bottom: 1.5px solid #EDEAE0; }
.card-title b { font-size: 15px; }
.reject-block { margin: 14px 10px; padding: 12px; background: #FFF8F5; border: 1.5px solid #E8C4B8; border-radius: 12px; }
.reject-block b { font-size: 14px; }
.reject-block small { display: block; color: #6F6E68; font-size: 12px; margin: 2px 0 8px; }
.reject-line { margin: 6px 0; border-color: #E8C4B8; background: #fff; }
.reject-tag { color: #A32D2D; font-weight: 700; font-size: 12px; }
.confirm-card { cursor: pointer; }
.row1 .arrow { margin-left: auto; color: #888780; font-size: 12px; }
.detail { animation: expand .18s ease; }
@keyframes expand { from { opacity: 0; transform: translateY(-4px); } }
.ai-report { margin: 8px 0; padding: 10px 12px; border-radius: 10px; }
.ai-report.ok { background: #E8F5E9; border: 1.5px solid #A5D6A7; }
.ai-report.warn { background: #FFF3E0; border: 1.5px solid #FFCC80; }
.ai-head { font-size: 14px; font-weight: 800; color: #2E7D32; display: flex; align-items: center; gap: 6px; }
.ai-report.warn .ai-head { color: #EF6C00; }
.ai-stars { font-size: 12px; letter-spacing: 2px; }
.ai-reason { font-size: 13px; color: #444; margin-top: 4px; }
.ai-kid { font-size: 12px; color: #534AB7; margin-top: 4px; font-style: italic; }
.step-card { background: #F7F5F0; border: 1.5px solid #E5E2D9; border-radius: 12px; padding: 10px 12px; margin: 8px 0; display: flex; flex-direction: column; gap: 8px; }
.step-top { display: flex; gap: 10px; align-items: center; }
.step-top .ctl { width: 130px; min-width: 72px; flex: 0 1 130px; height: 36px; }
.step-top > :not(.ctl) { flex-shrink: 0; }
.step-del { white-space: nowrap; }
@media (max-width: 360px) { .step-top { gap: 6px; } .step-top .ctl { padding-left: 8px; padding-right: 22px; } .step-del { padding-left: 3px; padding-right: 3px; } }
.step-no { flex: 0 0 22px; height: 22px; display: flex; align-items: center; justify-content: center; background: #534AB7; color: #fff; border-radius: 50%; font-size: 12px; font-weight: 800; }
.step-req { font-size: 12.5px; color: #5F5E5A; font-weight: 600; white-space: nowrap; display: flex; align-items: center; gap: 5px; margin-left: auto; }
.step-req input { width: 16px; height: 16px; accent-color: #534AB7; }
.step-del { color: #A32D2D; cursor: pointer; font-size: 12.5px; font-weight: 600; padding: 6px 8px; border-radius: 6px; }
.step-del:active { background: #FCEBEB; }
.step-add { font-size: 13px; font-weight: 700; margin-top: 6px; background: #fff; border: 1.5px dashed #B7B4A8; border-radius: 10px; padding: 10px; width: 100%; color: #534AB7; cursor: pointer; box-shadow: none; }
.ai-badge.ok { background: #E8F5E9; color: #2E7D32; border: 1.5px solid #A5D6A7; }
.ai-badge.warn { background: #FFF3E0; color: #EF6C00; border: 1.5px solid #FFCC80; }
.no-ev { font-size: 12px; color: #888780; padding: 8px 0; }
.card { background: #fff; margin: 10px; border-radius: 10px; padding: 14px; border: 1px solid #E5E2D9; }
.card b { font-size: 15px; }
.card input, .card select, .card textarea { width: 100%; height: 42px; font-size: 14px; border: 1.5px solid #D3D1C7; border-radius: 10px; padding: 0 12px; outline: none; background: #fff; box-sizing: border-box; appearance: none; -webkit-appearance: none; }
.card input:focus, .card select:focus, .card textarea:focus { border-color: #534AB7; box-shadow: 0 0 0 3px rgba(83, 74, 183, 0.12); }
.card input[type=number]::-webkit-inner-spin-button, .card input[type=number]::-webkit-outer-spin-button { -webkit-appearance: none; }
/* 复选框豁免：恢复原生样式（统一控件 appearance:none 误伤 checkbox） */
.card input[type=checkbox], .ev-block input[type=checkbox] { appearance: auto; -webkit-appearance: checkbox; width: 16px; height: 16px; margin: 0; accent-color: #534AB7; }
.card textarea { height: auto; padding: 10px 12px; resize: vertical; }
.row1 { display: flex; justify-content: space-between; align-items: center; }
.row1 em { font-style: normal; font-size: 12px; color: #534AB7; font-weight: 700; }
.row2 { font-size: 12px; color: #888780; margin-top: 6px; }
.btns { display: flex; gap: 8px; margin-top: 10px; }
button { font-size: 13px; font-weight: 600; cursor: pointer; border: 2px solid #2C2C2A; border-radius: 8px; padding: 8px 14px; box-shadow: 2px 2px 0 rgba(44,44,42,.6); }
button:active { transform: translate(2px, 2px); box-shadow: none; }
.ok { background: #E1F5EE; color: #0F6E56; }
.no { background: #FCEBEB; color: #A32D2D; }
.primary { margin-top: 16px; width: 100%; background: #534AB7; color: #fff; padding: 12px; font-size: 15px; border-radius: 12px; }
.task-line { background: #fff; margin: 6px 10px; border-radius: 8px; padding: 10px 14px; font-size: 13px; border: 1px solid #E5E2D9; }
.icon-row { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-top: 10px; }
.icon-row small { color: #888780; }
.ic-btn { width: 36px; height: 36px; font-size: 18px; padding: 0; background: #fff; border: 1.5px solid #D3D1C7; border-radius: 8px; cursor: pointer; }
.ic-btn.on { border-color: #534AB7; background: #EEEDFE; }

.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.ev-block { margin-top: 10px; padding: 10px; background: #FAF8F3; border-radius: 8px; }
.ev-block b { font-size: 13px; }
.ev-block small { display: block; color: #888780; font-size: 11px; margin: 4px 0 8px; }
.ev-opt { display: flex; align-items: center; gap: 6px; padding: 6px 0; font-size: 13px; cursor: pointer; }
.ev-opt input { width: 16px; height: 16px; }
.ev-opt em { font-style: normal; color: #888780; font-size: 11px; margin-left: auto; }
.ev-tag { font-style: normal; margin-left: 4px; }

.task-line .edit { float: right; color: #534AB7; font-weight: 700; cursor: pointer; font-size: 12px; }
.cancel-edit { float: right; color: #A32D2D; font-size: 12px; font-weight: 700; cursor: pointer; }
.pm-mask { position: fixed; inset: 0; z-index: 99; background: rgba(0,0,0,.35); display: flex; align-items: center; justify-content: center; }
.pm-box { width: min(340px, 90vw); background: #fff; border-radius: 14px; padding: 20px; }
.pm-box b { font-size: 15px; }
.pm-box .hint { font-size: 12px; color: #888780; margin: 8px 0 12px; }
.pm-box input { width: 100%; font-size: 18px; text-align: center; border: 1.5px solid #D3D1C7; border-radius: 8px; padding: 10px; outline: none; margin-bottom: 14px; }
.pm-btns { display: flex; gap: 10px; }
.pm-btns button { flex: 1; }
.task-line em { font-style: normal; color: #534AB7; }
.confirm-card { background: #fff; margin: 10px; border-radius: 10px; padding: 14px; border: 1px solid #E5E2D9; }
.evidence img { max-width: 100%; border-radius: 8px; margin-top: 8px; display: block; }
.evidence audio { width: 100%; margin-top: 8px; display: block; }
</style>
