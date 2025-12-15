---
name: test-env-config
description: 测试环境配置与变量注入强制规范。发布测试环境（push test 分支）、新增/修改测试环境配置项或密钥、改 .gitlab-ci.yml / docker-compose.yml / config/test.yaml、判断某个环境变量是否会生效时，必须走此 skill。核心铁律：测试环境的密钥/连接串一律放 GitLab CI/CD 变量（TEST_* 系列），由 CI 注入，禁止把明文写进仓库。
---

# 测试环境配置与变量注入规范 (test-env-config)

> **强制要求**：任何人（含 AI 助手）改动测试环境配置、新增变量、或发布到 test 分支前，**必须先读本 skill 并按其判定流程执行**。这是为了统一标准、避免「明文密钥回流仓库」和「以为变量直通 docker-compose 而漏注入」两类反复出现的错误。

## 0. 核心铁律（先记住这三条）

1. **密钥/连接串不写明文**：DB 连接串、Redis 密码、JWT_SECRET、ENCRYPTION_KEY、INTERNAL_TOKEN、OAuth client_id/secret 等，**只放 GitLab CI/CD 变量（`TEST_*`）**，仓库里（`config/test.yaml`、`docker-compose.yml`）一律留空或用强校验占位，**不得回填明文**。
2. **docker-compose 跑在远程测试主机，拿不到 Runner 变量**：`deploy-app-test` 通过 SSH 在 `192.0.2.10` 上执行 `docker compose`。GitLab CI/CD 变量**只存在于 Runner**，必须在 SSH 块里 `export 标准名='${TEST_xxx}'` 显式注入，远程的 `docker-compose.yml` 才读得到。**「直通 docker-compose」是错的——不 export 就拿不到。**
3. **非密钥的环境耦合值**（端口、公开 URL、issuer、scopes）可保留在 `docker-compose.yml` 默认值或 `config/test.yaml`，无需进 GitLab 变量。

## 1. 配置是怎么流到两个服务的（改之前必须搞清楚）

```
GitLab CI/CD 变量 (TEST_*)
   │  仅存在于 Runner
   ▼  deploy-app-test 的 ssh 块里  export 标准名='${TEST_xxx}'
远程测试主机的 shell 环境
   ▼  docker compose up（在 ${TEST_PATH} 内）
docker-compose.yml 的 environment（${VAR:?required} / ${VAR:-default}）
   ├─► gateway 容器      ← 走 Viper：config/test.yaml + 环境变量覆盖
   └─► control-plane 容器 ← 主要走 os.Getenv / Viper 环境变量绑定
```

| 服务 | 读取方式 | 关键代码 |
|------|----------|----------|
| **gateway** | Viper：读 `config/test.yaml`（镜像内置，build 时 `--build-arg APP_ENV=test`），**环境变量可覆盖**（`v.BindEnv`） | `internal/gateway/config/config.go` |
| **control-plane** | 主要 `os.Getenv` / Viper 环境变量绑定，**不依赖 test.yaml** | `cmd/control-plane/main.go`、`internal/controlplane/config/config.go`、各 `infra/*.go` |
| **migrate-test** | 用 `$TEST_DATABASE_URL`（URL 格式，gorm/atlas 都识别） | `.gitlab-ci.yml` migrate-test |

> 含义：要让 **gateway** 拿到某个值，env 名必须在 `internal/gateway/config/config.go` 里有 `BindEnv` 绑定（否则 Viper 不认）；**control-plane** 直接按 `os.Getenv("名字")` 取，名字必须对得上。

## 2. 当前测试环境变量清单（GitLab `TEST_*`）

| GitLab 变量 | 注入成的 env 名 | 消费方 | 是否密钥 |
|-------------|-----------------|--------|:--:|
| `TEST_DATABASE_URL` | `DATABASE_URL` | control-plane / migrate / gateway(可选) | ✅ |
| `TEST_REDIS_ADDR` | `REDIS_ADDR` | gateway / control-plane | ❌ |
| `TEST_REDIS_PASSWORD` | `REDIS_PASSWORD` | gateway / control-plane | ✅ |
| `TEST_REDIS_DB` | `REDIS_DB` | gateway / control-plane | ❌ |
| `TEST_JWT_SECRET` | `JWT_SECRET` | control-plane | ✅ |
| `TEST_INTERNAL_TOKEN` | `INTERNAL_AUTH_TOKEN`（+ gateway 的 `INTERNAL_TOKEN`、`AUTH_INTERNAL_TOKEN`） | gateway / control-plane | ✅ |
| `TEST_ENCRYPTION_KEY` | `ENCRYPTION_KEY` | control-plane | ✅ |
| `TEST_OAUTH_CLIENT_ID` | `OAUTH_CLIENT_ID` | control-plane | ✅ |
| `TEST_OAUTH_CLIENT_SECRET` | `OAUTH_CLIENT_SECRET` | control-plane | ✅ |

> 变量值不在本文件出现。增删改值用 `gitlab-cicd` skill（本地，含 token/PID）。**所有 `TEST_*` 必须 `protected=false`**：`test` 分支未受保护，protected 变量在 test 流水线会注入为空。

## 3. 新增 / 修改一个测试环境变量（标准 9 步）

> 加任何配置前，先按本节判断「会不会生效、要不要改代码」，再动手。

