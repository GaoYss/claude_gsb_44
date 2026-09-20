<template>
  <div class="page">
    <PageHeader title="班组管理" description="维护各班组可承接的故障类型与负责区域, 作为派工与改派匹配校验的依据">
      <el-button :icon="Refresh" @click="load">刷新</el-button>
      <el-button type="primary" :icon="Plus" @click="openCreate">新增班组</el-button>
    </PageHeader>

    <el-card shadow="never">
      <div class="filter-bar">
        <el-input v-model="query.keyword" placeholder="班组名称 / 联系人" clearable @keyup.enter="handleSearch" />
        <el-select v-model="query.status" placeholder="班组状态" clearable @change="handleSearch">
          <el-option v-for="(item, key) in TEAM_STATUS" :key="key" :label="item.label" :value="key" />
        </el-select>
        <el-button type="primary" :icon="Search" @click="handleSearch">查询</el-button>
        <el-button :icon="RefreshLeft" @click="handleReset">重置</el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="name" label="班组名称" width="140" fixed="left" />
        <el-table-column prop="member_count" label="人数" width="80" align="center" />
        <el-table-column label="可承接故障类型" min-width="220">
          <template #default="{ row }">
            <el-tag v-for="item in row.fault_types" :key="item" size="small" effect="plain" class="tag-item">{{ item }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="负责区域" min-width="200">
          <template #default="{ row }">
            <el-tag v-for="item in row.areas" :key="item" size="small" type="info" effect="plain" class="tag-item">{{ item }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="联系人" width="100">
          <template #default="{ row }">{{ row.contact || '-' }}</template>
        </el-table-column>
        <el-table-column label="联系电话" width="130">
          <template #default="{ row }">{{ row.phone || '-' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }"><StatusTag :dict="TEAM_STATUS" :value="row.status" /></template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="160" show-overflow-tooltip />
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="handleDelete(row)">删除</el-button>
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

    <TeamFormDialog v-model="formVisible" :model="editing" @saved="handleSaved" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import DataPagination from '@/components/common/DataPagination.vue'
import TeamFormDialog from './components/TeamFormDialog.vue'
import { teamApi } from '@/api/dispatch'
import { TEAM_STATUS } from '@/constants/dict'
import { useListPage } from '@/composables/useListPage'

const { loading, rows, total, query, load, search, reset, changePage, changePageSize } = useListPage(teamApi.list, {
  keyword: '',
  status: '',
})

const formVisible = ref(false)
const editing = ref(null)

function handleSearch() {
  search()
}

function handleReset() {
  reset()
}

function openCreate() {
  editing.value = null
  formVisible.value = true
}

function openEdit(row) {
  editing.value = { ...row }
  formVisible.value = true
}

async function handleDelete(row) {
  try {
    await ElMessageBox.confirm(`确认删除班组 ${row.name} ? 历史派工记录会保留班组名称快照`, '删除确认', {
      type: 'warning',
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
    })
  } catch (error) {
    return
  }

  try {
    await teamApi.remove(row.id)
    ElMessage.success('班组已删除')
    handleSaved()
  } catch (error) {
    // 错误提示由请求拦截器统一处理
  }
}

function handleSaved() {
  load()
}
</script>

<style scoped>
.tag-item {
  margin-right: 6px;
  margin-bottom: 2px;
}
</style>
