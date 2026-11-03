/**
 * image-compress 单测。
 *
 * jsdom 没有 createImageBitmap / canvas 2D 上下文 / toBlob，这里在**本文件内**自行 stub
 * （不动全局 setup）：createImageBitmap 用 vi.stubGlobal 注入，canvas 的 getContext / toBlob
 * 直接打到 HTMLCanvasElement.prototype 上，并按用例替换行为。
 * 断言都落在真实产出上：缩放后的目标宽高、drawImage 的入参、产出文件的 name/type/size。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { compressImage, replaceImageExtension } from '../image-compress'

/** canvas 2D 上下文替身里的 drawImage，用来断言缩放后的绘制参数 */
const drawImage = vi.fn()
/** 每个用例会创建的 canvas（getContext 被调用时记录） */
let canvases: HTMLCanvasElement[] = []
/** 可替换的 toBlob 行为（默认吐 webp） */
let toBlobHandler: (
  canvas: HTMLCanvasElement,
  cb: BlobCallback,
  type?: string,
  quality?: number,
) => void

const createObjectURL = vi.fn(() => 'blob:mock-url')
const revokeObjectURL = vi.fn()

/** 造一个体积可控的 File */
function makeFile(name: string, type: string, bytes = 1024): File {
  return new File([new Uint8Array(bytes)], name, { type, lastModified: 1700000000000 })
}

/** 让 createImageBitmap 返回指定原始宽高 */
function stubBitmap(width: number, height: number) {
  const close = vi.fn()
  const bitmap = { width, height, close }
  vi.stubGlobal('createImageBitmap', vi.fn().mockResolvedValue(bitmap))
  return bitmap
}

