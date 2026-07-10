type LocaleMessages = Record<string, any>

function isPlainObject(value: unknown): value is LocaleMessages {
  return Object.prototype.toString.call(value) === '[object Object]'
}

export function mergeLocaleMessages(...sources: LocaleMessages[]): LocaleMessages {
  const output: LocaleMessages = {}

  for (const source of sources) {
    for (const [key, value] of Object.entries(source)) {
      const existing = output[key]
      if (isPlainObject(existing) && isPlainObject(value)) {
        output[key] = mergeLocaleMessages(existing, value)
      } else {
        output[key] = value
      }
    }
  }

  return output
}
