/**
 * 模型网关员工使用手册 —— 静态内容数据模型
 *
 * 内容来源：独立 HTML 手册页（index.html）。本文件是**第一方静态内容**，
 * 所有文案逐字取自源 HTML 正文，未做改写、精简或补充；正文中的强调标记
 * （em / strong / code / span class="ui"）按原样以内联 HTML 保留，
 * 渲染层直接使用 v-html 输出（内容可信，不存在用户输入注入风险）。
 *
 * 约定：
 * - ManualFigure.name 是不带扩展名的素材名，渲染时拼成 /manual/assets/<name>.webp；
 *   path 取自源 figure 的 data-path，alt 取自 img 的 alt，
 *   caption 取自源 figcaption 里 span 的文字（允许内联 HTML），
 *   width / height 取自源 img 的 width / height 属性。
 * - 源 HTML 中 <ol class="steps">"> 结尾多出的那个 "> 是源文件笔误，未作为正文迁移。
 * - 源里的提示块有两种视觉变体：.note.tip（绿色提示）与 .note（琥珀色警示）。
 *   冻结契约里只有 tip 字段，故两者的文案都放在 ManualStep.tip，
 *   渲染层如需还原琥珀色样式可按所在章节/步骤自行区分。
 */

/** 手册插图（截图） */
export interface ManualFigure {
  /** 不带扩展名的素材名，渲染时拼成 /manual/assets/<name>.webp */
  name: string
  /** 源 figure 的 data-path，用于截图卡片顶部的地址栏 */
  path: string
  /** 源 img 的 alt */
  alt: string
  /** 源 figcaption 里 span 的文字，允许内联 HTML */
  caption: string
  /** 源 img 的 width 属性 */
  width: number
  /** 源 img 的 height 属性 */
  height: number
}

/** 手册步骤 */
export interface ManualStep {
  /** 步骤标题（源 h4） */
  title: string
  /** 正文段落，每项一段（允许内联 HTML：em / strong / code / span class="ui"） */
  body: string[]
  /** 该步骤的单张截图 */
  figures: ManualFigure[]
  /** 源 div.duo：两张并排截图 */
  duo?: ManualFigure[]
  /** 提示/警示文案（源 .note / .note.tip，允许内联 HTML） */
  tip?: string
}

/** 章内小节（源 div.sub） */
export interface ManualSub {
  id: string
  title: string
  steps: ManualStep[]
}

/** 手册章节 */
export interface ManualChapter {
  id: string
  /** 源 span.chap-no，如「第 1 段」 */
  no: string
  title: string
  /** 源 .chap-head p */
  intro: string
  steps: ManualStep[]
  subs: ManualSub[]
}

/** 常见问题 */
export interface ManualFaq {
  q: string
  /** 答案段落，每项一段 */
  a: string[]
}

/** 右侧目录项 */
export interface ManualTocItem {
  id: string
  label: string
  /** 1 = 一级章节，2 = 章节内小节 */
  level: 1 | 2
}

/** 首屏快捷入口卡片 */
export interface ManualQuickNav {
  id: string
  title: string
  desc: string
  /** 源 p.n，如「2 步」 */
  count: string
  /** 源内联 SVG 字符串 */
  icon: string
}

/** 手册头部信息 */
export const MANUAL_META: {
  eyebrow: string
  title: string
  lede: string
  version: string
  updated: string
  loginUrl: string
  baseUrl: string
  readTime: string
} = {
  eyebrow: '面向全体员工 · 约 10 分钟读完',
  title: '模型网关员工使用手册',
  lede: '网关把公司采购的几个大模型统一到一个入口，用公司账号登录、拿一个 API Key 就能调用。这份手册按你实际会走的路子分四段：登录、建 Key、接工具、看用量和预算。',
  version: 'v1.0',
  updated: '2026 年 10 月',
  loginUrl: 'aimeter.xk-devops.com',
  baseUrl: 'https://aimeter.xk-devops.com/v1',
  readTime: '约 10 分钟读完',
}

