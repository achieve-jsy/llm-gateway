CREATE TABLE IF NOT EXISTS departments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dept_id VARCHAR(64) NOT NULL UNIQUE,
    dept_name VARCHAR(128) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sub VARCHAR(128) NOT NULL UNIQUE,
    dept_id UUID REFERENCES departments(id) ON DELETE SET NULL ON UPDATE CASCADE,
    name VARCHAR(128) NOT NULL,
    email VARCHAR(256) NOT NULL,
    source VARCHAR(32) NOT NULL DEFAULT 'oidc',
    role VARCHAR(16) NOT NULL DEFAULT 'user',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_users_role CHECK (role IN ('admin', 'user')),
    CONSTRAINT chk_users_source CHECK (source = 'oidc')
);

CREATE INDEX IF NOT EXISTS idx_users_dept_id ON users (dept_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
