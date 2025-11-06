# ─── Stage 1: Build Vue frontend ─────────────────────────────────────────────
FROM node:20-alpine AS builder

WORKDIR /app
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts

COPY web/ .
RUN npm run build

# ─── Stage 2: Nginx runtime ─────────────────────────────────────────────────
FROM nginx:1.27-alpine

# 清除默认配置
RUN rm /etc/nginx/conf.d/default.conf

# 自定义 Nginx 配置（统一入口：前端 + 反代 gateway & control-plane）
COPY deploy/docker/nginx.conf /etc/nginx/conf.d/default.conf

# 前端静态文件
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
