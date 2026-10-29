<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from 'vant'

const router = useRouter()
const mode = ref('login') // login | register
const code = ref('')
const pin = ref('')
const regName = ref('')
const regPin = ref('')
const newCode = ref('') // 注册成功展示

async function login() {
  const r = await fetch('/api/admin/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code: code.value.trim(), pin: pin.value }),
  })
  if (r.ok) {
    const data = await r.json()
    localStorage.setItem('admin_token', data.token)
    router.push('/admin/home')
  } else {
    showToast('家庭码或 PIN 不对')
  }
}

async function register() {
  if (!regName.value.trim()) return showToast('给家庭起个名字～')
  if (!regPin.value || regPin.value.length < 4) return showToast('PIN 至少 4 位')
  const r = await fetch('/api/admin/family', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: regName.value.trim(), pin: regPin.value }),
  })
  if (!r.ok) return showToast('注册失败，稍后再试')
  const data = await r.json()
  newCode.value = data.code
  localStorage.setItem('admin_token', data.token)
  localStorage.setItem('familyCode', data.code)
}

function toHome() { router.push('/admin/home') }
</script>

<template>
  <div class="login-page">
    <!-- 注册成功：展示家庭码 -->
    <div v-if="newCode" class="login-box">
      <h2>🎉 家庭创建成功</h2>
      <p>把家庭码记好，孩子和家里其他家长都用它进入</p>
      <div class="big-code">{{ newCode }}</div>
      <button @click="toHome">进入管理后台</button>
    </div>

    <div v-else class="login-box">
      <h2>🐱 喵星成长屋</h2>
      <p>{{ mode === 'login' ? '家长登录' : '注册新家庭' }}</p>

      <template v-if="mode === 'login'">
        <input v-model="code" maxlength="6" placeholder="家庭码（6 位数字）" inputmode="numeric" />
        <input v-model="pin" type="password" maxlength="8" placeholder="管理 PIN" @keyup.enter="login" />
        <button @click="login">进入</button>
        <a class="switch-mode" @click="mode = 'register'">第一次使用？注册新家庭 →</a>
      </template>

      <template v-else>
        <input v-model="regName" maxlength="16" placeholder="家庭名字（如：王家）" />
        <input v-model="regPin" type="password" maxlength="8" placeholder="管理 PIN（至少 4 位）" @keyup.enter="register" />
        <button @click="register">创建家庭</button>
        <a class="switch-mode" @click="mode = 'login'">已有家庭码？返回登录 →</a>
      </template>
    </div>
  </div>
</template>

<style scoped>
.login-page { position: fixed; inset: 0; display: flex; align-items: center; justify-content: center; background: #F1EFE8; }
.login-box { background: #fff; border: 2px solid #2C2C2A; border-radius: 14px; box-shadow: 6px 6px 0 rgba(44,44,42,.2); padding: 32px 28px; width: 300px; text-align: center; }
h2 { margin-bottom: 4px; }
p { color: #888780; font-size: 13px; margin-bottom: 20px; }
.big-code { font-size: 44px; font-weight: 800; letter-spacing: 10px; text-align: center; background: #EEEDFE; border: 2px solid #2C2C2A; border-radius: 12px; padding: 16px; margin: 16px 0; color: #534AB7; }
.switch-mode { display: block; text-align: center; margin-top: 14px; color: #534AB7; font-size: 13px; font-weight: 600; cursor: pointer; }
input { width: 100%; font-size: 20px; text-align: center; letter-spacing: 8px; border: 2px solid #2C2C2A; border-radius: 8px; padding: 10px; outline: none; margin-bottom: 16px; }
button { width: 100%; font-size: 16px; font-weight: 700; color: #fff; background: #534AB7; border: 2px solid #2C2C2A; border-radius: 8px; padding: 10px; cursor: pointer; }
</style>
