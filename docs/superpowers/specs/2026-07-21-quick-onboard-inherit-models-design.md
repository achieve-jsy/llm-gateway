# 快速接入：继承源渠道模型与价格

日期：2026-07-21

## 目标

「快速接入供应商」选择「继承配置」时，可勾选「同时继承模型与价格」（默认开），把源渠道全部模型（含已禁用）与价格带到新渠道；禁用模型在新渠道仍为禁用。

## 方案

前端拉取 `GET /models?provider_id=` 填入 onboard `models[]`；后端 `QuickOnboardModelInput` 增加可选 `enabled`（默认 true）。

## 非目标

不复制密钥；不新增独立 clone API。
