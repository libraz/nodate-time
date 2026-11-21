import { createRouter, RouterProvider } from '@tanstack/react-router';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { NotFoundPage, RouteErrorPage } from '@/components/route-fallback';
import { startBuildCheck } from '@/lib/build-check';
import { routeTree } from './routeTree.gen';
import '@/index.css';

const router = createRouter({
  routeTree,
  defaultNotFoundComponent: NotFoundPage,
  defaultErrorComponent: ({ reset }) => <RouteErrorPage reset={reset} />,
});

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

startBuildCheck();

const root = document.getElementById('root');
if (root) {
  createRoot(root).render(
    <StrictMode>
      <RouterProvider router={router} />
    </StrictMode>,
  );
}
