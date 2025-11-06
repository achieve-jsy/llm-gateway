import { defineComponent, h } from 'vue'

/**
 * 轻量级 Element Plus 桩组件，用于组件单元测试。
 * 仅保留足够的双向绑定/事件以验证字段交互与 payload 构造。
 */

const ElInput = defineComponent({
  name: 'ElInputStub',
  props: {
    modelValue: { type: [String, Number], default: '' },
    placeholder: { type: String, default: '' },
    type: { type: String, default: 'text' },
    disabled: { type: Boolean, default: false },
    showPassword: { type: Boolean, default: false },
    rows: { type: Number, default: 1 }
  },
  emits: ['update:modelValue', 'input', 'change', 'keyup'],
  setup(props, { emit, attrs }) {
    return () =>
      h('input', {
        class: 'el-input-stub',
        type: props.type === 'password' ? 'password' : props.type === 'textarea' ? 'text' : props.type,
        value: props.modelValue,
        placeholder: props.placeholder,
        disabled: props.disabled,
        ...attrs,
        onInput: (e: any) => {
          emit('update:modelValue', e.target.value)
          emit('input', e.target.value)
        },
        onChange: (e: any) => emit('change', e.target.value),
        onKeyup: (e: any) => emit('keyup', e)
      })
  }
})

const ElInputNumber = defineComponent({
  name: 'ElInputNumberStub',
  props: {
    modelValue: { type: Number, default: 0 },
    min: Number,
    max: Number,
    step: Number,
    precision: Number,
    disabled: Boolean
  },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit, attrs }) {
    return () =>
      h('input', {
        class: 'el-input-number-stub',
        type: 'number',
        value: props.modelValue,
        ...attrs,
        onInput: (e: any) => {
          const v = e.target.value === '' ? 0 : Number(e.target.value)
          emit('update:modelValue', v)
          emit('change', v)
        }
      })
  }
})

const ElSelect = defineComponent({
  name: 'ElSelectStub',
  props: {
    modelValue: { type: [String, Number, Array], default: '' },
    placeholder: String,
    multiple: Boolean,
    clearable: Boolean,
    collapseTags: Boolean,
    disabled: Boolean
  },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit, slots, attrs }) {
    return () =>
      h(
        'select',
        {
          class: 'el-select-stub',
          'data-multiple': props.multiple ? 'true' : 'false',
          value: props.multiple ? undefined : props.modelValue,
          multiple: props.multiple,
          ...attrs,
          onChange: (e: any) => {
            let v: any
            if (props.multiple) {
              const selected = e.target?.selectedOptions
              if (selected) {
                v = Array.from(selected).map((o: any) => o.value)
              } else {
                v = []
              }
            } else {
              v = e.target.value
            }
            emit('update:modelValue', v)
            emit('change', v)
          }
        },
        slots.default ? slots.default() : []
      )
  }
})

const ElOption = defineComponent({
  name: 'ElOptionStub',
  props: {
    value: { type: [String, Number], required: true },
    label: { type: [String, Number], default: '' },
    disabled: Boolean
  },
  setup(props, { slots }) {
    return () =>
      h(
        'option',
        { value: props.value, disabled: props.disabled },
        slots.default ? slots.default() : [String(props.label ?? props.value)]
      )
  }
})

const ElButton = defineComponent({
  name: 'ElButtonStub',
  props: {
    type: String,
    size: String,
    text: Boolean,
    loading: Boolean,
    disabled: Boolean
  },
  emits: ['click'],
  setup(props, { emit, slots, attrs }) {
    return () =>
      h(
        'button',
        {
          class: ['el-button-stub', props.type ? `is-${props.type}` : ''],
          disabled: props.disabled,
          'data-loading': props.loading ? 'true' : 'false',
          ...attrs,
          onClick: (e: any) => emit('click', e)
        },
        slots.default ? slots.default() : []
      )
  }
})

const ElCheckbox = defineComponent({
  name: 'ElCheckboxStub',
  props: {
    modelValue: { type: [Boolean, String, Number], default: false },
    value: { type: [String, Number, Boolean], default: undefined },
    label: { type: [String, Number, Boolean], default: undefined },
    indeterminate: Boolean,
    disabled: Boolean
  },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit, slots, attrs }) {
    return () =>
      h(
        'label',
        { class: 'el-checkbox-stub' },
        [
          h('input', {
            type: 'checkbox',
            checked: typeof props.modelValue === 'boolean' ? props.modelValue : false,
            disabled: props.disabled,
            'data-value': props.value,
            ...attrs,
            onChange: (e: any) => {
              emit('update:modelValue', e.target.checked)
              emit('change', e.target.checked)
            }
          }),
          slots.default ? slots.default() : []
        ]
      )
  }
})

