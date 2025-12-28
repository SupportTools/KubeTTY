import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../server/cmd/gateway/ui/dist',
    emptyOutDir: true,
    // ES2022 supports top-level await (required by noVNC library)
    target: 'es2022',
  },
  server: {
    proxy: {
      '/api': {
        target: 'https://kubetty-dev.support.tools',
        changeOrigin: true,
        secure: true,
      },
      '/ws': {
        target: 'https://kubetty-dev.support.tools',
        changeOrigin: true,
        secure: true,
        ws: true,
      },
    }
  }
});
