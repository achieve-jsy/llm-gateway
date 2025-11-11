-- Seed admin dev user (password: admin123)
-- bcrypt cost=12 hash of 'admin123'
INSERT INTO dev_users (username, password_hash, role) 
VALUES ('admin', '$2a$10$Kh0XlJtKND8uLXKLOd8dYOiAaVJz1ZwigAJpiXumSal1U9SfMGCbW', 'admin')
ON CONFLICT (username) DO NOTHING;
