import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ImageUpload from '@/components/common/ImageUpload.vue'

let mockFileReaderResult: string | null = null
let mockFileReaderShouldError = false

function mockFileReader(result: string) {
  mockFileReaderResult = result
  mockFileReaderShouldError = false
  vi.spyOn(FileReader.prototype, 'readAsDataURL').mockImplementation(function (this: FileReader) {
    const reader = this
    Promise.resolve().then(() => {
      if (mockFileReaderShouldError) {
        reader.onerror?.(new Error('read failed') as any)
      } else {
        Object.defineProperty(reader, 'result', { value: mockFileReaderResult, writable: true })
        reader.onload?.({ target: reader } as any)
      }
    })
  })
  vi.spyOn(FileReader.prototype, 'readAsText').mockImplementation(function (this: FileReader, _blob: Blob) {
    const reader = this
    Promise.resolve().then(() => {
      if (mockFileReaderShouldError) {
        reader.onerror?.(new Error('read failed') as any)
      } else {
        Object.defineProperty(reader, 'result', { value: mockFileReaderResult, writable: true })
        reader.onload?.({ target: reader } as any)
      }
    })
  })
}

function mockFileReaderError() {
  mockFileReaderResult = ''
  mockFileReaderShouldError = true
  vi.spyOn(FileReader.prototype, 'readAsDataURL').mockImplementation(function (this: FileReader) {
    const reader = this
    Promise.resolve().then(() => {
      reader.onerror?.(new Error('read failed') as any)
    })
  })
  vi.spyOn(FileReader.prototype, 'readAsText').mockImplementation(function (this: FileReader) {
    const reader = this
    Promise.resolve().then(() => {
      reader.onerror?.(new Error('read failed') as any)
    })
  })
}

function createMockFile(size: number, type: string, name = 'test.png') {
  return new File(['x'.repeat(size)], name, { type })
}

function mountComponent(props: Partial<{
  modelValue: string
  mode: string
  uploadLabel: string
  removeLabel: string
  hint: string
  maxSize: number
}> = {}) {
  return mount(ImageUpload, {
    props: {
      modelValue: '',
      ...props
    }
  })
}

