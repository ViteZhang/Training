import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// 管理后台部署在 https://training.dreamerlab.cn/admin/，由流水线构建后同步到 Nginx 的 /admin/ 目录（T05）。
// 本地开发时 /api 代理到本机后端（make dev，:8080）；后端没起时设 VITE_API_PROXY=http://localhost:4010 用 mock。
export default defineConfig({
  base: '/admin/',
  plugins: [react()],
  server: {
    port: 5173,
    proxy: { '/api': { target: process.env.VITE_API_PROXY ?? 'http://localhost:8080', changeOrigin: true } },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    // antd 页面在 jsdom 里渲染慢，全量并行跑时单个用例常超过默认 5 秒；与 mobile 的 jest 保持一致。
    testTimeout: 20000,
  },
});
