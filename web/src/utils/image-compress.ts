/**
 * 图片压缩工具（反馈贴图等「前端先压缩再上传」的场景）。
 *
 * 设计取舍：
 * - 只在**宽度超过 maxWidth** 时才走 canvas 重编码。宽度本来就够小的时候重编码只会掉画质、
 *   体积还可能变大，得不偿失。
 * - 优先用 `createImageBitmap`（无需解码成 DOM 节点、性能好、不受主线程限制），
 *   不可用或失败时回退 `Image` + `URL.createObjectURL`。
 * - 编码优先 `image/webp`（同画质体积最小），`toBlob` 回调拿到 null 表示环境不支持 webp，
 *   回退 `image/jpeg`。
 * - 任何异常都**降级返回原文件**，绝不把异常抛给调用方打断用户提交流程。
 */

/** compressImage 的可选项 */
export interface CompressImageOptions {
  /** 最大宽度，超过才缩放；默认 1600 */
  maxWidth?: number
  /** canvas 重编码质量 0~1，默认 0.85 */
  quality?: number
}

/** compressImage 的返回值 */
export interface CompressedImage {
  /** 压缩后的文件；无需压缩 / 降级时就是入参原文件 */
  file: File
  /** 产出图片宽度；降级且尺寸未知时为 0 */
  width: number
  /** 产出图片高度；降级且尺寸未知时为 0 */
  height: number
}

export const DEFAULT_MAX_WIDTH = 1600
export const DEFAULT_QUALITY = 0.85

/** mime → 扩展名（用于把文件名改成实际产出的类型） */
const EXTENSION_BY_MIME: Record<string, string> = {
  'image/webp': '.webp',
  'image/jpeg': '.jpg',
  'image/jpg': '.jpg',
  'image/png': '.png',
  'image/gif': '.gif',
}

/** 把文件名的扩展名替换成 mime 对应的扩展名，如 `shot.png` → `shot.webp` */
export function replaceImageExtension(name: string, mime: string): string {
  const ext = EXTENSION_BY_MIME[mime] || ''
  const base = (name || 'image').replace(/\.[^./\\]*$/, '') || 'image'
  return `${base}${ext}`
}

/** 已解码的图片来源（bitmap 或 <img>），release 负责释放底层资源 */
interface LoadedSource {
  source: CanvasImageSource
  width: number
  height: number
  release: () => void
}

/**
 * 用 createImageBitmap 拿原图（含原始宽高）；不可用/失败时回退 <img> + object URL。
 * @throws 两条路径都失败时抛出（由 compressImage 统一降级）
 */
async function loadSource(file: File): Promise<LoadedSource> {
  if (typeof createImageBitmap === 'function') {
    try {
      const bitmap = await createImageBitmap(file)
      return {
        source: bitmap,
        width: bitmap.width,
        height: bitmap.height,
        release: () => {
          try {
            bitmap.close?.()
          } catch {
            /* 释放失败不影响主流程 */
          }
        },
      }
    } catch {
      // 老浏览器或异常编码会走到这里，继续回退 <img>
    }
  }

  const objectUrl = URL.createObjectURL(file)
  try {
    const img = await loadImageElement(objectUrl)
    return {
      source: img,
      width: img.naturalWidth || img.width || 0,
      height: img.naturalHeight || img.height || 0,
      release: () => URL.revokeObjectURL(objectUrl),
    }
  } catch (e) {
    URL.revokeObjectURL(objectUrl)
    throw e
  }
}

/** 加载 <img>，失败时 reject（不抛同步异常） */
function loadImageElement(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error('image decode failed'))
    img.src = url
  })
}

/**
 * canvas → Blob，优先 webp，不支持（回调为 null）时回退 jpeg。
 * 全都拿不到 blob 时 resolve(null)，由调用方走降级分支。
 */
function canvasToBlob(canvas: HTMLCanvasElement, quality: number): Promise<Blob | null> {
  return new Promise((resolve) => {
    if (typeof canvas.toBlob !== 'function') {
      resolve(null)
      return
    }
    try {
      canvas.toBlob(
        (blob) => {
          if (blob) {
            resolve(blob)
            return
          }
          // webp 不被支持：回退 jpeg
          try {
            canvas.toBlob((jpeg) => resolve(jpeg), 'image/jpeg', quality)
          } catch {
            resolve(null)
          }
        },
        'image/webp',
        quality,
      )
    } catch {
      resolve(null)
    }
  })
}

/** 用压缩后的 blob 重建 File：类型取 blob 实际 mime，文件名扩展名同步改掉 */
function rebuildFile(original: File, blob: Blob): File {
  const mime = blob.type || 'image/webp'
  return new File([blob], replaceImageExtension(original.name, mime), {
    type: mime,
    lastModified: original.lastModified || Date.now(),
  })
}

/**
 * 压缩图片：宽度超过 maxWidth 时等比缩到 maxWidth 并重编码为 webp（回退 jpeg）。
 *
 * - 非图片（`!file.type.startsWith('image/')`）原样返回，类型校验交给调用方
 * - 宽度未超限不缩放、不重编码
 * - 压缩结果反而更大时返回原文件
 * - 任何异常都降级返回原文件
 */
export async function compressImage(
  file: File,
  opts: CompressImageOptions = {},
): Promise<CompressedImage> {
  const maxWidth = Number.isFinite(opts.maxWidth) && (opts.maxWidth as number) > 0
    ? (opts.maxWidth as number)
    : DEFAULT_MAX_WIDTH
  const quality = Number.isFinite(opts.quality) ? (opts.quality as number) : DEFAULT_QUALITY

  // 非图片：不处理，由调用方按白名单拦下
  if (!file || typeof file.type !== 'string' || !file.type.startsWith('image/')) {
    return { file, width: 0, height: 0 }
  }

  let loaded: LoadedSource | null = null
  try {
    loaded = await loadSource(file)
    const { source, width, height } = loaded

    // 尺寸未知或宽度未超限：不缩放，直接返回原文件
    if (!width || !height || width <= maxWidth) {
      return { file, width: width || 0, height: height || 0 }
    }

    const targetWidth = Math.max(1, Math.round(maxWidth))
    const targetHeight = Math.max(1, Math.round((height * targetWidth) / width))

    const canvas = document.createElement('canvas')
    canvas.width = targetWidth
    canvas.height = targetHeight
    const ctx = canvas.getContext('2d')
    if (!ctx) return { file, width, height }

    ctx.drawImage(source, 0, 0, targetWidth, targetHeight)
    const blob = await canvasToBlob(canvas, quality)

    // 没拿到 blob（环境不支持 canvas 编码）或压缩后反而更大：返回原文件
    if (!blob || blob.size >= file.size) return { file, width, height }

    return { file: rebuildFile(file, blob), width: targetWidth, height: targetHeight }
  } catch {
    return { file, width: 0, height: 0 }
  } finally {
    loaded?.release()
  }
}
