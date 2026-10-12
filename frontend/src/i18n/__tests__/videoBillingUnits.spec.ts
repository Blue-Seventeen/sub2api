import { describe, expect, it } from 'vitest'
import en from '../locales/en'
import zh from '../locales/zh'

describe('video billing units in merged locales', () => {
  it('keeps usage video filters neutral to the configured pricing unit', () => {
    expect(zh.admin.usage.billingModeVideo).toBe('视频')
    expect(en.admin.usage.billingModeVideo).toBe('Video')
  })

  it.each([
    { name: 'zh', messages: zh, mode: '视频（按秒）', unit: '/ 秒' },
    { name: 'en', messages: en, mode: 'Video (Per Second)', unit: '/s' },
  ])('labels video prices per second throughout $name channels and plaza', ({ messages, mode, unit }) => {
    expect(messages.admin.channels.billingMode.video).toBe(mode)
    expect(messages.availableChannels.pricing.billingModeVideo).toBe(mode)
    expect(messages.admin.availableChannels.pricing.billingModeVideo).toBe(mode)
    expect(messages.modelPlaza.table.videoPerSecond).toBe(mode)
    expect(messages.modelPlaza.table.perUnitSecond).toBe(unit)
  })
})
