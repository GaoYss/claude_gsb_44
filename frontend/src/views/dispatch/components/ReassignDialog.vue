<template>
  <el-dialog
    :model-value="modelValue"
    title="改派工单"
    width="620px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-descriptions v-if="record" :column="2" border size="small" class="record-summary">
      <el-descriptions-item label="派工单号">{{ record.dispatch_no }}</el-descriptions-item>
      <el-descriptions-item label="故障单号">{{ record.fault_no }}</el-descriptions-item>
      <el-descriptions-item label="故障类型">{{ record.fault_type }}</el-descriptions-item>
      <el-descriptions-item label="所在道路">{{ record.road_name }}</el-descriptions-item>
      <el-descriptions-item label="当前班组" :span="2">
        <el-tag size="small" type="warning" effect="light">{{ record.team_name }}</el-tag>
      </el-descriptions-item>
    </el-descriptions>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="接收班组" prop="team_id">
        <div v-loading="suggestLoading" class="team-panel">
          <el-radio-group v-model="form.team_id" class="team-group">
            <el-radio :value="0" class="team-option">
              <span>自动均衡</span>
              <el-tag size="small" type="success" effect="plain">在办最少优先</el-tag>
            </el-radio>
            <el-radio
              v-for="item in candidates"
              :key="item.team_id"
              :value="item.team_id"
              :disabled="!item.matched"
              class="team-option"
            >
              <span>{{ item.team_name }}</span>
              <el-tag size="small" effect="plain">在办 {{ item.ongoing_count }}</el-tag>
              <el-tag v-if="item.recommended" size="small" type="success" effect="plain">推荐</el-tag>
              <el-tooltip v-if="!item.matched" :content="mismatchReason(item)" placement="top">
                <el-tag size="small" type="danger" effect="plain">不匹配</el-tag>
              </el-tooltip>
            </el-radio>
          </el-radio-group>
          <div v-if="!suggestLoading && candidates.length === 0" class="team-empty mismatch-warning">
            除当前班组外没有可承接的启用班组, 请先到「班组管理」维护
          </div>
        </div>
      </el-form-item>

      <el-form-item label="改派原因" prop="reason">
        <el-input
          v-model="form.reason"
          type="textarea"
          :rows="3"
          maxlength="255"
          show-word-limit
          placeholder="必填, 例如: 原班组满负荷, 按区域分工调整至责任班组"
        />
      </el-form-item>

      <el-form-item label="操作人" prop="operator">
        <el-input v-model="form.operator" maxlength="64" placeholder="选填" />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">确认改派</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { dispatchApi } from '@/api/dispatch'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  record: { type: Object, default: null },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)
const suggestLoading = ref(false)
const suggestions = ref([])

const createForm = () => ({
  team_id: 0,
  reason: '',
  operator: '',
})

const form = reactive(createForm())

const rules = {
  reason: [{ required: true, message: '改派原因不能为空', trigger: 'blur' }],
}

// 候选班组: 排除当前班组, 改派后工单在原班组与接收班组各留一条记录
const candidates = computed(() => suggestions.value.filter((item) => item.team_id !== props.record?.team_id))

function mismatchReason(item) {
  const parts = []
  if (!item.type_matched) parts.push('不可承接该故障类型')
  if (!item.area_matched) parts.push('负责区域不覆盖该道路')
  return parts.join('; ')
}

async function loadSuggestions() {
  if (!props.record?.fault_id) return
  suggestLoading.value = true
  try {
    suggestions.value = await dispatchApi.suggest(props.record.fault_id)
  } catch (error) {
    suggestions.value = []
  } finally {
    suggestLoading.value = false
  }
}

// 打开弹窗时重置表单并加载候选班组。
function syncForm() {
  Object.assign(form, createForm())
  suggestions.value = []
  loadSuggestions()
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload = { reason: form.reason, operator: form.operator }
    if (form.team_id > 0) {
      payload.team_id = form.team_id
    }
    await dispatchApi.reassign(props.record.id, payload)
    ElMessage.success('改派成功, 原班组已留存改出记录')
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.record-summary {
  margin-bottom: 16px;
}

.team-panel {
  width: 100%;
}

.team-group {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 4px;
}

.team-option {
  width: 100%;
  margin-right: 0 !important;
}

.team-option :deep(.el-radio__label) {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.team-empty {
  padding: 8px 0;
  font-size: 13px;
}

.mismatch-warning {
  color: #f56c6c;
}
</style>
