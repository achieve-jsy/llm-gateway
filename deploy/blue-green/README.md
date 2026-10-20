# 蓝绿切换与回滚剧本（调用链正文 `/datalog`）

> **本文件随发版一起落到宿主**：CI 的 `scp -r deploy/blue-green/*` 会把它拷到 `${DEPLOY_DIR}/README.md`，
> 因此运维在机器上（`cat ${DEPLOY_DIR}/README.md`）就能读到全部步骤 —— 不必回到 git 仓库。
>
> 适用前提：`deploy/blue-green/docker-compose.{blue,green}.yml` 已含 `/datalog` 的**长语法**挂载与
> per-color `TRACE_BODY_INST`；宿主 `trace-sealer.service` 已安装（`systemctl status trace-sealer`）。
> 容量口径（5 天保留量）与「撤掉挂载 ≤1 tick」的验证见 `docs/deployment.md` 的运行手册。
>
> `⛔` = 停止点：必须人工确认后才能继续。

## 0. 颜色 ↔ inst 的静态对应（整份剧本的前提）

| 颜色 | 编排文件 | `TRACE_BODY_INST` | 归档根 |
|---|---|---|---|
| blue | `deploy/blue-green/docker-compose.blue.yml` | `TRACE_BODY_INST=blue` | `/datalog/trace-archive-v4` |
| green | `deploy/blue-green/docker-compose.green.yml` | `TRACE_BODY_INST=green` | `/datalog/trace-archive-v4` |

- 对应关系是**静态**的：blue 文件 ⇒ `TRACE_BODY_INST=blue`；green 文件 ⇒ `TRACE_BODY_INST=green`。
  **重启后新颜色接管的仍是它自己的 inst** —— 颜色变了，inst 跟着编排文件走，不会漂移。
- `deploy/blue-green/deploy-blue-green.sh` **不需要任何改动**（它只管容器与 nginx，不认识任何
  `TRACE_BODY_` 键）。这条由变异集合的静态检查钉住：该脚本里不得出现这一族键名。
- inst 绝不来自 `config/prod.yaml`：全局 YAML 会让两个颜色写进同一个 `inst=` 目录 ⇒ part 序号与
  spool 互相踩。

## 1. 切换（发版到非活跃颜色）

按顺序执行（顺序本身是安全设计：先起新、再停旧，全程零宕机）：

1. **抄录切换前的状态**：`cat ${DEPLOY_DIR}/.active-color`，再 `docker ps --format '{{.Names}}'`。
2. **一条命令走完切换**：`./deploy-blue-green.sh`。它内部的固定次序是
   **启动新颜色 → `/health` + `/ready` → nginx 原子切流 → drain 旧颜色 → 停旧颜色**；
   切流前必须 `/health` 与 `/ready` 都通过（`READY_STABLE_CHECKS=3` 连续稳定）。
3. **抄录切换后的状态**：再 `cat ${DEPLOY_DIR}/.active-color`（切换后），
   并确认活跃容器名（`llm-blue-gateway` / `llm-green-gateway`）。
4. **切换期间的读侧口径**：切换期间两个 inst 并存 ⇒ 读侧必须用行上的 `body_ref_inst` 定位，
   绝不能用配置里的 `inst`（跨计划契约 H2 的落点）。旧颜色 drain 完成前旧 inst 仍可能有流量；
   配置里的 `inst` 只描述「当前活跃颜色」，用它定位会把旧 inst 的正文读成「找不到」。

## 2. 回滚

```bash
KEEP_OLD_ENV=true ./deploy-blue-green.sh --rollback   # nginx 切回上一个颜色
```

- 前提一：**旧容器仍在**（上次发版用了 `KEEP_OLD_ENV=true`，没有被 drain 停掉）。
- 前提二：**旧 inst 的数据仍在** —— 两个颜色共用同一个 root（`/datalog/trace-archive-v4`），新颜色
  只写自己的 `inst=` 目录，不会动旧 inst 的产物与 spool。
- **不需要重建任何数据**：读侧按行上的 `body_ref_inst` 定位，切回旧颜色后旧 inst 的行立刻可读。
- **sealer 必须继续运行**：功能回滚**不**停止 `trace-sealer.service`；未封的 spool 必须继续被封，
  否则已写出的 `body_ref_*` 会永久停在 spool 上（读侧永远点不动）。
- 回滚后核对：`systemctl is-active trace-sealer` 必须是 `active`；`cat ${DEPLOY_DIR}/.active-color`
  必须已回到旧颜色。

