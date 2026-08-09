import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "../internal/web/web/dist", emptyOutDir: true },
  server: {
    host: "0.0.0.0",
    proxy: { "/api": "http://127.0.0.1:7376", "/openapi.json": "http://127.0.0.1:7376" },
  },
});
