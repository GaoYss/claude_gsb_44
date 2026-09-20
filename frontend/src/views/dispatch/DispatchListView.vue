<template>
  <div class="page">
    <PageHeader title="派工管理" description="按故障类型与负责区域匹配派工, 改派留痕可追溯, 各班组在办数量保持均衡">
      <el-button :icon="Refresh" @click="reloadAll">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="openDispatch">派工</el-button>
    </PageHeader>

    <div v-loading="overviewLoading" class="card-grid">
      <StatCard label="在办工单" :value="overview.ongoing_total" icon="Loading" color="#e6a23c" hint="各班组当前在办合计" />
      <StatCard label="已办结工单" :value="overview.finished_total" icon="CircleCheck" color="#67c23a" hint="累计办结派工单" />
      <StatCard
        label="超时未完工"
        :value="overview.overdue_total"
        icon="AlarmClock"
        color="#f56c6c"
        :hint="`在办超过 ${overview.overdue_hours ?? 24} 小时未办结`"
      />
      <StatCard label="人均维修量" :value="overview.per_capita_avg" suffix="单/人" icon="User" color="#409eff" hint="已办结工单 ÷ 班组总人数" />
    </div>

    <el-card shadow="never">
      <div class="section-title">
        <span>班组概览</span>
        <span class="text-muted section-tip">在办数量尽量均衡, 派工时优先推荐在办少的班组</span>
      </div>
      <el-table :data="overview.teams" stripe>
        <el-table-column prop="team_name" label="班组" min-width="130" />
        <el-table-column prop="member_count" label="人数" width="80" align="center" />
        <el-table-column label="在办(均衡)" width="220">
          <template #default="{ row }">
            <div class="ongoing-cell">
              <el-progress
                :percentage="ongoingPercent(row.ongoing_count)"
                :status="row.overdue_count > 0 ? 'exception' : ''"
                :stroke-width="10"
                :show-text="false"
                class="ongoing-bar"
              />
              <span>{{ row.ongoing_count }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="finished_count" label="已办结" width="90" align="center" />
        <el-table-column prop="reassigned_count" label="已改出" width="90" align="center" />
        <el-table-column label="超时未完工" width="110" align="center">
          <template #default="{ row }">
            <span :class="{ 'overdue-text': row.overdue_count > 0 }">{{ row.overdue_count }}</span>
          </template>
        </el-table-column>
        <el-table-column label="人均维修量" width="120" align="center">
          <template #default="{ row }">{{ row.per_capita_repair }} 单/人</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }"><StatusTag :dict="TEAM_STATUS" :value="row.status" /></template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-card shadow="never">
      <div class="filter-bar">
        <el-input v-model="query.keyword" placeholder="派工单号 / 故障单号 / 路灯编号 / 班组" clearable @keyup.enter="handleSearch" />
        <el-select v-model="query.team_id" placeholder="班组" clearable filterable @change="handleSearch">
          <el-option v-for="item in teamOptions" :key="item.id" :label="item.name" :value="item.id" />
        </el-select>
        <el-select v-model="query.status" placeholder="工单状态" clearable @change="handleSearch">
          <el-option v-for="(item, key) in DISPATCH_STATUS" :key="key" :label="item.label" :value="key" />
        </el-select>
        <el-checkbox v-model="onlyOverdue" label="只看超时未完工" @change="handleSearch" />
        <el-button type="primary" :icon="Search" @click="handleSearch">查询</el-button>
        <el-button :icon="RefreshLeft" @click="handleReset">重置</el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="dispatch_no" label="派工单号" width="150" fixed="left" />
        <el-table-column prop="fault_no" label="故障单号" width="150" />
        <el-table-column prop="lamp_code" label="路灯编号" width="110" />
        <el-table-column prop="road_name" label="道路" width="110" />
        <el-table-column prop="fault_type" label="故障类型" width="100" />
        <el-table-column label="紧急程度" width="90">
          <template #default="{ row }"><StatusTag :dict="FAULT_LEVEL" :value="row.fault_level" /></template>
        </el-table-column>
        <el-table-column label="班组" width="120">
          <template #default="{ row }">
            <div>{{ row.team_name }}</div>
            <el-tooltip v-if="row.from_team_name" :content="`改派原因: ${row.reassign_reason || '-'}`" placement="top">
              <span class="text-muted reassign-from">由 {{ row.from_team_name }} 改入</span>
            </el-tooltip>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="110">
          <template #default="{ row }">
            <StatusTag :dict="DISPATCH_STATUS" :value="row.status" />
            <el-tag v-if="row.overdue" type="danger" size="small" effect="plain" class="overdue-tag">超时</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="派工时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.dispatched_at) }}</template>
        </el-table-column>
        <el-table-column label="办结时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.finished_at) }}</template>
        </el-table-column>
        <el-table-column prop="operator" label="派工人" width="100">
          <template #default="{ row }">{{ row.operator || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.status === 'ongoing'" link type="warning" @click="openReassign(row)">改派</el-button>
            <el-button v-if="row.status === 'ongoing'" link type="success" @click="handleFinish(row)">办结</el-button>
            <el-button link type="primary" @click="openFaultDetail(row)">故障详情</el-button>
          </template>
        </el-table-column>
      </el-table>
      <DataPagination
        :page="query.page"
        :page-size="query.page_size"
        :total="total"
        @page-change="changePage"
        @size-change="changePageSize"
      />
    </el-card>

    <DispatchFormDialog v-model="dispatchVisible" @saved="reloadAll" />
    <ReassignDialog v-model="reassignVisible" :record="reassigning" @saved="reloadAll" />
    <FaultDetailDrawer v-model="detailVisible" :fault-id="activeFaultId" />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import StatCard from '@/components/common/StatCard.vue'
import DataPagination from '@/components/common/DataPagination.vue'
import DispatchFormDialog from './components/DispatchFormDialog.vue'
import ReassignDialog from './components/ReassignDialog.vue'
import FaultDetailDrawer from '@/views/fault/components/FaultDetailDrawer.vue'
import { dispatchApi, teamApi } from '@/api/dispatch'
import { DISPATCH_STATUS, FAULT_LEVEL, TEAM_STATUS } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'
import { useListPage } from '@/composables/useListPage'

const { loading, rows, total, query, load, search, reset, changePage, changePageSize } = useListPage(dispatchApi.list, {
  keyword: '',
  team_id: '',
  status: '',
  overdue: false,
})

const overview = ref({ teams: [] })
const overviewLoading = ref(false)
const teamOptions = ref([])
const dispatchVisible = ref(false)
const reassignVisible = ref(false)
const reassigning = ref(null)
const detailVisible = ref(false)
const activeFaultId = ref(null)

// 只看超时未完工开关, 映射为 overdue 查询参数
const onlyOverdue = computed({
  get: () => Boolean(query.overdue),
  set: (value) => {
    query.overdue = value
  },
})

const maxOngoing = computed(() => Math.max(1, ...(overview.value.teams ?? []).map((item) => item.ongoing_count)))

function ongoingPercent(count) {
  return Math.round(((Number(count) || 0) / maxOngoing.value) * 100)
}

async function loadOverview() {
  overviewLoading.value = true
  try {
    overview.value = await dispatchApi.overview()
  } catch (error) {
    overview.value = { teams: [] }
  } finally {
    overviewLoading.value = false
  }
}

async function loadTeamOptions() {
  try {
    teamOptions.value = await teamApi.options()
  } catch (error) {
    teamOptions.value = []
  }
}

function reloadAll() {
  load()
  loadOverview()
  loadTeamOptions()
}

function handleSearch() {
  search()
}

function handleReset() {
  reset()
}

function openDispatch() {
  dispatchVisible.value = true
}

function openReassign(row) {
  reassigning.value = { ...row }
  reassignVisible.value = true
}

function openFaultDetail(row) {
  activeFaultId.value = row.fault_id
  detailVisible.value = true
}

async function handleFinish(row) {
  try {
    await ElMessageBox.confirm(`确认办结派工单 ${row.dispatch_no}(${row.team_name}) ?`, '办结确认', {
      type: 'warning',
      confirmButtonText: '确认办结',
      cancelButtonText: '取消',
    })
  } catch (error) {
    return
  }

  try {
    await dispatchApi.finish(row.id, {})
    ElMessage.success('派工单已办结')
    reloadAll()
  } catch (error) {
    // 错误提示由请求拦截器统一处理
  }
}

onMounted(() => {
  loadOverview()
  loadTeamOptions()
})
</script>

<style scoped>
.section-tip {
  font-size: 12px;
  font-weight: 400;
}

.ongoing-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ongoing-bar {
  flex: 1;
}

.overdue-text {
  color: #f56c6c;
  font-weight: 600;
}

.overdue-tag {
  margin-left: 4px;
}

.reassign-from {
  font-size: 12px;
  cursor: help;
}
</style>
