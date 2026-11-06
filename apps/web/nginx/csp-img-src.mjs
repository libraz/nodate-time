// Prints the nginx map that names the hosts the brand images are loaded from,
// for the SPA's img-src. Data URIs and unset images need nothing beyond
// `data:`, so the map is empty then. The images are validated here as well as
// in the Vite build.
// Usage: VITE_APP_LOGO_URL=... VITE_APP_ICON_URL=... bun csp-img-src.mjs
import { imageOrigins, resolveBrand } from '../src/lib/brand-rules.ts';

const env = process.env;
const brand = resolveBrand({
  // biome-ignore lint/style/useNamingConvention: the build variable's name
  VITE_APP_LOGO_URL: env.VITE_APP_LOGO_URL,
  // biome-ignore lint/style/useNamingConvention: the build variable's name
  VITE_APP_LOGO_DARK_URL: env.VITE_APP_LOGO_DARK_URL,
  // biome-ignore lint/style/useNamingConvention: the build variable's name
  VITE_APP_ICON_URL: env.VITE_APP_ICON_URL,
  // biome-ignore lint/style/useNamingConvention: the build variable's name
  VITE_APP_ICON_DARK_URL: env.VITE_APP_ICON_DARK_URL,
});
process.stdout.write(`map $host $spa_brand_img_src {\n    default "${imageOrigins(brand)}";\n}\n`);