/** 首屏快捷入口（源 .qn 的四张卡片） */
export const MANUAL_QUICK_NAV: ManualQuickNav[] = [
  {
    id: 'login',
    title: '系统登录',
    desc: '用大群账号走统一认证，不用单独注册。',
    count: '2 步',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M10 17l5-5-5-5"/><path d="M15 12H3"/><path d="M14 3h5a2 2 0 012 2v14a2 2 0 01-2 2h-5"/></svg>',
  },
  {
    id: 'key',
    title: '创建 API Key',
    desc: '起个名字、选模型组，拿到调用凭据。',
    count: '5 步',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="15" r="4"/><path d="M10.8 12.2L20 3"/><path d="M17 6l2 2"/><path d="M15 8l2 2"/></svg>',
  },
  {
    id: 'integrate',
    title: '配置与集成',
    desc: '复制一条命令，写进 OpenCode 配置。',
    count: '3 步',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3"/><path d="M13 15h4"/></svg>',
  },
  {
    id: 'usage',
    title: '用量与预算',
    desc: '查花费、翻调用明细、盯额度。',
    count: '7 步',
    icon: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20V10"/><path d="M10 20V4"/><path d="M16 20v-7"/><path d="M22 20H2"/></svg>',
  },
]

/** 右侧目录（顺序与源 aside.toc 的 ol 完全一致） */
export const MANUAL_TOC: ManualTocItem[] = [
  { id: 'login', label: '系统登录', level: 1 },
  { id: 'key', label: '创建 API Key', level: 1 },
  { id: 'integrate', label: '配置与集成', level: 1 },
  { id: 'usage', label: '用量与预算管理', level: 1 },
  { id: 'usage-detail', label: '我的用量', level: 2 },
  { id: 'budget', label: '我的预算', level: 2 },
  { id: 'models', label: '模型健康与模型广场', level: 2 },
  { id: 'faq', label: '常见问题', level: 1 },
]

