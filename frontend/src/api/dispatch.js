import request from './request'

// 班组接口。
export const teamApi = {
  list: (params) => request.get('/teams', { params }),
  detail: (id) => request.get(`/teams/${id}`),
  options: () => request.get('/teams/options'),
  create: (data) => request.post('/teams', data),
  update: (id, data) => request.put(`/teams/${id}`, data),
  remove: (id) => request.delete(`/teams/${id}`),
}

// 派工接口。
export const dispatchApi = {
  list: (params) => request.get('/dispatches', { params }),
  detail: (id) => request.get(`/dispatches/${id}`),
  create: (data) => request.post('/dispatches', data),
  reassign: (id, data) => request.post(`/dispatches/${id}/reassign`, data),
  finish: (id, data) => request.post(`/dispatches/${id}/finish`, data),
  suggest: (faultId) => request.get(`/dispatches/suggest/${faultId}`),
  overview: () => request.get('/dispatches/overview'),
  meta: () => request.get('/dispatches/meta'),
}
