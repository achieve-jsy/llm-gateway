# Gemini 模型接入可用性测试报告

> **测试基准**：全部以服务器 `192.0.2.10`（`bfubuntu`，出口 IP `98.98.125.99`）发起。
> **测试目的**：确认 5 个 Gemini 模型是否全部可用；验证「白名单/IP 限制」与「403 aiplatform.endpoints.predict」的根因。
> **结论（TL;DR）**：**5 个模型全部真实可用**，**4 个 key（K1/K2/K3/K4）全部可调通**，诀窍是**每个 key 用它自己所属的 GCP 项目**（不是共用一个项目）。`403 aiplatform.endpoints.predict` = 调用项目与 key 所属项目不一致，**无需改 IAM**，把端点指到 key 自己的项目即可。K1 此前"不可用"也是因为拿它去测了其它项目；它在自己的项目 `793586848164` 上全通（且多 region 全通）。

---

## 1. 测试环境

| 项 | 值 |
|---|---|
| 测试机 | `bfubuntu`（`192.0.2.10`） |
| 测试机出口公网 IP | `98.98.125.99` |
| 本地对照出口 IP | `212.42.208.47` |
| 目标端点 | Vertex AI 原生端点 `https://aiplatform.googleapis.com/v1/projects/{PROJECT}/locations/asia-southeast1/publishers/google/models/{model}:generateContent` |
| 鉴权头 | `x-goog-api-key: <KEY>` |
| 请求体 | `{"contents":[{"role":"user","parts":[{"text":"Say OK"}]}]}` |
| 被测模型 | `gemini-3.5-flash`、`gemini-3.1-pro-preview`、`gemini-3-flash-preview`、`gemini-3.6-flash`、`gemini-3.7-flash` |

## 2. 各 key 的归属项目（关键，已全部定位）

| Key | 所属 GCP 项目 | 在**各自项目**上的表现（Vertex，`asia-southeast1`） |
|---|---|---|
| K1 | `793586848164` | ✅ 5 模型全 `200`（且 asia-northeast1/us-central1/europe-west1/us-east5 也全 `200`） |
| K2 | `791838655877` | ✅ 5 模型全 `200` |
| K3 | `251441475008` | ✅ 5 模型全 `200` |
| K4 | `439364517691` | ✅ 5 模型全 `200` |

> 各 key 归属项目来源：先用 Vertex/AI Studio 报错推断（AI Studio 的 `ErrorInfo.metadata.consumer` 直接给出项目号），再用「各自项目」实测验证（4 个 key 在各自项目上均 `200` 反证属实）。

## 3. 测试矩阵（**各自归属项目**，服务器侧，Vertex AI 原生端点）

| 模型 | K1（own `793586848164`） | K2（own `791838655877`） | K3（own `251441475008`） | K4（own `439364517691`） |
|---|---|---|---|---|
| gemini-3.5-flash | **200** ✅ | **200** ✅ | **200** ✅ | **200** ✅ |
| gemini-3.1-pro-preview | **200** ✅ | **200** ✅ | **200** ✅ | **200** ✅ |
| gemini-3-flash-preview | **200** ✅ | **200** ✅ | **200** ✅ | **200** ✅ |
| gemini-3.6-flash | **200** ✅ | **200** ✅ | **200** ✅ | **200** ✅ |
| gemini-3.7-flash | **200** ✅ | **200** ✅ | **200** ✅ | **200** ✅ |

- **K1/K2/K3/K4 → 5 模型全部 `HTTP 200 OK`**（返回真实内容 `text='OK'`）。

### 校准
- 假模型名 `gemini-999-flash`（K3，own 项目）→ `HTTP 404 NOT_FOUND`。
- 因此 5 个模型名均**真实存在**，`200` 不是端点兜底。

## 4. 根因分析（为什么之前 403，现在通了）

1. **`403 aiplatform.endpoints.predict` 的根因 = 调用项目与 key 归属项目不一致**。
   - 之前统一用 K3 的项目 `251441475008` 测所有 key → K1/K2/K4 在该项目上无 `aiplatform.endpoints.predict` 权限 → `403`。
   - 改成**每个 key 用自己的项目**后，**4 个 key 全部 `200`**。**无需任何 IAM 授权**。
   - K1 项目号定位方式：AI Studio 报错的 `ErrorInfo.metadata.consumer = projects/793586848164`。
