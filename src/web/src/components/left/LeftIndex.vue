<template>
  <div v-if="isTablet && drawerVisible" class="left-mask" @click="closeDrawer"></div>
  <div class="left" :class="{ 'is-drawer': isTablet, 'is-open': drawerVisible }">
    <div class="header">
      <div class="title"><span>分组列表</span></div>
      <div class="button">
        <span>
          <el-icon @click="onAddClick"><Plus /></el-icon>
        </span>
      </div>
    </div>
    <Content ref="contentRef" @select="onItemSelect" />
  </div>
</template>

<script setup>
import { inject, ref, watch } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import Content from './LeftContent.vue'
import { useBreakpoint } from '@/utils/useBreakpoint'

const { isTablet } = useBreakpoint()

let contentRef = ref(null)
const drawerVisible = inject('groupDrawerVisible', ref(false))
const closeDrawer = inject('closeGroupDrawer', () => {
  drawerVisible.value = false
})

watch(isTablet, (val) => {
  if (!val) closeDrawer()
})

const onAddClick = () => {
  contentRef.value.onAddClick()
}

const onItemSelect = () => {
  if (isTablet.value) closeDrawer()
}
</script>

<style lang="scss" scoped>
.left-mask {
  position: fixed;
  inset: 0;
  z-index: 1900;
  background-color: rgba(0, 0, 0, 0.45);
}

.left {
  float: left;
  width: 280px;
  height: 100%;
  border-right: $border1;

  &.is-drawer {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    z-index: 2000;
    width: 280px;
    max-width: 78vw;
    background-color: $white-color;
    border-right: none;
    box-shadow: 2px 0 12px rgba(0, 0, 0, 0.15);
    transform: translateX(-100%);
    transition: transform 0.25s ease;
    overflow-y: auto;
  }

  &.is-drawer.is-open {
    transform: translateX(0);
  }

  .header {
    height: 60px;
    line-height: 60px;
    padding: 0 20px;
    background-color: $background-color2;
    color: $white-color;
    font-size: 24px;

    .title {
      float: left;
    }

    .button {
      float: right;

      .el-icon {
        cursor: pointer;
        font-size: 24px;
      }
    }
  }
}

@media (max-width: 767px) {
  .left .header {
    font-size: 20px;
  }
}
</style>
