<template>
  <div class="content">
    <el-scrollbar>
      <ul class="list">
        <template v-for="item in model.list" :key="item._id">
          <li
            class="item"
            :class="{ active: item._id === activeGroupId }"
            @click="onItemClick(item)"
          >
            <div class="name" :title="item.name">{{ item.name }}</div>
            <div v-if="item._id !== 'all'" class="action">
              <el-icon @click.stop="onEditClick(item)"><EditPen /></el-icon>
              <el-icon @click.stop="onDeleteClick(item)"><Delete /></el-icon>
            </div>
          </li>
        </template>
      </ul>
    </el-scrollbar>
    <el-dialog
      v-model="formDialog"
      :title="model.form._id ? '编辑分组' : '添加分组'"
      width="650"
      class="responsive-dialog"
      :before-close="onFormCancel"
    >
      <el-form
        ref="formRef"
        :model="model.form"
        :rules="model.rules"
        label-position="right"
        label-width="auto"
        size="default"
      >
        <el-row :gutter="20">
          <el-col :xs="24" :sm="12">
            <el-form-item label="名称" prop="name">
              <el-input v-model="model.form.name"></el-input>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12">
            <el-form-item label="排序" prop="sort">
              <el-input-number
                v-model="model.form.sort"
                :min="0"
                :max="99"
                controls-position="right"
              ></el-input-number>
            </el-form-item>
          </el-col>
        </el-row>
      </el-form>
      <template #footer>
        <el-button size="default" @click="onFormCancel">取消</el-button>
        <el-button type="primary" size="default" @click="onFormConfirm">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { defineExpose, inject, nextTick, onMounted, reactive, ref } from 'vue'
import { EditPen, Delete } from '@element-plus/icons-vue'

import { ElMessage, ElMessageBox } from 'element-plus'

import { getGroups, addGroup, updateGroup, deleteGroup } from '@/api'
import { validateForm } from '@/utils/common'

const emit = defineEmits(['select'])

const formRef = ref(null)
const formDialog = ref(false)

const activeGroupId = inject('activeGroupId')
const model = reactive({
  list: [],
  form: {},
  rules: {
    name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
    sort: [{ required: true, message: '请输入排序', trigger: 'blur' }]
  }
})

const updateDeviceGroup = inject('updateDeviceGroup')
const updateActiveGroupId = inject('updateActiveGroupId')

onMounted(() => {
  updateActiveGroupId('all')
  refresh()
})

const refresh = async () => {
  let deviceGroup = []
  try {
    const res = await getGroups()
    deviceGroup = res || []
  } catch (err) {
    console.error(err)
  }

  deviceGroup.unshift({
    _id: 'all',
    name: '全部配置',
    createdAt: 0,
    updatedAt: 0
  })
  updateDeviceGroup(deviceGroup)
  model.list = deviceGroup
}

const onAddClick = () => {
  model.form = {
    name: '',
    sort: 0
  }
  nextTick(() => {
    formRef.value && formRef.value.clearValidate()
    formDialog.value = true
  })
}
const onFormCancel = () => {
  formDialog.value = false
}
const onFormConfirm = async () => {
  let ret = await validateForm(formRef.value)
  if (ret) {
    try {
      if (model.form._id) {
        await updateGroup(model.form)
        ElMessage({ type: 'success', showClose: true, message: '编辑成功！' })
      } else {
        await addGroup(model.form)
        ElMessage({ type: 'success', showClose: true, message: '添加成功！' })
      }
      formDialog.value = false
      refresh()
    } catch (err) {
      if (!err?.silent)
        ElMessage({
          type: 'error',
          showClose: true,
          message: (model.form._id ? '编辑失败：' : '添加失败：') + (err?.message || err)
        })
    }
  }
}
const onItemClick = (item) => {
  updateActiveGroupId(item._id)
  emit('select', item)
}
const onEditClick = (item) => {
  model.form = { ...item }
  nextTick(() => {
    formRef.value && formRef.value.clearValidate()
    formDialog.value = true
  })
}
const onDeleteClick = (item) => {
  ElMessageBox.confirm('是否删除该分组？', '提示', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  })
    .then(async () => {
      try {
        await deleteGroup(item._id)
        ElMessage({
          type: 'success',
          showClose: true,
          message: '删除成功！'
        })
        refresh()
        if (activeGroupId.value == item._id) {
          updateActiveGroupId('all')
        }
      } catch (err) {
        if (!err?.silent) ElMessage({ type: 'error', showClose: true, message: '删除失败！' })
      }
    })
    .catch(() => {})
}

defineExpose({
  onAddClick
})
</script>

<style lang="scss" scoped>
.content {
  height: calc(100% - 60px);

  .list {
    width: 100%;
    height: 100%;
    overflow-y: auto;

    .item {
      position: relative;
      padding: 0 24px 0 18px;
      height: 56px;
      line-height: 56px;
      border-left: 5px solid transparent;
      border-bottom: 1px solid #ebeef5;
      font-size: 18px;
      color: $text-color2;

      &:hover {
        background-color: $background-color1;

        .action {
          visibility: visible;
        }
      }

      .name {
        width: 100%;
        padding-right: 64px;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .action {
        position: absolute;
        top: 0;
        right: 24px;
        background-color: $background-color1;
        visibility: hidden;

        .el-icon {
          width: 28px;
          height: 28px;
          text-align: center;
          font-size: 20px;
          color: $blue-color;
          cursor: pointer;
        }
      }
    }

    .item.active {
      border-left: 5px solid $blue-color;
    }
  }
}
</style>
