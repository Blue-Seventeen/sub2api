import { apiClient } from '../client'
import type { BasePaginationResponse } from '@/types'
import type { PromotionCommissionItem, PromotionScript } from '@/api/promotion'

export interface PromotionRelationItem {
  user_id: number
  email: string
  username: string
  invite_code: string
  level_name: string
  parent_user_id?: number
  parent_email: string
  direct_children_count: number
  total_children_count: number
  activated_direct_count: number
  bound_at?: string
}

export interface PromotionRelationChain {
  current?: {
    user_id: number
    email: string
    level_name: string
    invite_code: string
    invite_count: number
    total_rate: number
    actual_rebate_rate?: number
  }
  parent?: {
    user_id: number
    email: string
    level_name: string
    invite_code: string
    invite_count: number
    total_rate: number
    actual_rebate_rate?: number
  }
  grandparent?: {
    user_id: number
    email: string
    level_name: string
    invite_code: string
    invite_count: number
    total_rate: number
    actual_rebate_rate?: number
  }
}

export interface PromotionAdminDashboard {
  total_settled_amount: number
  pending_amount: number
  bound_users: number
  activated_users: number
  today_new_bindings: number
  today_new_activates: number
  today_pending_amount: number
}

export interface PromotionLevelConfig {
  id?: number
  level_no: number
  level_name: string
  required_activated_invites: number
  direct_rate: number
  indirect_rate: number
  sort_order: number
  enabled: boolean
}

export interface PromotionAdminDownlineItem {
  user_id: number
  email: string
  username?: string
  masked_email: string
  relation_depth: number
  level_name: string
  activated: boolean
  today_contribution: number
  total_contribution: number
  joined_at: string
  activated_at?: string
}

export interface PromotionSettingsConfig {
  activation_threshold_amount: number
  activation_bonus_amount: number
  daily_settlement_time: string
  settlement_enabled: boolean
  rule_activation_template: string
  rule_direct_template: string
  rule_indirect_template: string
  rule_level_summary_template: string
  invite_base_url: string
  poster_logo_url: string
  poster_title: string
  poster_headline: string
  poster_description: string
  poster_scan_hint: string
  poster_tags: string[]
}

export interface PromotionConfigResponse {
  settings: PromotionSettingsConfig
  levels: PromotionLevelConfig[]
  effective_timezone: string
  resolved_invite_base_url?: string
}

export const adminPromotionAPI = {
  getDashboard() {
    return apiClient.get<PromotionAdminDashboard>('/admin/promotion/dashboard')
  },

  getRelations(params?: { page?: number; page_size?: number; keyword?: string }) {
    return apiClient.get<BasePaginationResponse<PromotionRelationItem>>('/admin/promotion/relations', { params })
  },

  getRelationChain(userID: number) {
    return apiClient.get<PromotionRelationChain>(`/admin/promotion/relations/${userID}/chain`)
  },

  getDownlines(userID: number, params?: { page?: number; page_size?: number; status?: string; sort_by?: string; sort_order?: 'asc' | 'desc' }) {
    return apiClient.get<BasePaginationResponse<PromotionAdminDownlineItem>>(`/admin/promotion/relations/${userID}/downlines`, { params })
  },

  removeDirectDownline(userID: number, downlineUserID: number, note?: string) {
    return apiClient.delete<{ user_id: number; downline_user_id: number; removed: boolean }>(
      `/admin/promotion/relations/${userID}/downlines/${downlineUserID}`,
      {
        data: note ? { note } : undefined
      }
    )
  },

  bindParent(data: { user_id: number; parent_user_id: number; note?: string }) {
    return apiClient.post('/admin/promotion/relations/bind-parent', data)
  },

  removeParent(userID: number, note?: string) {
    return apiClient.delete(`/admin/promotion/relations/${userID}/parent`, {
      data: note ? { note } : undefined
    })
  },

  getCommissions(params?: {
    page?: number
    page_size?: number
    keyword?: string
    type?: string
    status?: string
    date_from?: string
    date_to?: string
  }) {
    return apiClient.get<BasePaginationResponse<PromotionCommissionItem>>('/admin/promotion/commissions', { params })
  },

  manualGrant(data: { user_id: number; amount: number; note?: string }) {
    return apiClient.post<PromotionCommissionItem>('/admin/promotion/commissions/manual-grant', data)
  },

  updateCommission(id: number, data: { amount: number; note?: string }) {
    return apiClient.put<PromotionCommissionItem>(`/admin/promotion/commissions/${id}`, data)
  },

  settleCommission(id: number, note?: string) {
    return apiClient.post<PromotionCommissionItem>(`/admin/promotion/commissions/${id}/settle`, note ? { note } : {})
  },

  batchSettle(ids: number[], note?: string) {
    return apiClient.post<{ settled_count: number; total_amount: number }>('/admin/promotion/commissions/batch-settle', {
      ids,
      note
    })
  },

  cancelCommission(id: number, note?: string) {
    return apiClient.post<PromotionCommissionItem>(`/admin/promotion/commissions/${id}/cancel`, note ? { note } : {})
  },

  getConfig() {
    return apiClient.get<PromotionConfigResponse>('/admin/promotion/config')
  },

  updateConfig(data: { settings: PromotionSettingsConfig; levels: PromotionLevelConfig[] }) {
    return apiClient.put<PromotionConfigResponse>('/admin/promotion/config', data)
  },

  getScripts(params?: { page?: number; page_size?: number; keyword?: string; category?: string }) {
    return apiClient.get<BasePaginationResponse<PromotionScript>>('/admin/promotion/scripts', { params })
  },

  createScript(data: Partial<PromotionScript>) {
    return apiClient.post<PromotionScript>('/admin/promotion/scripts', data)
  },

  updateScript(id: number, data: Partial<PromotionScript>) {
    return apiClient.put<PromotionScript>(`/admin/promotion/scripts/${id}`, data)
  },

  deleteScript(id: number) {
    return apiClient.delete<{ id: number; deleted: boolean }>(`/admin/promotion/scripts/${id}`)
  }
}

export default adminPromotionAPI
