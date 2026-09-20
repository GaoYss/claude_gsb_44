<template>
  <el-dialog
    :model-value="modelValue"
    title="派工"
    width="680px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="关联故障" prop="fault_id">
        <el-select
          v-model="form.fault_id"
          filterable
          remote
          reserve-keyword
          :remote-method="searchFaults"
          :loading="faultLoading"
          placeholder="输入故障单号 / 路灯编号搜索未闭环故障"
          style="width: 100%"
          @change="handleFaultChange"
        >
          <el-option
            v-for="item in faultCandidates"
            :key="item.id"
            :label="`${item.fault_no} · ${item.lamp_code} · ${item.fault_type}`"
            :value="item.id"
          />
        </el-select>
      </el-form-item>

      <el-descriptions v-if="currentFault" :column="2" border size="small" class="fault-summary">
        <el-descriptions-item label="故障单号">{{ currentFault.fault_no }}</el-descriptions-item>
        <el-descriptions-item label="路灯编号">{{ currentFault.lamp_code }}</el-descriptions-item>
        <el-descriptions-item label="所在道路">{{ currentFault.road_name }}</el-descriptions-item>
        <el-descriptions-item label="故障类型">{{ currentFault.fault_type }}</el-descriptions-item>
        <el-descriptions-item label="紧急程度">
          <StatusTag :dict="FAULT_LEVEL" :value="currentFault.fault_level" />
        </el-descriptions-item>
        <el-descriptions-item label="处理状态">
          <StatusTag :dict="FAULT_STATUS" :value="currentFault.status" />
        </el-descriptions-item>
      </el-descriptions>

      <el-form-item label="承接班组" prop="team_id">
        <div v-loading="suggestLoading" class="team-panel">
          <el-radio-group v-model="form.team_id" class="team-group">
            <el-radio :value="0" class="team-option">
              <span>自动均衡</span>
              <el-tag size="small" type="success" effect="plain">在办最少优先</el-tag>
            </el-radio>
            <el-radio
              v-for="item in suggestions"
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
          <div v-if="!form.fault_id" class="text-muted team-empty">请先选择关联故障, 系统按故障类型与负责区域校验匹配班组</div>
          <div v-else-if="!suggestLoading && matchedCount === 0" class="team-empty mismatch-warning">
            没有可承接该故障的启用班组, 请先到「班组管理」维护可承接类型与负责区域
          </div>
        </div>
      </el-form-item>

      <el-row :gutter="16">
        <el-col :span="12">
          <el-form-item label="派工人" prop="operator">
            <el-input v-model="form.operator" maxlength="64" placeholder="选填, 例如: 调度员王芳" />
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
      <el-button type="primary" :loading="submitting" @click="handleSubmit">确认派工</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import StatusTag from '@/components/common/StatusTag.vue'
import { faultApi } from '@/api/fault'
import { dispatchApi } from '@/api/dispatch'
import { FAULT_LEVEL, FAULT_STATUS } from '@/constants/dict'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)
const faultLoading = ref(false)
const suggestLoading = ref(false)
const faultCandidates = ref([])
const suggestions = ref([])

const createForm = () => ({
  fault_id: undefined,
  team_id: 0,
  operator: '',
  remark: '',
})

const form = reactive(createForm())

const rules = {
  fault_id: [{ required: true, message: '请选择关联故障', trigger: 'change' }],
}

const currentFault = computed(() => faultCandidates.value.find((item) => item.id === form.fault_id) ?? null)
const matchedCount = computed(() => suggestions.value.filter((item) => item.matched).length)

function mismatchReason(item) {
  const parts = []
  if (!item.type_matched) parts.push('不可承接该故障类型')
  if (!item.area_matched) parts.push('负责区域不覆盖该道路')
  return parts.join('; ')
}

async function searchFaults(keyword = '') {
  faultLoading.value = true
  try {
    const data = await faultApi.list({ keyword, only_open: true, page: 1, page_size: 20 }, { silent: true })
    faultCandidates.value = data?.items ?? []
  } catch (error) {
    faultCandidates.value = []
  } finally {
    faultLoading.value = false
  }
}

async function loadSuggestions(faultId) {
  suggestLoading.value = true
  try {
    suggestions.value = await dispatchApi.suggest(faultId)
  } catch (error) {
    suggestions.value = []
  } finally {
    suggestLoading.value = false
  }
}

function handleFaultChange() {
  form.team_id = 0
  suggestions.value = []
  if (form.fault_id) {
    loadSuggestions(form.fault_id)
  }
}

// 打开弹窗时重置表单并预载故障候选。
async function syncForm() {
  Object.assign(form, createForm())
  suggestions.value = []
  await searchFaults('')
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload = { fault_id: form.fault_id, operator: form.operator, remark: form.remark }
    if (form.team_id > 0) {
      payload.team_id = form.team_id
    }
    await dispatchApi.create(payload)
    ElMessage.success('派工成功')
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.fault-summary {
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
