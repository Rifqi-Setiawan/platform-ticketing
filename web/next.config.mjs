/** @type {import('next').NextConfig} */
const nextConfig = {
  output: 'standalone',
  poweredByHeader: false,
  async rewrites() {
    const origin = process.env.API_ORIGIN || 'http://127.0.0.1:8080'
    return [{ source: '/v1/:path*', destination: origin + '/v1/:path*' }]
  },
}

export default nextConfig
