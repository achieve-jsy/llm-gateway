# 施小救性格标签系统 - 前端集成 Spec

## 任务概述

将"施小救"吉祥物的 9 张性格标签图片集成到用户用量页面（MyUsagePage.vue），根据用户的使用数据动态展示个性化标签和对应动作图。

---

## 第一部分：图片处理（去背景）

### 输入
- 位置：`web/public/images/mascot/`
- 文件：9 张 800x800 PNG 图片（白色背景）
  - volwang.png, shenye.png, shengqian.png, jiyou.png, zhuanyi.png
  - chenxing.png, xiaolv.png, sikao.png, default.png

### 要求
- 去除白色背景，生成带透明通道的 RGBA PNG
- 保持轮廓清晰，不要锯齿
- 输出覆盖原文件即可

### 推荐方案
```bash
# 方案1: Python rembg (如果可用)
from rembg import remove
from PIL import Image
img = Image.open('input.png')
out = remove(img)
out.save('output.png')

# 方案2: ImageMagick
convert input.png -fuzz 10% -transparent white output.png

# 方案3: 在线工具或 Photoshop
```

---

## 第二部分：前端集成

### 目标页面
`web/src/views/MyUsagePage.vue`（我的用量页）

### 新增组件
在页面顶部显眼位置新增"施小救の性格档案"卡片。

#### 布局设计
```vue
<el-card class="personality-card">
  <div class="personality-content">
    <div class="mascot-image">
      <img :src="currentTag.image" :alt="currentTag.label" />
    </div>
    <div class="personality-info">
      <h3>{{ currentTag.emoji }} {{ currentTag.label }}</h3>
      <p class="story">{{ currentTag.story }}</p>
      <div class="share-btn">
        <el-button type="primary" size="small">分享我的标签</el-button>
      </div>
    </div>
  </div>
</el-card>
```

#### 样式要求
- 卡片宽度 100%，高度自适应
- 图片尺寸：300x300px（响应式，移动端缩小）
- 使用 Element Plus 的 el-card 组件
- 渐变背景色（浅色，与标签主题呼应）

---

## 第三部分：标签判定逻辑

### 数据结构
```typescript
interface PersonalityTag {
  id: string;           // volwang, shenye, shengqian...
  emoji: string;        // ⚡, 🌙, 💎...
  label: string;        // 卷王本王, 深夜哲学家...
  image: string;        // /images/mascot/volwang.png
  story: string;        // 动态数据故事文案
  shareText: string;    // 分享文案
}
```

### 判定规则（优先级从高到低）

```typescript
function determineTag(userData): PersonalityTag {
  // 1. 卷王本王: 日均调用量 ≥ 全站中位数 × 2.5
  if (userData.dailyAvgCalls >= siteMedian * 2.5) {
    return {
      id: 'volwang',
      emoji: '⚡',
      label: '卷王本王',
      image: '/images/mascot/volwang.png',
      story: `你最近7天日均调用${userData.dailyAvgCalls}次，超过全站${userData.percentile}%的同事`,
      shareText: `我是AiMeter卷王本王，日均${userData.dailyAvgCalls}次调用，谁与争锋？`
    };
  }

  // 2. 省钱天才: 缓存命中率 ≥ 25%
  if (userData.cacheHitPct >= 25) {
    return {
      id: 'shengqian',
      emoji: '💎',
      label: '省钱天才',
      image: '/images/mascot/shengqian.png',
      story: `你的缓存命中率达到${userData.cacheHitPct}%，相当于省了¥${userData.savedCost}`,
      shareText: `缓存命中率${userData.cacheHitPct}%，AiMeter省钱天才非我莫属。`
    };
  }

  // 3. 效率怪物: 输入输出比 ≥ 1:4
  if (userData.inputOutputRatio >= 4) {
    return {
      id: 'xiaolv',
      emoji: '🎯',
      label: '效率怪物',
      image: '/images/mascot/xiaolv.png',
      story: `你的输入输出比为1:${userData.inputOutputRatio.toFixed(1)}，用最少的prompt产出最多的结果`,
      shareText: `1:${userData.inputOutputRatio.toFixed(1)}的输入输出比，效率怪物的prompt从不多写一个字。`
    };
  }

  // 4. 深夜哲学家: peakHour 在 22:00 - 04:00
  if (userData.peakHour >= 22 || userData.peakHour <= 4) {
    return {
      id: 'shenye',
      emoji: '🌙',
      label: '深夜哲学家',
      image: '/images/mascot/shenye.png',
      story: `你最活跃的时间段是${userData.peakHour}点，贡献了${userData.nightPct}%的调用`,
      shareText: `凌晨${userData.peakHour}点的AiMeter是我的主场，深夜哲学家在此。`
    };
  }

  // 5. 晨型打工人: peakHour 在 06:00 - 09:00
  if (userData.peakHour >= 6 && userData.peakHour <= 9) {
    return {
      id: 'chenxing',
      emoji: '🌅',
      label: '晨型打工人',
      image: '/images/mascot/chenxing.png',
      story: `你每天${userData.peakHour}点就开始调用，比${userData.earlyPct}%的同事都早`,
      shareText: `每天早上${userData.peakHour}点准时开干，晨型打工人的自我修养。`
    };
  }

  // 6. 模型集邮癖: 使用过的不同模型数量 ≥ 5
  if (userData.uniqueModelCount >= 5) {
    return {
      id: 'jiyou',
      emoji: '🦄',
      label: '模型集邮癖',
      image: '/images/mascot/jiyou.png',
      story: `你用过${userData.uniqueModelCount}种不同模型，是全站最博爱的探索者`,
      shareText: `我用过${userData.uniqueModelCount}种模型！集邮癖的快乐你不懂。`
    };
  }

  // 7. 专一战士: top1模型调用占比 ≥ 85%
  if (userData.topModelPct >= 85) {
    return {
      id: 'zhuanyi',
      emoji: '💕',
      label: '专一战士',
      image: '/images/mascot/zhuanyi.png',
      story: `你对${userData.topModel}的爱高达${userData.topModelPct}%，真爱无疑`,
      shareText: `${userData.topModel}就是我的唯一，${userData.topModelPct}%的调用都是它，专一战士！`
    };
  }

  // 8. 深度思考家: reasoning_effort=high 占比 > 30%
  if (userData.highReasoningPct > 30) {
    return {
      id: 'sikao',
      emoji: '🧠',
      label: '深度思考家',
      image: '/images/mascot/sikao.png',
      story: `你有${userData.highReasoningPct}%的调用使用了深度推理模式`,
      shareText: `${userData.highReasoningPct}%的调用都是深度推理，思考家的大脑永不停歇。`
    };
  }

  // 0. 默认: 萌新探索者
  return {
    id: 'default',
    emoji: '🌟',
    label: '萌新探索者',
    image: '/images/mascot/default.png',
    story: `你已使用AiMeter ${userData.daysActive}天，累计调用${userData.totalCalls}次`,
    shareText: `萌新探索者，正在解锁AiMeter的无限可能。`
  };
}
```

