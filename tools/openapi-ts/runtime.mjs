// Resolver shim for the openapi-typescript CLI.
// The CLI builds output with ts.factory, which only TypeScript 5.x ships; the
// workspace pins the native TypeScript 7. Route the bare `typescript` specifier
// to the `typescript-5x` alias and leave every other specifier alone.

import { createRequire, registerHooks } from 'node:module';
import { dirname, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const rootDir = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const require = createRequire(import.meta.url);

let entry;
try {
  entry = require.resolve('typescript-5x', { paths: [rootDir] });
} catch {
  throw new Error("openapi-typescript needs the 'typescript-5x' devDependency; run 'bun install'.");
}

const { version } = require(require.resolve('typescript-5x/package.json', { paths: [rootDir] }));
if (!version.startsWith('5.')) {
  throw new Error(`typescript-5x resolved to ${version}; the generator needs a 5.x release.`);
}

const typescriptUrl = pathToFileURL(entry).href;

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === 'typescript') {
      return { url: typescriptUrl, shortCircuit: true };
    }
    return nextResolve(specifier, context);
  },
});