const ElCheckboxGroup = defineComponent({
  name: 'ElCheckboxGroupStub',
  props: {
    modelValue: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue', 'change'],
  setup(_, { slots }) {
    return () => h('div', { class: 'el-checkbox-group-stub' }, slots.default ? slots.default() : [])
  }
})

const ElDialog = defineComponent({
  name: 'ElDialogStub',
  props: {
    modelValue: Boolean,
    title: String,
    width: [String, Number]
  },
  emits: ['update:modelValue', 'close'],
  setup(props, { slots }) {
    return () => {
      if (!props.modelValue) return null
      return h('div', { class: 'el-dialog-stub', 'data-title': props.title }, [
        h('div', { class: 'el-dialog__body' }, slots.default ? slots.default() : []),
        h('div', { class: 'el-dialog__footer' }, slots.footer ? slots.footer() : [])
      ])
    }
  }
})

const ElForm = defineComponent({
  name: 'ElFormStub',
  props: {
    model: Object,
    rules: Object
  },
  setup(_, { slots, expose }) {
    expose({
      validate: () => Promise.resolve(true),
      validateField: () => Promise.resolve(true),
      resetFields: () => undefined,
      clearValidate: () => undefined
    })
    return () => h('form', { class: 'el-form-stub' }, slots.default ? slots.default() : [])
  }
})

const ElFormItem = defineComponent({
  name: 'ElFormItemStub',
  props: {
    label: String,
    prop: String,
    rules: [Array, Object]
  },
  setup(props, { slots }) {
    return () =>
      h(
        'div',
        {
          class: 'el-form-item-stub',
          'data-label': props.label,
          'data-prop': props.prop
        },
        [
          h('div', { class: 'el-form-item__label' }, [props.label]),
          h('div', { class: 'el-form-item__content' }, slots.default ? slots.default() : [])
        ]
      )
  }
})

const ElTabs = defineComponent({
  name: 'ElTabsStub',
  props: {
    modelValue: String
  },
  emits: ['update:modelValue', 'tab-click'],
  setup(_, { slots }) {
    return () => h('div', { class: 'el-tabs-stub' }, slots.default ? slots.default() : [])
  }
})

const ElTabPane = defineComponent({
  name: 'ElTabPaneStub',
  props: {
    label: String,
    name: String
  },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'el-tab-pane-stub', 'data-label': props.label, 'data-name': props.name }, slots.default ? slots.default() : [])
  }
})

const ElTag = defineComponent({
  name: 'ElTagStub',
  props: { type: String, size: String, closable: Boolean, effect: String, round: Boolean },
  emits: ['close'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'span',
        { class: ['el-tag-stub', props.type ? `is-${props.type}` : ''], onClick: () => emit('close') },
        slots.default ? slots.default() : []
      )
  }
})

const ElPopconfirm = defineComponent({
  name: 'ElPopconfirmStub',
  props: { title: String },
  emits: ['confirm', 'cancel'],
  setup(_, { slots, emit, expose }) {
    expose({ confirm: () => emit('confirm') })
    return () =>
      h(
        'div',
        {
          class: 'el-popconfirm-stub',
          onClick: () => emit('confirm')
        },
        slots.reference ? slots.reference() : []
      )
  }
})

const ElTable = defineComponent({
  name: 'ElTableStub',
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    return () => {
      const rows = (props.data as any[]).map((row, idx) =>
        h(
          'tr',
          { class: 'el-table-row', 'data-row-index': idx },
          slots.default
            ? slots
                .default({ row, $index: idx })
                .filter(
                  (node: any) =>
                    node && node.type && (node.type as any).name?.includes('TableColumn')
                )
                .map((col: any) => {
                  if (col.children?.default) {
                    return h('td', col.children.default({ row, $index: idx }))
                  }
                  const propName = col.props?.prop
                  if (propName) {
                    return h('td', String(row[propName] ?? ''))
                  }
                  return h('td')
                })
            : []
        )
      )
      return h('table', { class: 'el-table-stub' }, [h('tbody', rows)])
    }
  }
})

const ElTableColumn = defineComponent({
  name: 'ElTableColumnStub',
  props: { label: String, prop: String, width: [String, Number], align: String, minWidth: [String, Number] },
  setup(_, { slots }) {
    return () => h('div', { class: 'el-table-column-stub' }, slots.default ? slots.default({}) : [])
  }
})

const ElPagination = defineComponent({
  name: 'ElPaginationStub',
  props: {
    currentPage: Number,
    pageSize: Number,
    total: Number,
    layout: String
  },
  emits: ['update:currentPage', 'current-change', 'size-change'],
  setup(props) {
    return () =>
      h('div', {
        class: 'el-pagination-stub',
        'data-current-page': props.currentPage,
        'data-page-size': props.pageSize,
        'data-total': props.total
      })
  }
})

