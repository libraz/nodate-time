import { fileURLToPath, URL } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import { TanStackRouterVite } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';

const rootDir = fileURLToPath(new URL('.', import.meta.url));

/** Kept in step with src/lib/app-name.ts, which is where the bundle reads it. */
const DEFAULT_APP_NAME = 'Nodate Time';

/** The title is text and the name comes from configuration, not from here. */
function escapeHtml(value: string): string {
  return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export default defineConfig(({ mode }) => {
  // Resolved here as well as in the bundle because index.html is not
  // JavaScript: the tab and the title a crawler reads have to be right before
  // anything runs. An empty value falls back rather than blanking the title,
  // which is what an unset build argument produces.
  const appName = loadEnv(mode, rootDir, 'VITE_').VITE_APP_NAME?.trim() || DEFAULT_APP_NAME;

  return {
    plugins: [
      TanStackRouterVite({
        routesDirectory: './src/routes',
        generatedRouteTree: './src/routeTree.gen.ts',
        autoCodeSplitting: true,
        quoteStyle: 'single',
        semicolons: true,
      }),
      react(),
      tailwindcss(),
      {
        name: 'nodate-app-name',
        transformIndexHtml: (html: string) => html.replaceAll('%APP_NAME%', escapeHtml(appName)),
      },
    ],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: { port: 5173 },
  };
});
