<script setup>
import { computed } from 'vue'
import { getSceneLighting } from '../scene-art.js'

const props = defineProps({
  lighting: { type: Object, default: () => getSceneLighting() },
})

const night = computed(() => Math.max(0, Math.min(1, props.lighting?.night ?? 0)))
const dusk = computed(() => Math.max(0, Math.min(1, props.lighting?.dusk ?? 0)))

const stars = [
  [108, 12, 2], [166, 30, 1], [208, 8, 1], [252, 22, 2],
  [298, 40, 1], [328, 12, 1], [356, 26, 2], [390, 8, 1],
  [422, 46, 1], [460, 18, 2], [510, 34, 1], [554, 10, 1],
  [580, 44, 2], [136, 56, 1], [190, 68, 1], [236, 50, 1],
  [286, 64, 2], [378, 60, 1], [492, 58, 1], [538, 72, 1],
]

const leaves = [
  { left: '10%', top: '24%', delay: '-2s' },
  { left: '16%', top: '58%', delay: '-5s' },
  { left: '84%', top: '32%', delay: '-1s' },
  { left: '89%', top: '64%', delay: '-4s' },
]
</script>

<template>
  <div class="garden-backdrop" aria-hidden="true">
    <div class="garden-art">
      <svg
        v-for="(leaf, index) in leaves"
        :key="index"
        class="garden-leaf"
        viewBox="0 0 12 10"
        :style="{ left: leaf.left, top: leaf.top, animationDelay: leaf.delay }"
      >
        <path d="M2 2H8V4H10V6H8V8H4V6H2Z" fill="#94a65d" />
        <path d="M4 4H8V6H4ZM2 6H4V8H2Z" fill="#536f45" />
      </svg>
    </div>
    <div class="garden-dusk" :style="{ opacity: dusk * 0.22 }" />
    <div class="garden-night" :style="{ opacity: night * 0.62 }" />
    <svg
      class="garden-sky"
      viewBox="0 0 680 100"
      preserveAspectRatio="xMidYMin meet"
      :style="{ opacity: night * 0.65 }"
    >
      <rect
        v-for="([x, y, size], index) in stars"
        :key="index"
        :x="x"
        :y="y"
        :width="size"
        :height="size"
        fill="#e1dfb4"
      />
      <path d="M600 12H616V16H612V28H616V32H604V28H600V24H596V16H600Z" fill="#dedbb0" />
      <rect x="600" y="16" width="4" height="8" fill="#f4e7bf" />
    </svg>
    <div class="garden-lights" :style="{ opacity: night }">
      <svg
        v-for="side in ['left', 'right']"
        :key="side"
        class="garden-lantern"
        :class="`garden-lantern-${side}`"
        viewBox="0 0 24 36"
      >
        <rect x="2" y="8" width="20" height="20" fill="#edbd68" opacity="0.12" />
        <rect x="4" y="10" width="16" height="16" fill="#f5c675" opacity="0.2" />
        <rect x="10" y="0" width="4" height="4" fill="#82603e" />
        <rect x="8" y="4" width="8" height="2" fill="#b88950" />
        <rect x="6" y="6" width="12" height="4" fill="#644c36" />
        <rect x="4" y="10" width="16" height="2" fill="#916a41" />
        <rect x="6" y="12" width="12" height="14" fill="#735337" />
        <rect x="8" y="12" width="8" height="12" fill="#ffc779" />
        <rect x="10" y="14" width="4" height="8" fill="#ffeab0" />
        <rect x="11" y="12" width="2" height="12" fill="#b18344" />
        <rect x="6" y="26" width="12" height="4" fill="#b2864d" />
        <rect x="10" y="30" width="4" height="6" fill="#79543b" />
      </svg>
    </div>
  </div>
</template>

<style scoped>
.garden-backdrop {
  position: absolute;
  inset: 0;
  z-index: 0;
  overflow: hidden;
  pointer-events: none;
  image-rendering: pixelated;
  shape-rendering: crispEdges;
}

.garden-art,
.garden-dusk,
.garden-night,
.garden-lights {
  position: absolute;
  inset: 0;
  pointer-events: none;
}

.garden-art {
  background: url('/art/courtyard.webp') center bottom / 100% 100% no-repeat;
  image-rendering: pixelated;
}

.garden-dusk {
  z-index: 1;
  background: rgb(199 114 76);
}

.garden-night {
  z-index: 2;
  background: rgb(18 27 48);
}

.garden-sky {
  position: absolute;
  z-index: 3;
  top: 0;
  left: 0;
  width: 100%;
  height: 18%;
  pointer-events: none;
}

.garden-lights {
  z-index: 3;
}

.garden-lantern {
  position: absolute;
  bottom: 9%;
  width: 48px;
  height: 72px;
  filter: brightness(1.08);
}

.garden-lantern-left {
  left: 18%;
  transform: translateX(-50%);
}

.garden-lantern-right {
  right: 18%;
  transform: translateX(50%);
}

.garden-leaf {
  position: absolute;
  width: 24px;
  height: 20px;
  opacity: 0.45;
  transform-origin: 4px 16px;
  animation: garden-leaf-sway 8s steps(3, end) infinite;
}

@keyframes garden-leaf-sway {
  0%, 100% { transform: translate(0, 0) rotate(-3deg); }
  50% { transform: translate(2px, -2px) rotate(3deg); }
}

@media (max-width: 640px) {
  .garden-art {
    background-size: auto 100%;
    background-position: center bottom;
  }
}

@media (prefers-reduced-motion: reduce) {
  .garden-leaf {
    animation: none;
    transform: none;
  }
}
</style>
