---
name: blue-green-deploy
description: 蓝绿部署运维与排障指南（LLM Gateway，test 与 prod 共用 deploy-blue-green.sh）。改动/调试蓝绿部署、运行或排查 deploy-app-test / rollback-test、首次割接、按颜色切流量回滚、改 upstream-active / .active-color、新机器一次性接入蓝绿、或遇到蓝绿割接失败/卡住/端口冲突时使用。含本次改造踩坑清单（wget 健康检查误判、脏状态端口冲突、STOP_TIMEOUT、含空格 env sourcing、SSH 并发 255）。
---

# 蓝绿部署运维 (blue-green-deploy)

> 适用 LLM Gateway 的蓝绿部署。脚本 `deploy/blue-green/deploy-blue-green.sh` **test / prod 共用**，靠环境变量区分。关联 skill：测试发布全流程 `test-deploy-ops`、变量规范 `test-env-config`、跑/查流水线 `gitlab-cicd`。

## 1. 核心模型

- 两套同构环境 **blue / green**，同一时刻只有一种颜色对外服务（active）。
- nginx 站点用 `include <NGINX_UPSTREAM>` 引入活跃 upstream；切流量 = 改这个 include 文件 + `nginx -s reload`（**原子、零断连**）。
- 活跃颜色记录在脚本同目录的 `.active-color`（**不在仓库**，主机生成；scp 部署文件不会覆盖它）。
- `network_mode: host`：端口直接占宿主机，**两种颜色端口必须错开**。

| | blue | green | nginx 对外 | redis DB(test) |
|---|---|---|---|---|
| gateway | 8080 | 8082 | 9080 | blue=0 / green=1 |
| control-plane | 8081 | 8083 | — | （prod blue=2 / green=3）|

## 2. 部署流程（脚本逻辑，始终「先起新、再停旧」）

```
active=读(.active-color 或探测运行中的颜色，都没有=none)
target = blue↔green 互换；首次 active=none → target=green（关键，见 §6）
1. start_env target（docker compose up 新颜色）
2. health_check target（curl GET /health、/healthz）
   失败 → 不切流量、拆掉坏掉的 target、退出（旧环境继续服务，零宕机）
3. switch_nginx target（写 upstream-active.conf + nginx -t + reload）
4. save_state target（写 .active-color）
5. 停旧：active=none → stop_legacy(按名停旧单体容器)；否则 drain 后 stop_env(旧颜色)
```

## 3. 关键参数（env，prod 默认值不变）

| 变量 | 默认(prod) | test 取值 | 说明 |
|---|---|---|---|
| `NGINX_UPSTREAM` | `/opt/llm-gateway/upstream-active.conf` | `/data/llm-gateway/upstream-active.conf` | nginx include 的活跃 upstream 文件 |
| `DRAIN_TIMEOUT` | 600 | 10 | 切流后等旧环境 SSE 自然结束的秒数 |
| `STOP_TIMEOUT` | 600 | 30 | drain 后停旧容器的超时（见 §7 gateway 不响应 SIGTERM）|
| `HEALTH_RETRIES`/`HEALTH_INTERVAL` | 30 / 3 | 40 / 3 | 健康检查重试 |
| `LEGACY_CONTAINERS` | `llm-gw-gateway llm-gw-control-plane` | 同左 | 首次割接要下线的旧单体容器 |
| `REDIS_DB` / `REDIS_DB_GREEN` | 2 / 3 | 0 / 1 | 两色用不同 redis DB |
| 各密钥 + `IMAGE_TAG`/`REGISTRY`/`APP_ENV` | — | 由 CI/CD 变量注入 | compose 插值用 |

## 4. 一次性主机准备（新机器接入蓝绿）

nginx 站点把内联 upstream 换成 include（**只动 upstream 定义，server 块不动**）：

```bash
# 备份后，把站点里的 upstream gateway{...} / upstream control_plane{...} 两段
# 替换成一行 include；server 块继续用 http://gateway、http://control_plane
cp /etc/nginx/sites-available/default /etc/nginx/sites-available/default.bak.$(date +%s)
# 例：原文件前 N 行是两个 upstream 定义 → 用 include 替换
#   include /data/llm-gateway/upstream-active.conf;
cp /data/llm-gateway/upstream-blue.conf /data/llm-gateway/upstream-active.conf  # 初始指向当前在跑的颜色
nginx -t && nginx -s reload
```

`upstream-active.conf` 内容由脚本 `switch_nginx` 自动改写（从 `upstream-blue.conf`/`upstream-green.conf` 拷贝），无需手动维护。

## 5. 手动操作（在目标主机 /data/llm-gateway 下）

### 健壮地加载密钥再运行（手动测试用）
docker inspect / 现有容器导出的 env **含空格未加引号**（`DATABASE_URL=host=... port=...`、`OAUTH_SCOPES=openid profile email`），**不能用 `. ./env` source**（空格后会被当命令执行、变量被截断）。用逐行 export：

```bash
cd /data/llm-gateway
while IFS= read -r line; do case "$line" in ""|\#*) continue;; esac; export "${line%%=*}=${line#*=}"; done < blue-green.env
export NGINX_UPSTREAM=/data/llm-gateway/upstream-active.conf DRAIN_TIMEOUT=10 STOP_TIMEOUT=30 \
       REDIS_DB=0 REDIS_DB_GREEN=1 APP_ENV=test REGISTRY=192.0.2.10:5000 IMAGE_TAG=test-<sha> \
       LEGACY_CONTAINERS="llm-gw-gateway llm-gw-control-plane"
bash deploy-blue-green.sh            # 正常部署到另一颜色
bash deploy-blue-green.sh --rollback # 回滚（见 §6 限制）
```

