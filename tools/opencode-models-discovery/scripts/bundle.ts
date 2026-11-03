// 构建单文件 bundle，并保持「插件自身版本」与「服务端版本资产」同源（package.json）。
//
// 产物：
//   1. src/version.ts                                  —— 注入进 bundle，供自更新比对
//   2. ../../internal/controlplane/handler/assets/opencode-models-discovery.js
//   3. ../../internal/controlplane/handler/assets/opencode-models-discovery.version
//
// 用法：bun run scripts/bundle.ts （即 npm run build:bundle）
import { dirname, join } from 'node:path'
import { mkdir } from 'node:fs/promises'
import pkg from '../package.json' with { type: 'json' }

const scriptDir = import.meta.dir
const pluginRoot = join(scriptDir, '..')
const assetsDir = join(pluginRoot, '..', '..', 'internal', 'controlplane', 'handler', 'assets')
const version: string = pkg.version

// 1) 生成 src/version.ts（自动产物，勿手改）
const versionTsPath = join(pluginRoot, 'src', 'version.ts')
await Bun.write(
  versionTsPath,
  `// 本文件由 scripts/bundle.ts 自动生成，请勿手改。\n` +
    `// 版本号来源于 package.json，构建时写入并打进 bundle，供插件自更新比对。\n` +
    `export const PLUGIN_VERSION = ${JSON.stringify(version)}\n`,
)

// 2) 构建 bundle
const result = await Bun.build({
  entrypoints: [join(pluginRoot, 'src', 'index.ts')],
  target: 'node',
  format: 'esm',
})
if (!result.success) {
  for (const log of result.logs) console.error(log)
  throw new Error('bun build 失败')
}
const code = await result.outputs[0].text()

const jsPath = join(assetsDir, 'opencode-models-discovery.js')
const versionPath = join(assetsDir, 'opencode-models-discovery.version')
await mkdir(dirname(jsPath), { recursive: true })
await Bun.write(jsPath, code)
await Bun.write(versionPath, version + '\n')

console.log(`bundled v${version} -> ${jsPath} (${(code.length / 1024).toFixed(1)} KB)`)
console.log(`version asset -> ${versionPath}`)

// 3) V2 插件：OpenCode 2.x 换了整套插件契约，V1 bundle 在 2.x 里不会被加载，
//    所以单独打一份 V2 版本，由配置脚本按检测到的版本二选一下发。
const v2Result = await Bun.build({
  entrypoints: [join(pluginRoot, 'src', 'v2', 'index.ts')],
  target: 'node',
  format: 'esm',
})
if (!v2Result.success) {
  for (const log of v2Result.logs) console.error(log)
  throw new Error('bun build (v2) 失败')
}
const v2Code = await v2Result.outputs[0].text()
const v2JsPath = join(assetsDir, 'opencode-models-discovery-v2.js')
const v2VersionPath = join(assetsDir, 'opencode-models-discovery-v2.version')
await Bun.write(v2JsPath, v2Code)
await Bun.write(v2VersionPath, version + '\n')

console.log(`bundled v2 v${version} -> ${v2JsPath} (${(v2Code.length / 1024).toFixed(1)} KB)`)
console.log(`version asset -> ${v2VersionPath}`)