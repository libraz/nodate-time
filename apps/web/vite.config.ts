import { fileURLToPath, URL } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import { TanStackRouterVite } from '@tanstack/router-plugin/vite';
import react from '@vitejs/plugin-react';
import { defineConfig, loadEnv } from 'vite';
import { resolveBrand } from './src/lib/brand-rules';

const rootDir = fileURLToPath(new URL('.', import.meta.url));

/** The title is text and the name comes from configuration, not from here. */
function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

export default defineConfig(({ mode }) => {
  // Resolved here as well as in the bundle because index.html is not
  // JavaScript: the tab and the title a crawler reads have to be right before
  // anything runs. An empty value falls back rather than blanking the title,
  // which is what an unset build argument produces; an invalid one fails the build.
  const { appName, logoUrl } = resolveBrand(loadEnv(mode, rootDir, 'VITE_'));

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
        transformIndexHtml: (html: string) => ({
          // A function replacer, so `$&` and friends in a name stay literal.
          html: html.replaceAll('%APP_NAME%', () => escapeHtml(appName)),
          tags: logoUrl
            ? [{ tag: 'link', attrs: { rel: 'icon', href: logoUrl }, injectTo: 'head' as const }]
            : [],
        }),
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
