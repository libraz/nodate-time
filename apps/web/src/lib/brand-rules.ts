/**
 * Rules for the deployment's name and images. Pure functions with no imports, so
 * the Vite config, the CSP generator (run by bun) and the bundle can all load
 * this one file. Callers pass the environment in.
 */

export const DEFAULT_APP_NAME = 'Nodate Time';

const MAX_NAME_CODE_POINTS = 64;
const MAX_DATA_URI_LENGTH = 32_768;

/** C0, DEL, C1, line/paragraph separators, bidi embeddings and isolates, and ':'. */
// biome-ignore lint/suspicious/noControlCharactersInRegex: matching control characters is the point
const FORBIDDEN_NAME_CHARS = /[\u0000-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069:]/;

export interface BrandAsset {
  src: string;
  darkSrc: string | undefined;
}

export interface Brand {
  appName: string;
  logo: BrandAsset | undefined;
  icon: BrandAsset | undefined;
}

function resolveName(raw: unknown): string {
  const name = typeof raw === 'string' ? raw.trim() : '';
  if (name === '') return DEFAULT_APP_NAME;
  if ([...name].length > MAX_NAME_CODE_POINTS) {
    throw new Error(`VITE_APP_NAME must be at most ${MAX_NAME_CODE_POINTS} characters`);
  }
  if (FORBIDDEN_NAME_CHARS.test(name)) {
    throw new Error(
      "VITE_APP_NAME must not contain control characters, bidirectional controls, line separators or ':'",
    );
  }
  return name;
}

const ASCII_WS_EDGES = /^[ \t\n\r\f\v]+|[ \t\n\r\f\v]+$/g;
const PRINTABLE_ASCII = /^[\x21-\x7E]+$/;
const DATA_IMAGE = /^data:image\/(?:png|svg\+xml|webp|jpeg|gif)[;,]/i;

/** Returns the trimmed value, or undefined when unset; throws naming the variable. */
function resolveImageUrl(name: string, raw: unknown): string | undefined {
  const value = typeof raw === 'string' ? raw.replace(ASCII_WS_EDGES, '') : '';
  if (value === '') return undefined;
  if (!PRINTABLE_ASCII.test(value)) {
    throw new Error(`${name} must contain only printable ASCII characters`);
  }
  const shape = `${name} must be an https URL or a data:image/(png|svg+xml|webp|jpeg|gif) URI`;
  if (isDataUri(value)) {
    if (!DATA_IMAGE.test(value)) throw new Error(shape);
    if (value.length > MAX_DATA_URI_LENGTH) {
      throw new Error(`${name} data URI must be at most ${MAX_DATA_URI_LENGTH} characters`);
    }
    return value;
  }
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error(shape);
  }
  if (url.protocol.toLowerCase() !== 'https:' || url.host === '') throw new Error(shape);
  return value;
}

function resolveAsset(base: string, dark: string, env: Record<string, unknown>) {
  const src = resolveImageUrl(base, env[base]);
  const darkSrc = resolveImageUrl(dark, env[dark]);
  if (src === undefined) {
    if (darkSrc !== undefined) throw new Error(`${dark} requires ${base}`);
    return undefined;
  }
  return { src, darkSrc };
}

/** True for a data: URI, compared case-insensitively. */
export function isDataUri(value: string): boolean {
  return value.slice(0, 5).toLowerCase() === 'data:';
}

/** Validates the build environment; throws an error naming the offending variable. */
export function resolveBrand(env: Record<string, unknown>): Brand {
  return {
    appName: resolveName(env.VITE_APP_NAME),
    logo: resolveAsset('VITE_APP_LOGO_URL', 'VITE_APP_LOGO_DARK_URL', env),
    icon: resolveAsset('VITE_APP_ICON_URL', 'VITE_APP_ICON_DARK_URL', env),
  };
}

/** The CSP img-src sources: distinct https origins of all four images, space-separated. */
export function imageOrigins(brand: Brand): string {
  const urls = [brand.logo?.src, brand.logo?.darkSrc, brand.icon?.src, brand.icon?.darkSrc];
  const origins = new Set<string>();
  for (const url of urls) {
    if (url && !isDataUri(url)) origins.add(new URL(url).origin);
  }
  return [...origins].join(' ');
}

/** Favicon links from the icon: the base first, then the dark-scheme variant. */
export function faviconTags(brand: Brand): { rel: 'icon'; href: string; media?: string }[] {
  const { icon } = brand;
  if (!icon) return [];
  const tags: { rel: 'icon'; href: string; media?: string }[] = [{ rel: 'icon', href: icon.src }];
  if (icon.darkSrc) {
    tags.push({ rel: 'icon', href: icon.darkSrc, media: '(prefers-color-scheme: dark)' });
  }
  return tags;
}
