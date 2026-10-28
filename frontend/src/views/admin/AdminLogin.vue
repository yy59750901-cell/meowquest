<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { showToast } from 'vant'

const router = useRouter()
const pin = ref('')

async function login() {
  const r = await fetch('/api/admin/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ pin: pin.value }),
  })
  if (r.ok) {
    const data = await r.json()
    localStorage.setItem('admin_token', data.token) // 手机浏览器 cookie 兼容：localStorage + header
    router.push('/admin/home')
  } else {
    showToast('PIN 不对哦')
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-box">
      <h2>🐱 喵星成长屋</h2>
      <p>家长后台</p>
      <input v-model="pin" type="password" maxlength="8" placeholder="输入管理 PIN" @keyup.enter="login" />
      <button @click="login">进入</button>
    </div>
  </div>
</template>

<style scoped>
.login-page { position: fixed; inset: 0; display: flex; align-items: center; justify-content: center; background: #F1EFE8; }
.login-box { background: #fff; border: 2px solid #2C2C2A; border-radius: 14px; box-shadow: 6px 6px 0 rgba(44,44,42,.2); padding: 32px 28px; width: 300px; text-align: center; }
h2 { margin-bottom: 4px; }
p { color: #888780; font-size: 13px; margin-bottom: 20px; }
input { width: 100%; font-size: 20px; text-align: center; letter-spacing: 8px; border: 2px solid #2C2C2A; border-radius: 8px; padding: 10px; outline: none; margin-bottom: 16px; }
button { width: 100%; font-size: 16px; font-weight: 700; color: #fff; background: #534AB7; border: 2px solid #2C2C2A; border-radius: 8px; padding: 10px; cursor: pointer; }
</style>