const ElEmpty = defineComponent({
  name: 'ElEmptyStub',
  props: { description: String },
  setup(props, { slots }) {
    // 既支持 description prop（旧用法），也支持 #description / 默认 slot
    // （CatalogSyncPage 空状态把 CTA 按钮放在 #description slot 里，桩必须把它渲染出来，
    //   否则 data-testid="empty-cta-*" 在测试里永远找不到）。
    return () =>
      h(
        'div',
        { class: 'el-empty-stub' },
        [
          props.description,
          slots.description ? slots.description() : null,
          slots.default ? slots.default() : null
        ]
      )
  }
})

// ElAlert 桩：把 type/title/description 都摆到 DOM 上，方便测试用 data-* 或 text 断言。
// CatalogSyncPage 用它做同步状态横幅（修复 B）。
const ElAlert = defineComponent({
  name: 'ElAlertStub',
  props: {
    type: String,
    title: String,
    description: String,
    closable: { type: Boolean, default: true },
    showIcon: Boolean
  },
  setup(props, { slots }) {
    return () =>
      h(
        'div',
        {
          class: 'el-alert-stub',
          'data-type': props.type,
          'data-title': props.title,
          'data-description': props.description
        },
        [
          props.title,
          ' ',
          props.description,
          slots.default ? slots.default() : null
        ]
      )
  }
})

const ElDivider = defineComponent({
  name: 'ElDividerStub',
  props: { contentPosition: String },
  setup(_, { slots }) {
    return () => h('hr', { class: 'el-divider-stub' }, slots.default ? slots.default() : [])
  }
})

const ElIcon = defineComponent({
  name: 'ElIconStub',
  setup(_, { slots }) {
    return () => h('i', { class: 'el-icon-stub' }, slots.default ? slots.default() : [])
  }
})

const ElTooltip = defineComponent({
  name: 'ElTooltipStub',
  props: { content: String, placement: String },
  setup(_, { slots }) {
    return () => h('span', { class: 'el-tooltip-stub' }, slots.default ? slots.default() : [])
  }
})

const ElSwitch = defineComponent({
  name: 'ElSwitchStub',
  props: { modelValue: Boolean },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-switch-stub',
        type: 'checkbox',
        checked: props.modelValue,
        onChange: (e: any) => {
          emit('update:modelValue', e.target.checked)
          emit('change', e.target.checked)
        }
      })
  }
})

const ElColorPicker = defineComponent({
  name: 'ElColorPickerStub',
  props: { modelValue: String, showAlpha: Boolean },
  emits: ['update:modelValue', 'change', 'active-change'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-color-picker-stub',
        type: 'color',
        value: props.modelValue,
        'data-show-alpha': String(props.showAlpha ?? true),
        onInput: (e: any) => {
          emit('update:modelValue', e.target.value)
          emit('change', e.target.value)
        }
      })
  }
})

const ElSkeleton = defineComponent({
  name: 'ElSkeletonStub',
  setup() {
    return () => h('div', { class: 'el-skeleton-stub' })
  }
})

export const elementStubs = {
  ElInput,
  ElInputNumber,
  ElSelect,
  ElOption,
  ElButton,
  ElCheckbox,
  ElCheckboxGroup,
  ElDialog,
  ElForm,
  ElFormItem,
  ElTabs,
  ElTabPane,
  ElTag,
  ElPopconfirm,
  ElTable,
  ElTableColumn,
  ElPagination,
  ElEmpty,
  ElDivider,
  ElIcon,
  ElTooltip,
  ElSwitch,
  ElColorPicker,
  ElSkeleton
}

export const elementStubsKebab: Record<string, any> = {
  'el-input': ElInput,
  'el-input-number': ElInputNumber,
  'el-select': ElSelect,
  'el-option': ElOption,
  'el-button': ElButton,
  'el-checkbox': ElCheckbox,
  'el-checkbox-group': ElCheckboxGroup,
  'el-dialog': ElDialog,
  'el-form': ElForm,
  'el-form-item': ElFormItem,
  'el-tabs': ElTabs,
  'el-tab-pane': ElTabPane,
  'el-tag': ElTag,
  'el-popconfirm': ElPopconfirm,
  'el-table': ElTable,
  'el-table-column': ElTableColumn,
  'el-pagination': ElPagination,
  'el-empty': ElEmpty,
  'el-alert': ElAlert,
  'el-divider': ElDivider,
  'el-icon': ElIcon,
  'el-tooltip': ElTooltip,
  'el-switch': ElSwitch,
  'el-color-picker': ElColorPicker,
  'el-skeleton': ElSkeleton
}

export function findInputByLabel(wrapper: any, label: string) {
  const items = wrapper.findAll('.el-form-item-stub')
  const item = items.find((it: any) => it.attributes('data-label') === label)
  if (!item) return null
  return item.find('input, select, textarea')
}

export function findInputByProp(wrapper: any, prop: string) {
  const items = wrapper.findAll('.el-form-item-stub')
  const item = items.find((it: any) => it.attributes('data-prop') === prop)
  if (!item) return null
  return item.find('input, select, textarea')
}
