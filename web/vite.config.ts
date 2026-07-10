import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 独立 SPA 构建（不再是插件单 bundle）。
// dev 模式把 API / 认证 / 运行时资产代理到本地后端（默认 :8181）。
const BACKEND = process.env.STUDIO_BACKEND_URL || 'http://localhost:8181';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5174,
    proxy: {
      '/api': BACKEND,
      '/auth': BACKEND,
      '/assets-runtime': BACKEND,
    },
  },
  build: {
    outDir: 'dist',
  },
});