beforeEach(() => {
  canvases = []
  drawImage.mockClear()
  createObjectURL.mockClear()
  revokeObjectURL.mockClear()

  // 默认「支持 webp」，并产出比原图小的 blob
  toBlobHandler = (_canvas, cb, type) => cb(new Blob([new Uint8Array(2000)], { type: type || 'image/webp' }))

  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockImplementation(function (
    this: HTMLCanvasElement,
  ) {
    canvases.push(this)
    return { drawImage } as unknown as CanvasRenderingContext2D
  })
  Object.defineProperty(HTMLCanvasElement.prototype, 'toBlob', {
    configurable: true,
    writable: true,
    value(this: HTMLCanvasElement, cb: BlobCallback, type?: string, quality?: number) {
      toBlobHandler(this, cb, type, quality)
    },
  })

  // jsdom 未实现 object URL
  ;(URL as unknown as Record<string, unknown>).createObjectURL = createObjectURL
  ;(URL as unknown as Record<string, unknown>).revokeObjectURL = revokeObjectURL
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('replaceImageExtension', () => {
  it('按产出 mime 改写扩展名，没有扩展名时补上', () => {
    expect(replaceImageExtension('screenshot.png', 'image/webp')).toBe('screenshot.webp')
    expect(replaceImageExtension('photo.jpeg', 'image/jpeg')).toBe('photo.jpg')
    expect(replaceImageExtension('shot', 'image/webp')).toBe('shot.webp')
  })
})

describe('compressImage', () => {
  it('宽度超过 maxWidth 时等比缩到 1600 并重编码为 webp', async () => {
    const bitmap = stubBitmap(3200, 1800)
    const source = makeFile('screenshot.png', 'image/png', 50_000)

    const result = await compressImage(source)

    // 真的按 2:1 缩放了：drawImage 的目标宽高 + canvas 尺寸 + 返回值都要对
    expect(drawImage).toHaveBeenCalledTimes(1)
    expect(drawImage).toHaveBeenCalledWith(bitmap, 0, 0, 1600, 900)
    expect(canvases).toHaveLength(1)
    expect(canvases[0].width).toBe(1600)
    expect(canvases[0].height).toBe(900)
    expect(result.width).toBe(1600)
    expect(result.height).toBe(900)

    // 产出的文件真的是新文件：mime / 扩展名 / 体积都来自 webp blob
    expect(result.file).not.toBe(source)
    expect(result.file.type).toBe('image/webp')
    expect(result.file.name).toBe('screenshot.webp')
    expect(result.file.size).toBe(2000)

    // bitmap 用完要释放
    expect(bitmap.close).toHaveBeenCalled()
  })

  it('宽度未超过 maxWidth 时不做缩放、不重编码，原样返回', async () => {
    stubBitmap(800, 600)
    const source = makeFile('small.png', 'image/png', 4096)
    // 记录 toBlob 是否被碰到；即使被碰到也要回调，避免用例挂到超时才发现问题
    const spy = vi.fn()
    toBlobHandler = (_canvas, cb) => {
      spy()
      cb(new Blob([new Uint8Array(10)], { type: 'image/webp' }))
    }

    const result = await compressImage(source)

    expect(result.file).toBe(source)
    expect(result.width).toBe(800)
    expect(result.height).toBe(600)
    expect(canvases).toHaveLength(0)
    expect(spy).not.toHaveBeenCalled()
  })

  it('非图片文件原样返回，且不触碰 createImageBitmap', async () => {
    const bitmapSpy = vi.fn()
    vi.stubGlobal('createImageBitmap', bitmapSpy)
    const source = makeFile('notes.txt', 'text/plain', 32)

    const result = await compressImage(source)

    expect(result.file).toBe(source)
    expect(result.width).toBe(0)
    expect(result.height).toBe(0)
    expect(bitmapSpy).not.toHaveBeenCalled()
  })

  it('解码两条路径都失败时降级返回原文件，并释放回退用的 object URL', async () => {
    vi.stubGlobal('createImageBitmap', vi.fn().mockRejectedValue(new Error('decode failed')))
    // <img> 回退路径也失败（jsdom 不会真的加载图片，这里显式触发 error）
    class FailingImage {
      onload: (() => void) | null = null
      onerror: ((e?: unknown) => void) | null = null
      naturalWidth = 0
      naturalHeight = 0
      set src(_value: string) {
        queueMicrotask(() => this.onerror?.(new Error('image decode failed')))
      }
    }
    vi.stubGlobal('Image', FailingImage)

    const source = makeFile('broken.png', 'image/png', 2048)

    const result = await compressImage(source)

    expect(result.file).toBe(source)
    expect(result.width).toBe(0)
    expect(result.height).toBe(0)
    expect(createObjectURL).toHaveBeenCalledTimes(1)
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:mock-url')
  })

  it('webp 不被支持（toBlob 回调为 null）时回退 jpeg', async () => {
    stubBitmap(2000, 1000)
    const types: string[] = []
    toBlobHandler = (_canvas, cb, type) => {
      types.push(String(type))
      if (type === 'image/webp') {
        cb(null)
        return
      }
      cb(new Blob([new Uint8Array(1500)], { type: 'image/jpeg' }))
    }
    const source = makeFile('big.png', 'image/png', 9000)

    const result = await compressImage(source)

    expect(types).toEqual(['image/webp', 'image/jpeg'])
    expect(result.file.type).toBe('image/jpeg')
    expect(result.file.name).toBe('big.jpg')
    expect(result.width).toBe(1600)
    expect(result.height).toBe(800)
  })

  it('压缩结果反而更大时返回原文件', async () => {
    stubBitmap(3200, 1800)
    toBlobHandler = (_canvas, cb) => cb(new Blob([new Uint8Array(100_000)], { type: 'image/webp' }))
    const source = makeFile('shot.png', 'image/png', 5000)

    const result = await compressImage(source)

    expect(result.file).toBe(source)
    // 尺寸仍然报原图尺寸，便于调用方回传后端
    expect(result.width).toBe(3200)
    expect(result.height).toBe(1800)
  })

  it('maxWidth 可由调用方覆盖', async () => {
    stubBitmap(3000, 1500)

    const result = await compressImage(makeFile('wide.png', 'image/png', 50_000), { maxWidth: 1000 })

    expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 1000, 500)
    expect(result.width).toBe(1000)
    expect(result.height).toBe(500)
  })
})
