/**
 * Rules for the deployment's name and logo. Pure functions with no imports, so
 * the Vite config, the CSP generator (run by bun) and the bundle can all load
 * this one file. Callers pass the environment in.
 */

export const DEFAULT_APP_NAME = 'Nodate Time';

const MAX_NAME_CODE_POINTS = 64;
const MAX_DATA_URI_LENGTH = 32_768;
const DATA_URI_PREFIX = /^data:image\/(?:png|svg\+xml|webp|jpeg|gif)[;,]/;

/** C0, DEL, C1, line/paragraph separators, bidi embeddings and isolates, and ':'. */
// biome-ignore lint/suspicious/noControlCharactersInRegex: matching control characters is the point
const FORBIDDEN_NAME_CHARS = /[\u0000-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069:]/;

export interface Brand {
  appName: string;
  logoUrl: string | undefined;
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

function resolveLogo(raw: unknown): string | undefined {
  const logo = typeof raw === 'string' ? raw.trim() : '';
  if (logo === '') return undefined;
  if (DATA_URI_PREFIX.test(logo)) {
    if (logo.length > MAX_DATA_URI_LENGTH) {
      throw new Error(
        `VITE_APP_LOGO_URL as a data URI must be at most ${MAX_DATA_URI_LENGTH} characters`,
      );
    }
    return logo;
  }
  let url: URL;
  try {
    url = new URL(logo);
  } catch {
    throw new Error('VITE_APP_LOGO_URL must be an https URL or a data:image URI');
  }
  if (url.protocol !== 'https:') {
    throw new Error('VITE_APP_LOGO_URL must be an https URL or a data:image URI');
  }
  return logo;
}

/** Validates the build environment; throws an error naming the offending variable. */
export function resolveBrand(env: Record<string, unknown>): Brand {
  return {
    appName: resolveName(env.VITE_APP_NAME),
    logoUrl: resolveLogo(env.VITE_APP_LOGO_URL),
  };
}

/**
 * The CSP img-src source for a logo: the https origin, or '' for a data URI
 * or no logo (data: is already allowed).
 */
export function logoImgSrc(logoUrl: string | undefined): string {
  if (!logoUrl || logoUrl.startsWith('data:')) return '';
  return new URL(logoUrl).origin;
}
