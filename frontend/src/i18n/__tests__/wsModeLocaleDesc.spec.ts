import { describe, expect, it } from 'vitest'

import zh from '../locales/zh/admin/accounts'

describe('OpenAI WS mode locale descriptions', () => {
  it('documents the global v2 router requirement for account WS modes', () => {
    expect(zh.accounts.openai.wsModeDesc).toContain('mode_router_v2_enabled=true')
  })
})
