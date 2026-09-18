import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    // dist/.gitkeep keeps the Go embed happy without a build, so don't wipe the directory.
    emptyOutDir: false,
    target: 'es2023',
  },
  server: {
    proxy: { '/api': 'http://localhost:8080' },
  },
});