2. **白名单/IP 限制**（仅 K1/K2 存在）：
   - 本地 `212.42.208.47`：K1 → `403 IP address restriction`（确实被白名单拦）。
   - 服务器 `98.98.125.99`：K1 **不再报 IP 限制**（服务器 IP 在白名单内）；之后之前失灵纯属项目不匹配。
3. **K1 的 AI Studio 报 `API_KEY_SERVICE_BLOCKED`**：这是该 key 在 `generativelanguage`（AI Studio）服务上被 block，**与 Vertex(aiplatform) 无关**——K1 在 Vertex 上、自己的项目里完全可用。**正确路径是 Vertex(aiplatform)，不要走 AI Studio。**

## 5. 结论

1. **5 个模型全部可用** —— 在各自的归属项目上，**K1/K2/K3/K4 对 5 个模型均 `HTTP 200`**；假模型名是 `404`，模型名真实。
2. **`403 aiplatform.endpoints.predict` 可解决** —— 解法：**每个 key 指向它自己所属的 GCP 项目**，无需改 IAM。
3. **白名单是真实限制**（K1/K2 本地被 IP 白名单拦截，服务器 IP 可通过）。
4. **K1 现已定位并可用**（项目 `793586848164`，Vertex 上 5 模型全通、多 region 全通）；其 AI Studio 被 block 属正常（该路径不采用）。

## 6. 建议

- **生产接入**：选用 **K1 / K2 / K3 / K4 中任一个**，并把网关 Vertex provider 的 endpoint（`projects/{PROJECT}/locations/...`）指向**该 key 自己的项目**。
  - K1 → `projects/793586848164/...`（region 最全）
  - K2 → `projects/791838655877/...`
  - K3 → `projects/251441475008/...`（**当前网关已配**）
  - K4 → `projects/439364517691/...`
- AI Studio（`generativelanguage`）路径对全部 key 均不可用/被禁，**不要走**。
- 每个 key 对应一个项目，**勿跨项目共用**，否则必然 `403 predict`。

## 7. 附录

### 7.1 测试命令（服务器侧）

```bash
EP="https://aiplatform.googleapis.com/v1/projects/{PROJECT}/locations/asia-southeast1/publishers/google/models"
BODY='{"contents":[{"role":"user","parts":[{"text":"Say OK"}]}]}'
curl -sS -X POST "$EP/{model}:generateContent" \
  -H "x-goog-api-key: <KEY>" -H 'Content-Type: application/json' -d "$BODY"
```

### 7.2 网关侧登记状态（本分支 `feat/gemini-models` 已做的前置工作）

- **Provider**：`vertex-gemini-e2e`
  - endpoint=`projects/439364517691/locations/asia-southeast1/publishers/google`（**当前用 K4**）
  - credentials=`api_key`=K4；`type=vertex`
- **模型组**：5 个都是「一组一模型」，组名=模型名（客户端按组名请求）：
  `gemini-3.5-flash` / `gemini-3.1-pro-preview` / `gemini-3-flash-preview` / `gemini-3.6-flash` / `gemini-3.7-flash`。
- **网关 E2E（本地，Key=client key，K4 项目）**：5 模型全 `OK`（`text='OK'`，`total_tokens` 32~98）。
- **部署约束**：网关跑在哪台机器，就要用那个出口 IP **不在白名单** 的 key。
  - 网关在**本地**（出口 `212.42.208.47`）：只能 K3 / K4（无 IP 限制），K1/K2 被 IP 白名单拦。
  - 网关上**服务器**（出口 `98.98.125.99`）：K1/K2/K3/K4 都可用（服务器 IP 在各 key 白名单内）。
- **代码修复**（`vertex.go`）：空 `auth_type` 自动判定为 `api_key`，且 `newProvider` 把解析出的 `authType=api_key` 透传给 gemini 子 provider（否则 gemini 默认 `gcp_adc`，不设 `x-goog-api-key`，被 Google 拒绝）。

### 7.3 密钥安全

本报告不写入密钥明文；测试用 Key 仅在运行时通过环境变量/参数传入，**禁止入库**。
