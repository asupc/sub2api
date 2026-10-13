import { createI18n } from 'vue-i18n'

// 二开说明：仅保留中文(zh)，语言固定，不提供切换能力
type LocaleCode = 'zh'

const DEFAULT_LOCALE: LocaleCode = 'zh'

export const i18n = createI18n({
  legacy: false,
  locale: DEFAULT_LOCALE,
  fallbackLocale: DEFAULT_LOCALE,
  messages: {},
  // 禁用 HTML 消息警告 - 引导步骤使用富文本内容（driver.js 支持 HTML）
  // 这些内容是内部定义的，不存在 XSS 风险
  warnHtmlMessage: false
})

export async function loadLocaleMessages(locale: LocaleCode): Promise<void> {
  const module = await import('./locales/zh')
  i18n.global.setLocaleMessage(locale, module.default)
}

export async function initI18n(): Promise<void> {
  await loadLocaleMessages(DEFAULT_LOCALE)
  document.documentElement.setAttribute('lang', DEFAULT_LOCALE)
}

export function getLocale(): LocaleCode {
  return DEFAULT_LOCALE
}

export default i18n
