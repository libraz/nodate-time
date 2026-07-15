import { readdir, readFile, stat } from 'node:fs/promises';
import { join } from 'node:path';

const assetsDirectory = join(import.meta.dir, '..', 'dist', 'assets');
const files = await readdir(assetsDirectory);
const mainChunk = files.find((file) => /^index-[\w-]+\.js$/.test(file));

if (!mainChunk) {
  throw new Error('Unable to find the production entry chunk');
}

const mainBytes = (await stat(join(assetsDirectory, mainChunk))).size;
const maxMainBytes = 350 * 1024;
if (mainBytes > maxMainBytes) {
  throw new Error(
    `Initial JavaScript chunk is ${(mainBytes / 1024).toFixed(1)} KiB; budget is ${maxMainBytes / 1024} KiB`,
  );
}

const html = await readFile(join(import.meta.dir, '..', 'dist', 'index.html'), 'utf8');
if (/modulepreload[^>]+src-[\w-]+\.js/.test(html)) {
  throw new Error('The optional holiday-data chunk must not be preloaded');
}

console.log(`Bundle budget passed: ${mainChunk} ${(mainBytes / 1024).toFixed(1)} KiB`);
