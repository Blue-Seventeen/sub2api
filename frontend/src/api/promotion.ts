import { apiClient } from './client'
import type { BasePaginationResponse } from '@/types'

export interface PromotionOverview {
  user_id: number
  invite_code: string
  invite_link: string
  poster_config: {
    invite_base_url: string
    logo_url: string
    title: string
    headline: string
    description: string
    scan_hint: string
    tags: string[]
    primary_invite_code: string
  }
  current_level_no: number
  current_level_name: string
  current_direct_activated: number
  current_direct_rate: number
  current_indirect_rate: number
  current_total_rate: number
  next_level_no?: number
  next_level_name?: string
  next_level_required_activate?: number
  today_earnings: number
  pending_amount: number
  settled_amount: number
  total_reward_amount: number
  commission_amount: number
  activation_amount: number
  total_invites: number
  activated_invites: number
  inactive_invites: number
  activation_threshold_amount: number
  activation_bonus_amount: number
  level_rate_summaries: Array<{
    level_no: number
    level_name: string
    required_activated_invites: number
    direct_rate: number
    indirect_rate: number
    total_rate: number
  }>
  rule_templates: {
    activation: string
    direct: string
    indirect: string
    level_summary: string
  }
  leaderboard: Array<{
    user_id: number
    masked_email: string
    level_name: string
    invite_count: number
    total_earnings: number
    current_level_no: number
  }>
}

export interface PromotionTeamItem {
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

export interface PromotionCommissionItem {
  id: number
  beneficiary_user_id: number
  beneficiary_email: string
  beneficiary_masked: string
  source_user_id?: number
  source_user_email: string
  source_user_masked: string
  commission_type: string
  relation_depth: number
  business_date: string
  base_amount: number
  amount: number
  status: string
  level_name: string
  rate_snapshot?: number
  note: string
  settled_at?: string
  cancelled_at?: string
  created_at: string
}

export interface PromotionScript {
  id: number
  name: string
  category: string
  content: string
  rendered_preview: string
  use_count: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export const promotionAPI = {
  previewReferrer(inviteCode: string) {
    return apiClient.get<{
      valid: boolean
      invite_code: string
      referrer?: {
        user_id: number
        masked_email: string
        level_name: string
      }
    }>(`/promotion/public/referrers/${encodeURIComponent(inviteCode)}`)
  },

  async bindReferrer(inviteCode: string) {
    const { data } = await apiClient.post<{
      user_id: number
      parent_user_id?: number
      bound_at?: string
      invite_code: string
    }>('/promotion/me/bind-referrer', {
      invite_code: inviteCode
    })
    return data
  },

  async getOverview() {
    const { data } = await apiClient.get<PromotionOverview>('/promotion/me/overview')
    return data
  },

  async getTeam(params?: { page?: number; page_size?: number; keyword?: string; status?: string; sort_by?: string; sort_order?: 'asc' | 'desc' }) {
    const { data } = await apiClient.get<BasePaginationResponse<PromotionTeamItem>>('/promotion/me/team', {
      params
    })
    return data
  },

  async getEarnings(params?: { page?: number; page_size?: number; keyword?: string; type?: string; status?: string }) {
    const { data } = await apiClient.get<BasePaginationResponse<PromotionCommissionItem>>('/promotion/me/earnings', {
      params
    })
    return data
  },

  async getScripts() {
    const { data } = await apiClient.get<PromotionScript[]>('/promotion/me/scripts')
    return data
  },

  async markScriptUsed(id: number) {
    const { data } = await apiClient.post<{ id: number; used_at: string }>(`/promotion/me/scripts/${id}/use`)
    return data
  }
}

export default promotionAPI