/** 手册正文：4 章（usage 章带 3 个小节）；FAQ 单独走 MANUAL_FAQS */
export const MANUAL_CHAPTERS: ManualChapter[] = [
  {
    id: 'login',
    no: '第 1 段',
    title: '系统登录',
    intro: '网关接的是公司统一认证，账号就是你的大群账号。如果登录后提示未关联部门，找部门管理员把你加进组织架构。',
    steps: [
      {
        title: '打开登录页',
        body: [
          '浏览器访问 <code>https://aimeter.xk-devops.com/login</code>，会看到「欢迎登录某公司网络」页面。这里也是统一大模型接入管理平台的入口。',
        ],
        figures: [
          {
            name: 'login-entry',
            path: 'aimeter.xk-devops.com/login',
            alt: '登录页：左侧是某公司品牌图，右侧卡片里有绿色的大群登录按钮',
            caption: '点图中红框标出的<em>「大群登录」</em>按钮，走企业身份一键登录，不用手动输账号密码。',
            width: 2298,
            height: 1318,
          },
        ],
      },
      {
        title: '扫码或验证码验证',
        body: [
          '跳到「使用大群账号授权」页面后，右侧有两种方式：<strong>扫码登录</strong>——用大群 App 扫页面上的二维码；<strong>验证码登录</strong>——用手机号收验证码。手机上大群 App 已经登录着的话，直接点头像授权更快。',
          '验证通过后自动回到网关首页。左侧「员工」分组里就是后面会用到的菜单：<span class="ui">我的 Key</span>、<span class="ui">我的用量</span>、<span class="ui">模型广场</span>、<span class="ui">我的预算</span>、<span class="ui">模型组健康</span>。',
        ],
        figures: [
          {
            name: 'auth-method',
            path: '大群账号授权',
            alt: '大群账号授权页：左侧说明，右侧是扫码登录的二维码和头像',
            caption: '二维码和头像都在图中标注的位置，用大群 App 扫一下、手机上确认即可。',
            width: 2298,
            height: 1321,
          },
        ],
      },
    ],
    subs: [],
  },
  {
    id: 'key',
    no: '第 2 段',
    title: '创建 API Key',
    intro: 'Key 就是你在网关上的身份凭据，脚本、IDE 插件、Agent 工具都靠它取模型。建议按用途分开建，比如一个给 OpenCode、一个给 CI，用起来出问题也好定位。',
    steps: [
      {
        title: '进入「我的 Key」',
        body: [
          '左侧「员工」分组里点 <span class="ui">我的 Key</span>，进入密钥管理页。页面顶部是当前 BASE URL 和复制按钮，右上角是整页唯一的操作入口——<span class="ui">创建 Key</span>。',
          '第一次进来列表是空的，只有一句「暂无 API 密钥」；建过之后这里会列出所有 Key。',
        ],
        figures: [
          {
            name: 'key-entry',
            path: 'aimeter.xk-devops.com / 我的 Key',
            alt: '我的 Key 页面：空列表提示「暂无 API 密钥」，左侧导航的「我的 Key」被标注',
            caption: '图中标注的是左侧导航里的<em>「我的 Key」</em>入口，页面顶部能看到 BASE URL。',
            width: 2298,
            height: 1318,
          },
        ],
      },
      {
        title: '点「创建 Key」',
        body: [
          '列表里已有的 Key 会显示名称、模型组、过期时间和最后使用时间；右侧操作列有四个动作：<span class="ui">使用密钥</span>（查看调用示例）、<span class="ui">配置 OpenCode</span>、<span class="ui">重置</span>、<span class="ui">删除</span>。',
          '新建点右上角绿色的 <span class="ui">创建 Key</span>。',
        ],
        figures: [
          {
            name: 'key-list',
            path: '我的 Key / 创建 Key',
            alt: '我的 Key 列表：已有的 Key 与右上角的创建 Key 按钮',
            caption: '点图中标注的<em>「创建 Key」</em>打开新建弹窗。',
            width: 2298,
            height: 1321,
          },
        ],
      },
      {
        title: '给 Key 起个名字',
        body: [
          '弹窗里第一项是 <span class="ui">名称</span>，只给你自己识别用，不影响调用；只允许字母、数字、下划线和连字符，比如 <code>opencode-dev</code>。',
          '<span class="ui">过期时间</span>不填则永久有效，需要定期轮换的话在这里设。',
        ],
        figures: [
          {
            name: 'dialog-name',
            path: '我的 Key / 创建 API Key',
            alt: '创建 API Key 弹窗：名称输入框、过期时间、模型组',
            caption: '在图中标注的<em>名称</em>输入框里填名字。',
            width: 2298,
            height: 1318,
          },
        ],
      },
      {
        title: '选择这个 Key 能调用哪些模型',
        body: [
          '点 <span class="ui">模型组</span> 下拉框展开选项。想要全都能用就选「全部模型（不限制）」，下方会提示"该 Key 可访问全部模型，不限制"；想控成本或者只给某个项目用，就只勾需要的模型组。',
        ],
        figures: [],
        duo: [
          {
            name: 'dialog-group',
            path: '模型组 / 展开下拉框',
            alt: '创建 API Key 弹窗：模型组下拉框',
            caption: '',
            width: 2298,
            height: 1321,
          },
          {
            name: 'dialog-group-open',
            path: '模型组 / 选择「全部模型（不限制）」',
            alt: '模型组下拉列表展开：全部模型（不限制）与各模型组选项',
            caption: '',
            width: 2298,
            height: 1318,
          },
        ],
        tip: '不选任何模型组的话，这个 Key 建出来调不了模型。拿不准就选「全部模型（不限制）」，之后想收紧，重新建一个 Key 更省事。',
      },
      {
        title: '确认创建，把 Key 存好',
        body: [
          '点 <span class="ui">确认创建</span>，列表里立刻多出一行。创建完就把密钥复制下来存到安全的地方（密码管理器之类），别贴在聊天记录或代码仓库里。',
        ],
        figures: [
          {
            name: 'dialog-submit',
            path: '我的 Key / 创建 API Key',
            alt: '创建 API Key 弹窗：已填好名称和模型组，等待确认创建',
            caption: '名称和模型组都填好后，点图中标注的<em>确认创建</em>。',
            width: 2298,
            height: 1321,
          },
        ],
      },
    ],
    subs: [],
  },
  {
    id: 'integrate',
    no: '第 3 段',
    title: '配置与集成',
    intro: '网关内置了 OpenCode 的一键配置。不用手写配置文件，复制一条命令到终端执行就行；Windows 用户在弹窗里切到 PowerShell 页签拿对应的命令。',
    steps: [
      {
        title: '在 Key 列表点「配置 OpenCode」',
        body: [
          '回到 <span class="ui">我的 Key</span>，找到要用的那一行，点操作列里的 <span class="ui">配置 OpenCode</span>。每个 Key 都能单独配置，换 Key 重新走一遍就可以。',
        ],
        figures: [
          {
            name: 'key-row-opencode',
            path: '我的 Key / 配置 OpenCode',
            alt: '我的 Key 列表：某一行右侧的配置 OpenCode 按钮',
            caption: '点图中标注的<em>「配置 OpenCode」</em>打开配置弹窗。',
            width: 2298,
            height: 1318,
          },
        ],
      },
      {
        title: '复制配置命令',
        body: [
          '弹窗里先列出这次会写入的内容：写入位置 <code>~/.config/opencode/opencode.json</code>、Provider 是「某公司」、Base URL 和 API Key。选好系统类型，再点命令右侧的复制图标。',
        ],
        figures: [
          {
            name: 'opencode-dialog',
            path: '我的 Key / 配置 OpenCode',
            alt: '配置 OpenCode 弹窗：写入位置、Provider、Base URL、API Key 和 macOS/Linux 命令',
            caption: '点图中标注的<em>复制图标</em>，把整条命令复制到剪贴板。',
            width: 2298,
            height: 1321,
          },
        ],
      },
      {
        title: '在终端执行，再回来确认结果',
        body: [
          '把命令粘贴到终端执行，完成后回到弹窗点右下角的 <span class="ui">已复制，等待结果</span>。弹窗会停在「等待脚本执行…」，脚本执行完成后自动显示结果；如果命令还没跑，点「返回重新复制」再拿一次。',
          '写入成功之后，OpenCode 里就多出一个「某公司」Provider。',
        ],
        figures: [
          {
            name: 'opencode-copied',
            path: '我的 Key / 配置 OpenCode · 等待执行结果',
            alt: '配置 OpenCode 弹窗：等待脚本执行，脚本执行完成后将自动显示结果',
            caption: '注意<em>命令要真的执行完</em>再点这个按钮，否则检测不到写入结果。',
            width: 2298,
            height: 1318,
          },
        ],
        tip: '如果点完一直显示失败，先确认命令是在终端里成功执行过的（不是只粘贴没回车），再检查当前用户对 <code>~/.config/opencode/</code> 有没有写权限。',
      },
    ],
    subs: [],
  },
  {
    id: 'usage',
    no: '第 4 段',
    title: '用量与预算管理',
    intro: '这一段的入口都在左侧导航里。想确认「花了多少、哪个模型最费钱」，去 <span class="ui">我的用量</span>；想知道「这个月还剩多少额度」，去 <span class="ui">我的预算</span>；想比较有哪些模型可用、价格和速度如何，看 <span class="ui">模型广场</span> 和 <span class="ui">模型组健康</span>。',
    steps: [],
    subs: [
      {
        id: 'usage-detail',
        title: '我的用量',
        steps: [
          {
            title: '进入「我的用量」，先看花费总览',
            body: [
              '左侧点 <span class="ui">我的用量</span>。顶部可以按今天、近 3 天、近 7 天、近 14 天、近 30 天切换，也能自定义起止时间；下面还能按调用类型（Chat、Embedding、Rerank 等）和模型组、模型名、Key、状态过滤。',
              '四张卡片是这期间的花费、日均花费、总请求数和总 Token，再往下的「花费分布」和「花费排行」能看出钱主要花在哪些 Key 上。刚接入、还没有调用时这些数字都是 0。',
            ],
            figures: [
              {
                name: 'usage-overview',
                path: '员工 / 我的用量',
                alt: '我的用量页面：时间范围筛选、花费总览卡片与花费分布',
                caption: '图中标注的就是<em>「我的用量」</em>入口，在「员工」分组里；页面内用时间范围快捷选项切周期最省事。',
                width: 2298,
                height: 1318,
              },
            ],
          },
          {
            title: '在调用明细里逐条看',
            body: [
              '表格逐条记录每一次调用：KEY、KEY 类型、模型/路由、模型组、渠道、类型（同步还是流式）、端点、Token 用量（输入、缓存写入、输出）、状态、TTFT 和耗时。右上角的「导出」可以把当前筛选结果导成文件。',
              '点模型组名称（图中标注的 <code>deepseek-v4-flash-official</code>）可以打开这条请求的详情。',
            ],
            figures: [
              {
                name: 'usage-detail-table',
                path: '我的用量 / 调用明细',
                alt: '调用明细列表：每次调用的模型、渠道、Token 用量与耗时',
                caption: '点图中标注的<em>模型组名称</em>，打开这条请求的详情弹窗。',
                width: 2298,
                height: 1321,
              },
            ],
          },
          {
            title: '打开请求详情，看模型收到了什么',
            body: [
              '弹窗顶部是这次请求的 ID，下面是它的原始报文：Model、Provider，以及 SYSTEM、USER、ASSISTANT、TOOL 各段内容。排查「模型到底收到了什么」时看这里最直接——ASSISTANT 显示「（空）」表示这次没有返回内容。',
            ],
            figures: [
              {
                name: 'request-detail',
                path: '调用明细 / 请求详情',
                alt: '请求详情弹窗：请求 ID 与原始报文（SYSTEM、USER、ASSISTANT、TOOL）',
                caption: '「请求详情」里能看到<em>请求 ID</em> 和这次调用的原始报文。',
                width: 2298,
                height: 1318,
              },
            ],
          },
        ],
      },
      {
        id: 'budget',
        title: '我的预算',
        steps: [
          {
            title: '进入「我的预算」',
            body: [
              '左侧点 <span class="ui">我的预算</span>，默认停在「我的预算」页签。这一页只显示你的个人预算和额度来源——普通用户看不到部门额度和其他人的信息。',
              '额度不够时，点右上角的 <span class="ui">申请提升等级额度</span> 提交申请，审批通过后生效。',
            ],
            figures: [
              {
                name: 'budget-entry',
                path: '员工 / 我的预算',
                alt: '我的预算页面：三个页签与个人预算说明',
                caption: '图中标注的<em>「我的预算」</em>就是入口，进入后默认停在同名页签。',
                width: 2298,
                height: 1321,
              },
            ],
          },
          {
            title: '切到「模型等级额度」',
            body: [
              '页签切到 <span class="ui">模型等级额度</span>，可以按模型等级看额度：超智力模型、超级模型、高等级模型、中等模型、低等级模型。',
              '「正常」表示这个等级配了单独额度，能看到本周期额度和消耗进度；「未配置」表示仍受总预算约束、不单独计量。最右边一列是关联的模型组数量，点「查看全部」能看清单。',
            ],
            figures: [
              {
                name: 'budget-overview',
                path: '我的预算 / 模型等级额度',
                alt: '模型等级额度：按等级列出额度状态与关联模型组',
                caption: '切到<em>「模型等级额度」</em>页签后，可以按等级查看额度与关联模型组。',
                width: 2298,
                height: 1318,
              },
            ],
          },
          {
            title: '按日 / 周 / 月看剩余额度',
            body: [
              '右上角可以切 <span class="ui">日</span>、<span class="ui">周</span>、<span class="ui">月</span> 三种周期，看当前周期还能用多少；页面上的「当前周期」会跟着变（图中是"周"）。',
              '每个等级右侧的「申请提升」按钮只针对该等级的额度提交申请。',
            ],
            figures: [
              {
                name: 'budget-tier',
                path: '我的预算 / 模型等级额度',
                alt: '模型等级额度：五个等级的本周期额度、关联模型组与操作',
                caption: '图中标注的是周期切换，选<em>「周」</em>看本周额度。',
                width: 2298,
                height: 1321,
              },
            ],
          },
        ],
      },
      {
        id: 'models',
        title: '模型健康与模型广场',
        steps: [
          {
            title: '进「模型组健康」看整体可用性',
            body: [
              '左侧导航点 <span class="ui">模型组健康</span>。顶部可以切换「模型组视角 / 模型视角」，按名称搜索、按健康度筛选，回溯窗口选 1 小时、6 小时或 24 小时。',
              '刚接入、还没有调用数据时这里是空的（图中的「暂无模型组性能数据」）。有流量之后，每一行是一个模型组，会显示健康度、延迟、TTFT、吞吐量（tok/s）和请求数，最右边是这段时间的可用性色带；点某一行可以展开看这一组的成功率、延迟分位数和输出速率。',
            ],
            figures: [
              {
                name: 'health-groups',
                path: '员工 / 模型组健康',
                alt: '模型组健康页面：模型组视角 / 模型视角切换与空数据提示',
                caption: '图中标注的<em>「模型组健康」</em>入口，在「员工」分组里。',
                width: 2298,
                height: 1321,
              },
            ],
            // 源 HTML 里这条 tip 写在 ol.steps 之后（小节末尾），契约中 ManualSub 没有 tip 字段，故挂在该小节唯一一步上
            tip: '挑模型先看 <span class="ui">模型广场</span>：每张卡片上有模型组名称、输入/输出价格、能力标签和上下文长度。代码里的 <code>model</code> 参数要填卡片上的<strong>模型组名称</strong>（如 <code>deepseek-v4-flash-official</code>），不要填供应商原始模型 ID——网关会自己挑后端渠道。',
          },
        ],
      },
    ],
  },
]

