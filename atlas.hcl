data "external_schema" "gorm" {
  program = [
    "bash",
    "-c",
    "cd tools/atlas-loader && go run main.go"
  ]
}

env "local" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/16/dev?search_path=public"
  url = getenv("DATABASE_URL")
  migration {
    dir = "file://atlas-migrations"
  }
}

env "dev" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/16/dev?search_path=public"
  url = getenv("DATABASE_URL")
  migration {
    dir = "file://atlas-migrations"
  }
}

env "staging" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/16/dev?search_path=public"
  url = getenv("DATABASE_URL")
  migration {
    dir = "file://atlas-migrations"
  }
}

env "prod" {
  src = data.external_schema.gorm.url
  dev = "docker://postgres/16/dev?search_path=public"
  url = getenv("DATABASE_URL")
  migration {
    dir = "file://atlas-migrations"
  }
}