## 3. 功能降级（最轻的回滚：只关写侧，不动颜色）

1. 在每个颜色**各自的** compose 里把写侧开关 `TRACE_BODY_ROOT` 重新注释掉，然后重建容器：
   `docker compose -f ${DEPLOY_DIR}/docker-compose.<color>.yml -p llm-<color> up -d`。
   ⇒ 网关立刻回到「写 jsonb」模式（构造期的 root 存在性检查不满足 ⇒ 写侧 sink 为 nil）。
2. **已写出的 ref 不受影响**：它们指向的产物与 spool 都还在磁盘上，读侧仍按 `body_ref_inst` 定位。
   ⇒ 这是**事后补救只对之后的行生效**，不是「切流无损」。
3. **sealer 必须继续运行**，直到所有 spool 封完（`trace-sealer` 是独立进程，与功能开关无关）。
4. **逐字节等价的自证（可粘贴 SQL）**：功能关闭后新行必须与「从未开启过」逐字节相同 ——
   `body_ref_*` 四列全 NULL、`body_missing=false`、正文在 jsonb 列。把 `:off_since` 换成
   「关闭开关并重建容器的时刻」，下面三条查询都必须返回 **0**：

```sql
-- ① 期望 0：功能关闭后的行不得留下任何定位键
SELECT count(*) AS ref_rows
  FROM call_traces
 WHERE ts >= :off_since
   AND (body_ref_dt IS NOT NULL OR body_ref_inst IS NOT NULL
        OR body_ref_part IS NOT NULL OR body_ref_row IS NOT NULL);

-- ② 期望 0：功能关闭后不得出现写入期失败标记
SELECT count(*) AS missing_rows
  FROM call_traces
 WHERE ts >= :off_since
   AND body_missing IS TRUE;

-- ③ 期望 0：功能关闭后每一行都必须在 jsonb 列里有正文（不许只留 NULL 的壳）
SELECT count(*) AS jsonb_rows
  FROM call_traces
 WHERE ts >= :off_since
   AND request_body IS NULL AND response_body IS NULL;
```

## 4. 硬门：取消注释写侧开关之前（`⛔ 停止点 1`，D73）

**同一 root 的读侧可用之前，不得设置写侧开关**。取消注释 `TRACE_BODY_ROOT` 之前必须同时满足：

1. **① 同一 root 的读侧已可用**：读侧服务（计划 3）已落地，并且在 nginx 后面的管理面用一条真实
   `request_id` 点读成功（200 + 正文）。
2. **② 挂载正常**：`findmnt -T /datalog --output TARGET,SOURCE,FSTYPE` 的 `TARGET` 正好是 `/datalog`，
   且 `FSTYPE` 是真实文件系统（不是 `/`、不是容器层）。
3. **③ 容量足够**：`/datalog` 剩余空间 ≥ **5 天保留量**（口径见 `docs/deployment.md` 的运行手册）。

以上任一不满足 ⇒ `⛔` **停下来**，不得取消注释：取消注释是**开启静默丢正文**的唯一开关 ——
读侧未就绪时正文只进 `/datalog`，管理面与任何 API 都取不到（`body_ref_*` 有值、jsonb 列为 NULL）。

## 5. 旧颜色处置（drain 期间与 drain 之后）

- **drain 期间旧颜色功能仍是关闭的**：旧容器按上一版编排运行（没有 `/datalog` 挂载、没有写侧开关）
  ⇒ **正文继续进 jsonb** ⇒ **无数据损失**。这是「两个 inst 并存也安全」的另一半。
- drain 结束后旧容器被**停**（默认路径；`KEEP_OLD_ENV=true` 时保留供快速回滚）。
- 下一次发版会把旧颜色按**新编排**重建（那时它也有 `/datalog` 挂载与写侧开关）⇒
  **两个颜色最终都按新路径写**，各自的 `inst` 独立、互不干扰。

## 6. 禁止事项（逐条）

- 不得在两个颜色里设同一个 `inst`（两个写者会写进同一个 `inst=` 目录 ⇒ part 序号与 spool 互踩）。
- 不得把 `TRACE_BODY_*` 放进 `config/prod.yaml`（全局 YAML 会让两个颜色共用一个 inst）。
- 不得在功能开启后停止 sealer（未封的 spool 会永久停在磁盘上，`body_ref_*` 永远点不动）。