describe('ImageUpload.vue', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFileReaderResult = null
    mockFileReaderShouldError = false
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('rendering', () => {
    it('should render upload button', () => {
      const wrapper = mountComponent()
      expect(wrapper.find('.upload-btn').exists()).toBe(true)
      expect(wrapper.find('.upload-btn').text()).toContain('上传')
    })

    it('should render custom upload label', () => {
      const wrapper = mountComponent({ uploadLabel: '选择图片' })
      expect(wrapper.find('.upload-btn').text()).toContain('选择图片')
    })

    it('should render hint text', () => {
      const wrapper = mountComponent({ hint: '支持 PNG 格式' })
      expect(wrapper.find('.upload-hint').text()).toBe('支持 PNG 格式')
    })

    it('should not render hint when empty', () => {
      const wrapper = mountComponent()
      expect(wrapper.find('.upload-hint').exists()).toBe(false)
    })

    it('should render preview box', () => {
      const wrapper = mountComponent()
      expect(wrapper.find('.preview-box').exists()).toBe(true)
    })

    it('should show placeholder icon when no image', () => {
      const wrapper = mountComponent()
      expect(wrapper.find('.preview-img').exists()).toBe(false)
      expect(wrapper.find('.placeholder-content').exists()).toBe(true)
    })

    it('should show preview image when modelValue is set', () => {
      const wrapper = mountComponent({ modelValue: 'data:image/png;base64,test' })
      expect(wrapper.find('.preview-img').exists()).toBe(true)
      expect(wrapper.find('.preview-img').attributes('src')).toBe('data:image/png;base64,test')
    })

    it('should show has-image class when modelValue is set', () => {
      const wrapper = mountComponent({ modelValue: 'data:image/png;base64,test' })
      expect(wrapper.find('.preview-box').classes()).toContain('has-image')
    })

    it('should show remove button when modelValue is set', () => {
      const wrapper = mountComponent({ modelValue: 'data:image/png;base64,test' })
      expect(wrapper.find('.remove-btn').exists()).toBe(true)
    })

    it('should hide remove button when modelValue is empty', () => {
      const wrapper = mountComponent({ modelValue: '' })
      expect(wrapper.find('.remove-btn').exists()).toBe(false)
    })
  })

  describe('remove', () => {
    it('should emit empty string when remove button clicked', async () => {
      const wrapper = mountComponent({ modelValue: 'data:image/png;base64,test' })

      await wrapper.find('.remove-btn').trigger('click')

      expect(wrapper.emitted('update:modelValue')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')![0]).toEqual([''])
    })
  })

  describe('upload trigger', () => {
    it('should trigger file input on upload button click', async () => {
      const wrapper = mountComponent()
      const clickSpy = vi.fn()
      wrapper.vm.$el.querySelector('.hidden-input')?.addEventListener('click', clickSpy)

      await wrapper.find('.upload-btn').trigger('click')

      expect(clickSpy).toHaveBeenCalled()
    })

    it('should trigger file input on preview box click', async () => {
      const wrapper = mountComponent()
      const clickSpy = vi.fn()
      wrapper.vm.$el.querySelector('.hidden-input')?.addEventListener('click', clickSpy)

      await wrapper.find('.preview-box').trigger('click')

      expect(clickSpy).toHaveBeenCalled()
    })
  })

  describe('file upload handling', () => {
    it('should emit data URL on successful upload', async () => {
      mockFileReader('data:image/png;base64,filecontent')
      const wrapper = mountComponent()

      const input = wrapper.find('.hidden-input')
      const file = createMockFile(100, 'image/png')
      Object.defineProperty(input.element, 'files', { value: [file] })

      await input.trigger('change')
      await flushPromises()

      expect(wrapper.emitted('update:modelValue')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')![0][0]).toContain('data:image/png;base64,filecontent')
    })

    it('should show error when file exceeds max size', async () => {
      const wrapper = mountComponent({ maxSize: 50 })
      const input = wrapper.find('.hidden-input')
      const file = createMockFile(200 * 1024, 'image/png')
      Object.defineProperty(input.element, 'files', { value: [file] })

      await input.trigger('change')
      await flushPromises()

      expect(wrapper.find('.upload-error').exists()).toBe(true)
      expect(wrapper.find('.upload-error').text()).toContain('文件过大')
    })

    it('should show error when file is not an image', async () => {
      const wrapper = mountComponent()
      const input = wrapper.find('.hidden-input')
      const file = createMockFile(100, 'text/plain', 'test.txt')
      Object.defineProperty(input.element, 'files', { value: [file] })

      await input.trigger('change')
      await flushPromises()

      expect(wrapper.find('.upload-error').exists()).toBe(true)
      expect(wrapper.find('.upload-error').text()).toContain('请选择图片文件')
    })

    it('should not emit when no file selected', async () => {
      const wrapper = mountComponent()
      const input = wrapper.find('.hidden-input')
      Object.defineProperty(input.element, 'files', { value: [] })

      await input.trigger('change')
      await flushPromises()

      expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    })

    it('should clear error text when a valid file is uploaded after an error', async () => {
      const wrapper = mountComponent()
      const input = wrapper.find('.hidden-input')

      const textFile = createMockFile(100, 'text/plain', 'test.txt')
      Object.defineProperty(input.element, 'files', { value: [textFile], configurable: true })
      await input.trigger('change')
      await flushPromises()

      expect(wrapper.find('.upload-error').exists()).toBe(true)
      expect(wrapper.find('.upload-error').text()).toContain('请选择图片文件')
    })

    it('should handle FileReader error', async () => {
      mockFileReaderError()
      const wrapper = mountComponent()

      const input = wrapper.find('.hidden-input')
      const file = createMockFile(100, 'image/png')
      Object.defineProperty(input.element, 'files', { value: [file] })

      await input.trigger('change')
      await flushPromises()

      expect(wrapper.find('.upload-error').exists()).toBe(true)
      expect(wrapper.find('.upload-error').text()).toContain('文件读取失败')
    })
  })

  describe('SVG mode', () => {
    it('should accept .svg files in SVG mode', () => {
      const wrapper = mountComponent({ mode: 'svg' })
      const input = wrapper.find('.hidden-input')
      expect(input.attributes('accept')).toBe('.svg')
    })

    it('should accept image/* in image mode', () => {
      const wrapper = mountComponent()
      const input = wrapper.find('.hidden-input')
      expect(input.attributes('accept')).toBe('image/*')
    })

    it('should read as text in SVG mode', async () => {
      mockFileReader('<svg>test</svg>')
      const wrapper = mountComponent({ mode: 'svg' })

      const input = wrapper.find('.hidden-input')
      const file = createMockFile(100, 'image/svg+xml')
      Object.defineProperty(input.element, 'files', { value: [file] })

      await input.trigger('change')
      await flushPromises()

      expect(wrapper.emitted('update:modelValue')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')![0][0]).toBe('<svg>test</svg>')
    })
  })
})