### 验证零宕机（割接全程不掉）
另开一条后台轮询，割接后看是否全程 200：

```bash
nohup bash -c 'for i in $(seq 1 160); do echo "$(date +%H:%M:%S) $(curl -s -o /dev/null -w %{http_code} --max-time 2 http://127.0.0.1:9080/health)"; sleep 0.4; done' > /tmp/bg_poll.log 2>&1 &
# 跑完后：
grep -vc " 200" /tmp/bg_poll.log   # 期望 0
```

### 查状态
```bash
cat /data/llm-gateway/.active-color
grep server /data/llm-gateway/upstream-active.conf
docker ps --format '{{.Names}}\t{{.Status}}' | grep llm
```

## 6. 回滚机制与限制（重要）

- `--rollback` = **把 nginx 切回另一种颜色**，要求**对端容器仍在运行**。
- 两种「新版本不可用」：
  1. **健康检查就没过** → 脚本根本没切流量，旧环境一直服务，**无需回滚**，自动安全。
  2. **健康过了、切了流量、之后才发现坏** → 仅在 `DRAIN_TIMEOUT` 窗口内旧颜色还活着，`--rollback` 才能纯切流量切回；**过了窗口旧颜色已被 stop_env 关停删除**，此时 `--rollback` 会因「对端不健康」失败 → 只能用**指定旧版本重新部署**回滚。
- 现状是「省资源（切完只剩一套）」换「回滚有时限」。如需「随时秒级切回」，需改成部署后保留旧颜色、下次部署再回收（代价：常驻两套、内存翻倍）——**当前刻意不做**。

## 7. 踩坑清单（本次改造实测，优先看这里）

1. **健康检查必须用 curl(GET)，不能用 `wget --spider`**
   - 容器内是 busybox wget（`--spider` 发 GET）→ 通过；脚本在**宿主机**是 GNU wget（`--spider` 发 **HEAD**），而 gin 的 `/health`、`/healthz` 只注册 GET，HEAD 返回 404 → **新环境明明健康却被误判失败**（120s 超时）。**疑似生产首次割接失败诱因之一**。已改为 `curl -fsS`。
2. **首次割接必须 none→green，不能 none→blue**
   - 旧单体容器占着 blue 端口 8080/8081；先起 green（8082/8083）不冲突 → 健康 → 切流 → 再停旧栈 = 零宕机。若先上 blue 会和旧栈撞端口。
3. **别中途 kill 脚本（会留脏状态）**
   - 脚本被打断后 green/blue 容器残留，`get_active_color` 会探测到残留颜色，导致下次选错 target、和仍在跑的环境**撞端口** → 健康检查假阳性/假阴性。处理：先回到干净起点——`docker compose -f docker-compose.{blue,green}.yml -p llm-{blue,green} down`（**down 也要先 export 密钥**否则插值报 `REDIS_PASSWORD required`）、`rm -f .active-color`、确认只剩该在跑的环境。
4. **gateway 不响应 SIGTERM，停旧靠 STOP_TIMEOUT 强杀**
   - green/blue compose 有 `stop_grace_period: 600s`，gateway 不优雅退出会等满 600s。务必设 `STOP_TIMEOUT`（test 30）让 `docker compose down --timeout` 到点强制回收。失败回收路径已硬编码 30s。
5. **SSH 并发 255 / 会话卡住**
   - 一条后台 ssh 卡在 `docker compose down --timeout 600` 会占着会话，新连接可能 255。处理：杀掉本地卡住的 ssh 进程释放会话，再新连过去 `docker rm -f` 目标容器；避免用宽泛 `pkill -f "compose.*down"`。
6. **含空格的 env 不能直接 source**（见 §5），`DATABASE_URL`/`OAUTH_SCOPES` 会被截断。CI 里没这问题（用 `export VAR='...'` 注入）。

## 8. CI 集成（.gitlab-ci.yml）

- `deploy-app-test`：scp `deploy/blue-green/*` 到测试机 → 在 ssh 块里 `export` 蓝绿参数与密钥 → `bash deploy-blue-green.sh`。`timeout: 15m`。
- `rollback-test`：手动 job，`bash deploy-blue-green.sh --rollback`（同样要 export 密钥，因为 stop_env 会跑 compose down 触发插值）。
- `verify-test`：**蓝绿感知**——ssh 读 `.active-color` 判断 active 颜色，校验对应端口（blue 8080/8081 或 green 8082/8083）+ 走 nginx 9080 冒烟。**不要写死 8080/8081**，否则 active=green 时误报失败。
- 触发 `changes` 含 `deploy/blue-green/**/*`。
- 密钥 export 块**不可删**（远程拿不到 GitLab 变量，删了 compose `${VAR:?}` 插值失败、容器起不来；详见 `test-env-config`）。

## 9. 关键文件

| 文件 | 职责 |
|---|---|
| `deploy/blue-green/deploy-blue-green.sh` | 蓝绿主脚本（test/prod 共用，env 区分）|
| `deploy/blue-green/docker-compose.{blue,green}.yml` | 两色服务定义（容器名 `llm-{blue,green}-*`、端口、`${VAR:?}` 必填项）|
| `deploy/blue-green/upstream-{blue,green}.conf` | 两色 upstream，被 `switch_nginx` 拷成 `upstream-active.conf` |
| `deploy/blue-green/upstream-active.conf` | 当前活跃 upstream（nginx include 目标，脚本自动改写）|
| `deploy/blue-green/.active-color` | 活跃颜色状态（主机生成，不在仓库）|
| `deploy/blue-green/nginx-prod.conf.example` | 生产 nginx 蓝绿配置示例 |
