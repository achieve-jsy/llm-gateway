variable "REGISTRY" {
  default = "192.0.2.10:5000"
}

variable "IMAGE_TAG" {
  default = "latest"
}

variable "APP_ENV" {
  default = "test"
}

group "default" {
  targets = ["gateway", "control-plane"]
}

target "backend" {
  context    = "."
  dockerfile = "deploy/docker/backend.Dockerfile"
  args = {
    APP_ENV = APP_ENV
  }
  cache-from = ["type=registry,ref=${REGISTRY}/llm-backend:build-cache"]
}

target "gateway" {
  inherits = ["backend"]
  target   = "gateway"
  tags     = ["${REGISTRY}/llm-gateway:${IMAGE_TAG}"]
  cache-to = ["type=registry,ref=${REGISTRY}/llm-backend:build-cache,mode=max"]
}

target "control-plane" {
  inherits = ["backend"]
  target   = "control-plane"
  tags     = ["${REGISTRY}/llm-control-plane:${IMAGE_TAG}"]
  # gateway target 已导出共享 builder/runtime 层，避免两个 exporter 竞争同一 cache ref。
  cache-to = []
}
