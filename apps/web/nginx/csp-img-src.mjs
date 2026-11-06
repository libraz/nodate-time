// Prints the nginx map that names the host the logo is loaded from, for the
// SPA's img-src. A data URI or no logo needs nothing beyond `data:`, so the
// map is empty then. The logo is validated here as well as in the Vite build.
// Usage: VITE_APP_LOGO_URL=... bun csp-img-src.mjs
import { logoImgSrc, resolveBrand } from '../src/lib/brand-rules.ts';

// biome-ignore lint/style/useNamingConvention: the build variable's name
const { logoUrl } = resolveBrand({ VITE_APP_LOGO_URL: process.env.VITE_APP_LOGO_URL });
process.stdout.write(`map $host $spa_logo_src {\n    default "${logoImgSrc(logoUrl)}";\n}\n`);
