FROM 192.0.2.10:5000/alpine:3.21 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && ln -snf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
    && echo "Asia/Shanghai" > /etc/timezone

COPY config/ /app/config/
COPY atlas-migrations/ /app/atlas-migrations/

WORKDIR /app

ARG APP_ENV=prod
ENV APP_ENV=${APP_ENV}
ENV TZ=Asia/Shanghai

FROM runtime AS gateway
COPY gateway /usr/local/bin/gateway
ENV GATEWAY_PORT=8080
EXPOSE 8080
ENTRYPOINT ["gateway"]

FROM runtime AS control-plane
COPY control-plane /usr/local/bin/control-plane
ENV CONTROL_PLANE_PORT=8081
EXPOSE 8081
ENTRYPOINT ["control-plane"]
