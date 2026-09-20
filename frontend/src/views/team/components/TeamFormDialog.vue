<template>
  <el-dialog
    :model-value="modelValue"
    :title="isEdit ? '编辑班组' : '新增班组'"
    width="640px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px">
      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="班组名称" prop="name">
            <el-input v-model="form.name" maxlength="64" placeholder="例如: 市政照明一班" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="班组人数" prop="member_count">
            <el-input-number v-model="form.member_count" :min="1" :max="999" style="width: 100%" />
          </el-form-item>
        </el-col>
        <el-col :span="24">
          <el-form-item label="可承接类型" prop="fault_types">
            <el-select v-model="form.fault_types" multiple collapse-tags :max-collapse-tags="4" placeholder="选择可承接的故障类型" style="width: 100%">
              <el-option v-for="item in meta.fault_types" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="24">
          <el-form-item label="负责区域" prop="areas">
            <el-select v-model="form.areas" multiple collapse-tags :max-collapse-tags="4" placeholder="选择负责的道路区域" style="width: 100%">
              <el-option v-for="item in meta.roads" :key="item" :label="item" :value="item" />
            </el-select>
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="联系人" prop="contact">
            <el-input v-model="form.contact" maxlength="64" placeholder="选填" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="联系电话" prop="phone">
            <el-input v-model="form.phone" maxlength="32" placeholder="选填" />
          </el-form-item>
        </el-col>
        <el-col :span="12">
          <el-form-item label="状态" prop="status">
            <el-radio-group v-model="form.status">
              <el-radio-button value="enabled">启用</el-radio-button>
              <el-radio-button value="disabled">停用</el-radio-button>
            </el-radio-group>
          </el-form-item>
        </el-col>
        <el-col :span="24">
          <el-form-item label="备注" prop="remark">
            <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
          </el-form-item>
        </el-col>
      </el-row>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { teamApi, dispatchApi } from '@/api/dispatch'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  model: { type: Object, default: null },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)
const meta = ref({ fault_types: [], roads: [] })

const isEdit = computed(() => Boolean(props.model?.id))

const createForm = () => ({
  name: '',
  member_count: 5,
  fault_types: [],
  areas: [],
  contact: '',
  phone: '',
  status: 'enabled',
  remark: '',
})

const form = reactive(createForm())

const rules = {
  name: [{ required: true, message: '请输入班组名称', trigger: 'blur' }],
  member_count: [{ required: true, message: '请输入班组人数', trigger: 'blur' }],
  fault_types: [{ required: true, type: 'array', min: 1, message: '至少选择一种可承接故障类型', trigger: 'change' }],
  areas: [{ required: true, type: 'array', min: 1, message: '至少选择一个负责区域', trigger: 'change' }],
}

async function loadMeta() {
  try {
    meta.value = await dispatchApi.meta()
  } catch (error) {
    meta.value = { fault_types: [], roads: [] }
  }
}

// 打开弹窗时初始化: 编辑模式回填班组信息。
function syncForm() {
  Object.assign(form, createForm())
  if (props.model) {
    Object.assign(form, {
      name: props.model.name,
      member_count: props.model.member_count,
      fault_types: [...(props.model.fault_types ?? [])],
      areas: [...(props.model.areas ?? [])],
      contact: props.model.contact,
      phone: props.model.phone,
      status: props.model.status,
      remark: props.model.remark,
    })
  }
  loadMeta()
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    if (isEdit.value) {
      await teamApi.update(props.model.id, { ...form })
      ElMessage.success('班组已更新')
    } else {
      await teamApi.create({ ...form })
      ElMessage.success('班组已创建')
    }
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    submitting.value = false
  }
}
</script>
