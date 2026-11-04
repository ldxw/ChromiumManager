<template>
  <div class="right">
    <div class="header">
      <div class="title">
        <el-icon v-if="isTablet" class="menu-btn" @click="openGroupDrawer">
          <Expand />
        </el-icon>
        <span>配置列表</span>
      </div>
      <div class="form">
        <el-input
          v-model="model.condition.keyword"
          placeholder="名称 / IP / 语言 / 时区"
          clearable
          class="input-with-select"
          @keydown.enter="onSearchClick"
        >
          <template #prepend>
            <el-input
              :model-value="filterProxyName"
              placeholder="全部代理"
              class="input-picker"
              @click="onManageClick"
              @keydown.prevent
            >
              <template #suffix>
                <span
                  class="input-picker-suffix"
                  @click.stop="model.condition.proxyId ? onProxyClear() : onManageClick()"
                >
                  <el-icon v-if="model.condition.proxyId" class="el-input__clear">
                    <CircleClose />
                  </el-icon>
                  <el-icon v-else><ArrowDown /></el-icon>
                </span>
              </template>
            </el-input>
          </template>
        </el-input>
        <el-button
          type="primary"
          size="default"
          :icon="Search"
          class="search"
          @click="onSearchClick"
        ></el-button>
        <el-button
          type="primary"
          size="default"
          :icon="Refresh"
          class="refresh"
          @click="onRefreshClick"
        ></el-button>
        <el-button
          type="primary"
          size="default"
          :icon="Plus"
          class="add"
          @click="onAddClick"
        ></el-button>
      </div>
    </div>
    <Content ref="contentRef" />
    <ProxyManagement v-model="proxyDialog" @change="fetchProxies" @select="onProxySelect" />
  </div>
</template>

<script setup>
import { inject, provide, reactive, ref, computed, watch, onMounted } from 'vue'
import { Search, Refresh, Plus, ArrowDown, CircleClose, Expand } from '@element-plus/icons-vue'
import { getProxies } from '@/api'
import { useBreakpoint } from '@/utils/useBreakpoint'

import Content from './RightContent.vue'
import ProxyManagement from './ProxyManagement.vue'

const activeGroupId = inject('activeGroupId')
const openGroupDrawer = inject('openGroupDrawer', () => {})
const { isTablet } = useBreakpoint()

let contentRef = ref(null)
let proxyDialog = ref(false)
let proxies = ref([])
let model = reactive({
  condition: { proxyId: '', keyword: '' }
})

const fetchProxies = async () => {
  try {
    const res = await getProxies({ all: 1 })
    if (res) proxies.value = res
  } catch (e) {
    console.error(e)
  }
}

provide('proxies', proxies)
provide('fetchProxies', fetchProxies)

onMounted(() => {
  fetchProxies()
})

watch(activeGroupId, () => {
  model.condition = { proxyId: '', keyword: '' }
})

const filterProxyName = computed(() => {
  const p = proxies.value.find((item) => item._id === model.condition.proxyId)
  return p ? p.name : ''
})

const onManageClick = () => {
  proxyDialog.value = true
}

const onProxySelect = async (proxyId) => {
  await fetchProxies()
  model.condition.proxyId = proxyId
  onSearchClick()
}

const onProxyClear = () => {
  model.condition.proxyId = ''
  onSearchClick()
}

const onSearchClick = () => {
  contentRef.value.onSearchClick(model.condition.proxyId, model.condition.keyword)
}
const onRefreshClick = () => {
  contentRef.value.onRefreshClick()
}
const onAddClick = () => {
  contentRef.value.onAddClick()
}
</script>

<style lang="scss">
.right .header .form {
  .el-input-group__prepend .el-input {
    width: 120px;

    .el-input__wrapper {
      box-shadow: none;
      background-color: transparent;

      .el-input__inner {
        height: 36px;
        line-height: 36px;
        text-indent: 10px;
      }
    }
  }

  .input-with-select .el-input-group__prepend {
    background-color: $white-color;
    padding: 0;
  }

  .input-with-select .el-input__wrapper {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }

  .el-button {
    height: 38px;
    line-height: 38px;

    .el-icon {
      font-size: 20px;
    }
  }
}

@media (max-width: 767px) {
  .right .header .form {
    .el-input-group__prepend .el-input {
      width: 90px;
    }

    .el-button {
      padding: 8px 10px;
    }
  }
}
</style>

<style lang="scss" scoped>
.right {
  display: flex;
  flex-direction: column;
  margin-left: 280px;
  height: 100%;
  min-width: 0;

  .header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 20px;
    min-height: 60px;
    flex-wrap: wrap;
    border-bottom: $border1;

    .title {
      display: flex;
      align-items: center;
      gap: 8px;
      font-size: 24px;
      color: $text-color1;
      white-space: nowrap;

      .menu-btn {
        font-size: 22px;
        cursor: pointer;
        color: $text-color2;
      }
    }

    .form {
      display: flex;
      align-items: center;
      gap: 8px;
      margin-left: auto;
      padding: 10px 0;
      flex: 0 1 50%;
      min-width: 320px;

      .search {
        border-top-left-radius: 0;
        border-bottom-left-radius: 0;
        margin-left: 0;
      }

      .refresh,
      .add {
        margin-left: 0;
      }

      .el-input {
        min-width: 0;
        flex: 1;
      }
    }
  }
}

@media (max-width: 1279px) {
  .right .header .form {
    width: 100%;
    flex: 1 1 100%;
    min-width: 0;
    padding: 0 0 10px;
  }
}

@media (max-width: 1023px) {
  .right {
    margin-left: 0;

    .header {
      padding: 0 12px;

      .title {
        font-size: 20px;
      }
    }
  }
}

@media (max-width: 767px) {
  .right .header {
    min-height: 52px;
    gap: 8px;

    .title {
      font-size: 18px;
    }

    .form {
      flex-wrap: wrap;
      row-gap: 8px;

      // 搜索框独占一行，按钮另起一行右对齐，避免输入区被挤成一条缝
      :deep(.input-with-select) {
        flex: 1 1 100%;
      }

      .search {
        margin-left: auto;
        border-radius: 4px;
      }

      .refresh,
      .add {
        margin-left: 0;
      }
    }
  }
}
</style>