/**
 * 常见问题（源 .faq 的 7 组问答）
 *
 * 注意：FAQ 在源 HTML 里也是一个 section.chap（章头为「附」/「常见问题」/
 * 「用顺手之后最常被问到的几个。」）。按冻结契约 FAQ 不进 MANUAL_CHAPTERS、
 * 只走 MANUAL_FAQS，而 ManualFaq 只有 q / a 两个字段，故该章头没有承载字段；
 * 渲染层如需还原，可在后续版本追加导出的 FAQ 章头元信息。
 */
export const MANUAL_FAQS: ManualFaq[] = [
  {
    q: '一个 Key 可以给多个工具用吗？',
    a: [
      '可以。网关不区分调用方，同一个 Key 放在 OpenCode、脚本或 CI 里都能用，调用地址统一是 <code>https://aimeter.xk-devops.com/v1</code>，格式兼容 OpenAI 的 Chat Completions 接口。更推荐按用途分开建——比如 <code>opencode-dev</code>、<code>ci-report</code>——这样出问题时能马上定位是哪个工具在用，也能单独重置或删掉其中一个。',
      '列表右侧的操作有：使用密钥（查看调用示例）、配置 OpenCode、重置、删除。',
    ],
  },
  {
    q: '有哪些模型可以用？代码里的 model 填什么？',
    a: [
      '左侧 <span class="ui">模型广场</span> 列出了全部可调用的模型组，每张卡片上有名称、输入/输出价格、能力标签和上下文长度。代码里的 <code>model</code> 参数填卡片上的模型组名称（如 <code>deepseek-v4-flash-official</code>），不要填供应商原始的模型 ID。',
      '想比较速度就看 <span class="ui">模型组健康</span>：TTFT 越低响应越快，吞吐量越高处理能力越强；实时对话这类对速度敏感的场景优先挑 TTFT 低的。',
    ],
  },
  {
    q: '创建 Key 时模型组没选，会怎样？',
    a: [
      '这个 Key 建出来调不了任何模型。创建时选「全部模型（不限制）」就都能用；只想开放部分模型，就按需要勾选。改权限需要重新建一个 Key。',
    ],
  },
  {
    q: '配置命令执行了，弹窗还提示失败？',
    a: [
      '先确认命令是真的在终端里跑完了（不是只粘贴、没回车），再回到弹窗点「已复制，等待结果」——这时弹窗会停在「等待脚本执行…」，执行完成后自动显示结果。如果你是事后才发现没跑，点「返回重新复制」再拿一次命令。仍然失败的话，检查当前用户对 <code>~/.config/opencode/</code> 有没有写权限；Windows 用户确认拿的是 PowerShell 页签里那条命令。',
    ],
  },
  {
    q: '费用是怎么算的？',
    a: [
      '每次调用按输入和输出分别计价，单价写在模型广场的卡片上；调用明细里能看到每次请求的 Token 用量（输入、缓存写入、输出分开列）。想省钱的两条经验：简单任务用 flash 一类的轻量模型，长对话注意别把没用的上下文一直带着。',
    ],
  },
  {
    q: '感觉某个模型变慢了，怎么换？',
    a: [
      '先去「模型组健康」看是不是上游在抖，再决定换不换。换的时候在「模型广场」找到目标模型组，把卡片上的名称复制过来替换代码里的 <code>model</code> 参数即可，其他代码不用动。',
    ],
  },
  {
    q: '登录后提示未关联部门 / 账号没有权限？',
    a: [
      '说明账号还没进组织架构，或者还没被分配 Key 和预算权限。联系部门管理员把你加到对应部门即可，加好之后重新登录一次。',
    ],
  },
]
