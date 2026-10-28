<script setup>
// 家长后台（Vant 移动组件库）：确认队列 / 任务配置 / 愿望审批 / 流水
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { showToast, showConfirmDialog } from 'vant'

const router = useRouter()
const tab = ref('confirm')
const confirms = ref([])
const tasks = ref([])
const wishes = ref([])
const wishHealth = ref(0)
const ledger = ref([])
const newTask = ref({ name: '', icon: '⭐', category: 'custom', goal_type: 'duration', goal_value: 30, confirm_mode: 'c', base_score: 20, date_mode: 'weekday_fixed', weekdays: '1,3,5', weekly_quota: 3 })
const editingId = ref(0) // >0 = 编辑模式

function editTask(t) {
  editingId.value = t.id
  newTask.value = {
    name: t.name, icon: t.icon || '⭐', category: t.category || 'custom',
    goal_type: t.goal_type || 'duration', goal_value: t.goal_value || 0,
    confirm_mode: t.confirm_mode || 'c', base_score: t.base_score || 20,
    date_mode: t.schedule?.date_mode || 'weekday_fixed',
    weekdays: t.schedule?.weekdays || '1,3,5',
    weekly_quota: t.schedule?.weekly_quota || 3,
  }
}
function resetTaskForm() {
  editingId.value = 0
  newTask.value = { name: '', icon: '⭐', category: 'custom', goal_type: 'duration', goal_value: 30, confirm_mode: 'c', base_score: 20, date_mode: 'weekday_fixed', weekdays: '1,3,5', weekly_quota: 3 }
}

const admHeaders = () => ({ 'Content-Type': 'application/json', 'X-Admin-Token': localStorage.getItem('admin_token') || '' })
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
  tasks.value = t.tasks || []
  wishes.value = w.wishes || []
  wishHealth.value = w.daily_stars_avg || 0
  ledger.value = l.items || []
}

async function approve(id) {
  const { ok } = await jpost(`/api/admin/confirm/${id}`, { approve: true })
  if (ok) { showToast('已通过'); await load() }
}
async function reject(id) {
  const r = await showConfirmDialog({ title: '驳回', message: '孩子可以重新完成后再次提交' })
  if (r === 'confirm') {
    await jpost(`/api/admin/confirm/${id}`, { approve: false, reason: '再认真做一遍哦～' })
    showToast('已驳回')
    await load()
  }
}

