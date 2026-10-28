import { createApp } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import Vant from 'vant'
import 'vant/lib/index.css'
import '@vant/touch-emulator'

import App from './App.vue'
import KidHome from './views/KidHome.vue'
import AdminLogin from './views/admin/AdminLogin.vue'
import AdminHome from './views/admin/AdminHome.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: KidHome },
    { path: '/admin', component: AdminLogin },
    { path: '/admin/home', component: AdminHome },
  ],
})

const app = createApp(App)
app.use(router).use(Vant).mount('#app')
