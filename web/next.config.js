/** @type {import('next').NextConfig} */
const nextConfig = {
  // Semi Design 需要 transpile 才能在 Turbopack 下正常工作
  transpilePackages: ['@douyinfe/semi-ui', '@douyinfe/semi-icons', '@douyinfe/semi-foundation'],
  // 允许 WireGuard 客户端访问 HMR
  allowedDevOrigins: ['10.10.0.2', '10.10.0.0/24'],
  async rewrites() {
    return [
      { source: '/v1/:path*',   destination: 'http://localhost:8080/v1/:path*' },
      { source: '/api/:path*',  destination: 'http://localhost:8081/api/:path*' },
      { source: '/auth/:path*', destination: 'http://localhost:8081/auth/:path*' },
    ]
  },
}

module.exports = nextConfig
