<script setup>
import { computed } from 'vue'
import { decorArt } from '../scene-art.js'

const props = defineProps({
  wallpaperKey: { type: String, default: '' },
})

const wallColor = computed(() => decorArt(props.wallpaperKey).wallColor || '#fffaf0')
</script>

<template>
  <div class="pixel-room" aria-hidden="true">
    <div class="pixel-wall" :style="{ backgroundColor: wallColor }" />
    <div class="pixel-floor" />
    <div class="pixel-skirting" />
    <img
      class="pixel-window"
      src="/art/cottage-window.png"
      alt=""
      width="162"
      height="152"
      draggable="false"
    >
  </div>
</template>

<style scoped>
.pixel-room {
  position: absolute;
  inset: 0;
  z-index: 0;
  pointer-events: none;
  shape-rendering: crispEdges;
  image-rendering: pixelated;
}

.pixel-wall {
  position: absolute;
  top: 0;
  left: 0;
  width: 1280px;
  height: 404px;
  background-image: url('/art/plaster.png');
  background-repeat: repeat;
  background-size: 512px 512px;
  background-blend-mode: multiply;
  image-rendering: pixelated;
}

.pixel-floor {
  position: absolute;
  top: 404px;
  left: 0;
  width: 1280px;
  height: 316px;
  background-image: url('/art/wood-floor.png');
  background-repeat: repeat;
  background-size: 1280px 320px;
  image-rendering: pixelated;
}

.pixel-skirting {
  position: absolute;
  z-index: 1;
  top: 392px;
  left: 0;
  width: 1280px;
  height: 20px;
  border-radius: 0;
  background: linear-gradient(to bottom,
    #c69969 0 4px,
    #a77950 4px 9px,
    #8b6345 9px 11px,
    #ae8056 11px 16px,
    #79583e 16px 20px
  );
  box-shadow: 0 4px 0 rgb(71 48 32 / 20%);
}

.pixel-skirting::after {
  position: absolute;
  top: 9px;
  left: 0;
  width: 100%;
  height: 2px;
  content: '';
  background: repeating-linear-gradient(to right,
    transparent 0 16px,
    rgb(229 185 127 / 45%) 16px 20px,
    transparent 20px 44px,
    rgb(76 49 33 / 35%) 44px 48px,
    transparent 48px 64px
  );
}

.pixel-window {
  position: absolute;
  z-index: 1;
  top: 228px;
  left: 623px;
  display: block;
  width: 162px;
  height: 152px;
  image-rendering: pixelated;
}
</style>
