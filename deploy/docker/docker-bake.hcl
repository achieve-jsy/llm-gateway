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
}

target "gateway" {
  inherits = ["backend"]
  target   = "gateway"
  tags     = ["${REGISTRY}/llm-gateway:${IMAGE_TAG}"]
}

target "control-plane" {
  inherits = ["backend"]
  target   = "control-plane"
  tags     = ["${REGISTRY}/llm-control-plane:${IMAGE_TAG}"]
}