### 数据字段映射

根据 MyUsagePage.vue 现有数据结构：

| 现有字段 | 用途 | 标签判定 |
|---------|------|---------|
| `req_count` / 7天 | 日均调用量 | 卷王本王 |
| `cacheHitPct` | 缓存命中率 | 省钱天才 |
| `inputOutputRatio` | 输入输出比 | 效率怪物 |
| `peakHour` | 最活跃小时 | 深夜哲学家/晨型打工人 |
| 使用过的模型数量 | 从 heatmap 统计 | 模型集邮癖 |
| `topModel` + 占比 | 最爱模型 | 专一战士 |
| `reasoning_effort` 占比 | 深度推理比例 | 深度思考家 |

---

## 第四部分：社交分享功能

### 分享按钮点击后

```typescript
async function shareTag() {
  // 生成分享卡片（Canvas 或 HTML2Canvas）
  const canvas = document.createElement('canvas');
  const ctx = canvas.getContext('2d');
  
  // 绘制背景
  ctx.fillStyle = '#f0f9ff';
  ctx.fillRect(0, 0, 800, 600);
  
  // 绘制施小救图片
  const img = new Image();
  img.src = currentTag.value.image;
  await img.decode();
  ctx.drawImage(img, 50, 100, 400, 400);
  
  // 绘制标签文字
  ctx.font = 'bold 48px Arial';
  ctx.fillStyle = '#1f2937';
  ctx.fillText(`${currentTag.value.emoji} ${currentTag.value.label}`, 500, 150);
  
  ctx.font = '24px Arial';
  ctx.fillStyle = '#6b7280';
  wrapText(ctx, currentTag.value.story, 500, 220, 280, 30);
  
  // 绘制二维码（可选）
  // drawQRCode(ctx, 'https://aimeter.com/my/usage');
  
  // 导出图片
  canvas.toBlob((blob) => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `施小救-${currentTag.value.label}.png`;
    link.click();
  });
}
```

---

## 验收标准

1. ✅ 9 张图片全部去除白色背景，透明通道正常
2. ✅ MyUsagePage 页面顶部显示"施小救の性格档案"卡片
3. ✅ 根据用户数据动态显示正确的标签和图片
4. ✅ 数据故事文案正确填充
5. ✅ 点击分享按钮能下载分享卡片图片
6. ✅ 响应式布局，移动端正常显示
7. ✅ 代码无 lint 错误，提交到 feat/mascot-personality-tags 分支

---

## 交付物

- [ ] 9 张透明背景 PNG 图片
- [ ] MyUsagePage.vue 修改（新增性格档案卡片）
- [ ] 标签判定逻辑函数
- [ ] 分享卡片生成功能
- [ ] Git commit 到 feat/mascot-personality-tags 分支

---

## 注意事项

- 不要修改现有的 API 接口，使用现有数据字段
- 保持与现有页面风格一致（Element Plus + Vue3）
- 图片加载使用懒加载，避免首屏阻塞
- 分享功能优先支持下载，后续可扩展到微信/飞书分享