**第 1 步 · 定性**：它是密钥/环境耦合值吗？
- 是（密码、token、连接串、client secret）→ 走 GitLab `TEST_*` 变量，仓库里留空/强校验。
- 否（端口、公开 URL、issuer、scopes）→ 直接放 `docker-compose.yml` 默认值或 `config/test.yaml`，**不必**进 GitLab 变量。

**第 2 步 · 找消费方 & 确认读取方式**（决定 env 名 + 要不要改代码）：
- 给 **gateway** 用 → 打开 `internal/gateway/config/config.go`，确认 `Config` 结构体有对应字段、`SetDefault` 有默认、**`BindEnv("xxx.yyy","ENV_NAME")` 有绑定**。没有就**加代码**，否则环境变量不生效。
- 给 **control-plane** 用 → 搜它在哪 `os.Getenv("ENV_NAME")` 或 Viper `BindEnv`（`cmd/control-plane/main.go`、`internal/controlplane/`、`infra/*.go`）。env 名必须和取用处一致。

**第 3 步 · 建 GitLab 变量**：用 `gitlab-cicd` skill 新增 `TEST_<NAME>`，**`protected=false`、`environment_scope=*`**；含 `?`/空格/`_` 等字符的值 `masked=false`。

**第 4 步 · docker-compose.yml**：在对应服务的 `environment:` 加一行
- 密钥：`NAME: ${NAME:?NAME required (CI/CD 变量注入)}`（强校验，缺失即失败，不留明文默认）
- 非密钥：`NAME: ${NAME:-默认值}`

**第 5 步 · .gitlab-ci.yml `deploy-app-test`**：在 ssh 块的 `export ...` 区域加
`export NAME='${TEST_NAME}'`
（**这一步最容易漏——漏了远程 docker-compose 就拿不到，第 4 步的 `:?` 会直接失败**）

**第 6 步 · 迁移相关**：若该值是 DB 连接串等迁移要用的，确认 `migrate-test` 用的是 `$TEST_DATABASE_URL`。

**第 7 步 · config/test.yaml**：若是密钥且 gateway 经 Viper 读，对应项**留空 `""`**（靠 env 覆盖），**绝不写明文**；非密钥可正常写。

**第 8 步 · 本地编译校验**：改了 Go（如加 BindEnv）→ `go build ./...`。

**第 9 步 · 发布验证**：push `test` → 看流水线
- `deploy-app-test` 必须 `--wait` 健康检查通过（容器起得来=变量注入对了）
- `verify-test` 三项（gateway:8080 / control-plane:8081 / nginx:9080）全过

## 4. 「变量到底有没有生效」排查清单

变量没生效时，**从注入链路逐段查**：

- [ ] GitLab 里建了 `TEST_<NAME>` 且 **`protected=false`**？（test 未受保护，protected 会注入为空）—— 用 `gitlab-cicd` skill 查。
- [ ] `deploy-app-test` 的 ssh 块里 **export 了** `NAME='${TEST_NAME}'`？（漏 export = 远程拿不到）
- [ ] `docker-compose.yml` 对应服务 `environment` 里**透传了** `NAME`？
- [ ] 消费方读取方式对不对？
  - gateway：`internal/gateway/config/config.go` 有 `BindEnv` 绑定吗？
  - control-plane：`os.Getenv("NAME")` 的名字和注入名一致吗？
- [ ] 若 gateway 经 test.yaml：是不是被 test.yaml 里的**非空明文**盖住了？（应留空让 env 覆盖）
- [ ] 看容器日志：`docker logs llm-gw-control-plane --tail 50`（如 `JWT_SECRET missing`、`WRONGPASS`、`DATABASE_URL required`）。

## 5. 反模式（明确禁止，已发生过的错误）

- ❌ **把密钥明文写回 `config/test.yaml`**（"补全凭据使配置可直接使用"）。→ 等于把密钥重新提交进仓库；正确做法是留空 + GitLab 变量注入。
- ❌ **以为「GitLab 变量直通 docker-compose」而删掉 `deploy-app-test` 的 `export`**。→ docker-compose 跑在远程 SSH 主机，拿不到 Runner 变量，`${VAR:?}` 会失败、容器起不来。
- ❌ **docker-compose 用 `${VAR:-明文默认值}` 兜底密钥**。→ 明文进仓库。密钥统一 `${VAR:?required}`。
- ❌ **把 `TEST_*` 设成 `protected=true`**。→ test 分支未受保护，注入为空。
- ❌ 在 skill / 文档 / commit 里**回显密钥明文**。

## 6. 关联

- `gitlab-cicd`（本地 skill，含 token/PID）：增删改 GitLab CI/CD 变量、跑/查流水线。
- `test-deploy-ops`（本地 skill）：测试环境部署故障排查（容器 unhealthy、迁移、重启顺序）。
- `atlas-db-migration`：数据库迁移生成与多人并行规范。
- 代码锚点：`internal/gateway/config/config.go`、`cmd/control-plane/main.go`、`docker-compose.yml`、`.gitlab-ci.yml`（`migrate-test` / `deploy-app-test`）。

---

**一句话规范**：测试环境密钥 = GitLab `TEST_*` 变量（protected=false）→ `deploy-app-test` 的 ssh 块 `export` 注入 → `docker-compose.yml` 用 `${VAR:?required}` 读取；仓库零明文。新增变量必须把「建变量 / export / compose 透传 / 消费方读取绑定」四处都接上，缺一不生效。