async function createTask() {
  if (!newTask.value.name) return showToast('任务名必填')
  if (editingId.value) {
    // 编辑模式：PUT 更新
    const { ok } = await jput(`/api/admin/task/${editingId.value}`, newTask.value)
    if (ok) { showToast('已保存'); resetTaskForm(); await load() }
    return
  }
  const { ok } = await jpost('/api/admin/task', newTask.value)
  if (ok) { showToast('已创建'); resetTaskForm(); await load() }
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

const statusText = { pending: '待定价', saving: '攒星星中', redeem_requested: '待兑现', fulfilled: '已实现', rejected: '已取消' }

onMounted(load)
</script>

<template>
  <div class="admin">
    <van-tabs v-model:active="tab" sticky>
      <van-tab title="✅ 待确认" name="confirm">
        <div v-if="!confirms.length" class="empty">没有待确认的任务</div>
        <van-swipe-cell v-for="it in confirms" :key="it.id">
          <div class="confirm-card">
            <div class="row1"><b>{{ it.snapshot?.icon }} {{ it.snapshot?.name }}</b>
              <em>{{ Math.round(it.duration_sec / 60) }} 分钟</em></div>
            <div class="row2">提交于 {{ new Date(it.submitted_at).toLocaleString('zh-CN') }}</div>
            <div class="btns">
              <button class="ok" @click="approve(it.id)">✓ 通过</button>
              <button class="no" @click="reject(it.id)">驳回</button>
            </div>
          </div>
        </van-swipe-cell>
      </van-tab>

      <van-tab title="📋 任务配置" name="tasks">
        <div class="card">
          <b>{{ editingId ? '✏️ 修改任务 #' + editingId : '➕ 新建任务' }}</b>
          <a v-if="editingId" class="cancel-edit" @click="resetTaskForm">取消编辑</a>
          <input v-model="newTask.name" placeholder="任务名（如：琴键小挑战）" />
          <div class="grid">
            <select v-model="newTask.date_mode">
              <option value="weekday_fixed">固定周几</option>
              <option value="weekly_quota">每周次数</option>
            </select>
            <input v-if="newTask.date_mode === 'weekday_fixed'" v-model="newTask.weekdays" placeholder="周几，如 1,3,5" />
            <input v-else v-model.number="newTask.weekly_quota" type="number" placeholder="每周次数" />
          </div>
          <div class="grid">
            <input v-model.number="newTask.goal_value" type="number" placeholder="时长(分钟)" />
            <select v-model="newTask.confirm_mode">
              <option value="a">自动确认</option>
              <option value="c">需家长确认</option>
            </select>
          </div>
          <button class="primary" @click="createTask">{{ editingId ? '保存修改' : '创建' }}</button>
        </div>
        <div v-for="t in tasks" :key="t.id" class="task-line">
          {{ t.icon }} {{ t.name }} ·
          <em v-if="t.schedule?.date_mode === 'weekly_quota'">每周{{ t.schedule.weekly_quota }}次</em>
          <em v-else>周{{ t.schedule?.weekdays }}</em>
          · {{ t.base_score }}⭐
          <a class="edit" @click="editTask(t)">✏️ 编辑</a>
        </div>
      </van-tab>

      <van-tab title="🌠 愿望审批" name="wish">
        <div v-if="!wishes.length" class="empty">还没有愿望</div>
        <div v-for="w in wishes" :key="w.id" class="card">
          <div class="row1"><b>✨ {{ w.title }}</b><em>{{ statusText[w.status] }}</em></div>
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
          {{ i.date }} · {{ i.source }} · 基础{{ i.base }} × {{ i.date_multiplier }} × {{ i.streak_multiplier.toFixed(1) }} = <b>+{{ i.final }}⭐</b>
        </div>
      </van-tab>
      <!-- 愿望定价输入 -->
      <div v-if="priceModal" class="pm-mask" @click.self="priceModal = null">
        <div class="pm-box">
          <b>🌠 给「{{ priceModal.wish.title }}」定价</b>
          <p class="hint">孩子期望 {{ priceModal.wish.hoped_price || '?' }} 星 · 近30天日均 {{ Math.round(wishHealth) }} 星 · 约 {{ priceModal.wish.suggest_days || '?' }} 天达成</p>
          <input v-model="priceModal.price" type="number" placeholder="定价多少颗星星？" />
          <div class="pm-btns">
            <button class="ghost" @click="priceModal = null">取消</button>
            <button class="ok" @click="confirmPrice">确认定价</button>
          </div>
        </div>
      </div>
    </van-tabs>
  </div>
</template>

<style scoped>
.admin { max-width: 560px; margin: 0 auto; min-height: 100vh; background: #F7F5F0; }
.empty { text-align: center; color: #888780; padding: 40px 0; font-size: 13px; }
.card { background: #fff; margin: 10px; border-radius: 10px; padding: 14px; border: 1px solid #E5E2D9; }
.card b { font-size: 15px; }
.card input, .card select { width: 100%; margin-top: 8px; font-size: 14px; border: 1.5px solid #D3D1C7; border-radius: 6px; padding: 8px 10px; outline: none; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
.grid input, .grid select { margin-top: 8px; }
.row1 { display: flex; justify-content: space-between; align-items: center; }
.row1 em { font-style: normal; font-size: 12px; color: #534AB7; font-weight: 700; }
.row2 { font-size: 12px; color: #888780; margin-top: 6px; }
.btns { display: flex; gap: 8px; margin-top: 10px; }
button { font-size: 13px; font-weight: 600; cursor: pointer; border: 2px solid #2C2C2A; border-radius: 8px; padding: 8px 14px; box-shadow: 2px 2px 0 rgba(44,44,42,.6); }
button:active { transform: translate(2px, 2px); box-shadow: none; }
.ok { background: #E1F5EE; color: #0F6E56; }
.no { background: #FCEBEB; color: #A32D2D; }
.primary { margin-top: 10px; width: 100%; background: #534AB7; color: #fff; }
.task-line { background: #fff; margin: 6px 10px; border-radius: 8px; padding: 10px 14px; font-size: 13px; border: 1px solid #E5E2D9; }
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
</style>
