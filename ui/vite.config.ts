import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'path';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: '../api-gateway/static',
    emptyOutDir: true,
    sourcemap: false,
    minify: 'esbuild',
    rollupOptions: {
      output: {
        manualChunks: {
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
          'radix-ui': [
            '@radix-ui/react-dialog',
            '@radix-ui/react-progress',
            '@radix-ui/react-scroll-area',
            '@radix-ui/react-select',
            '@radix-ui/react-separator',
            '@radix-ui/react-slot',
            '@radix-ui/react-tabs',
            '@radix-ui/react-tooltip',
          ],
          'icons': ['lucide-react'],
          'charts': ['recharts'],
        },
      },
    },
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    watch: {
      usePolling: true,
      interval: 1000,
    },
    proxy: {
      '/v1': process.env.VITE_PROXY_TARGET || 'http://arl-gateway:8080',
      '/health': process.env.VITE_PROXY_TARGET || 'http://arl-gateway:8080',
      '/api/metrics': process.env.VITE_PROXY_TARGET || 'http://arl-gateway:8080',
      '/metrics': process.env.VITE_PROXY_TARGET || 'http://arl-gateway:8080',
      '/ws': { target: process.env.VITE_PROXY_TARGET || 'http://arl-gateway:8080', ws: true },
    },
  },
});
