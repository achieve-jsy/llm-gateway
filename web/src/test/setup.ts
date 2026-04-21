/** Vitest 环境：修复 Node 实验性 localStorage 缺方法导致 auth store 导入失败 */
const memory = new Map<string, string>()

const polyfill: Storage = {
  get length() {
    return memory.size
  },
  clear() {
    memory.clear()
  },
  getItem(key: string) {
    return memory.has(key) ? memory.get(key)! : null
  },
  key(index: number) {
    return Array.from(memory.keys())[index] ?? null
  },
  removeItem(key: string) {
    memory.delete(key)
  },
  setItem(key: string, value: string) {
    memory.set(key, String(value))
  },
}

Object.defineProperty(globalThis, 'localStorage', {
  value: polyfill,
  configurable: true,
  writable: true,
})
