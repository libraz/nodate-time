import { resolveBrand } from './brand-rules';

/**
 * What this deployment calls itself, and its logo if it has one.
 *
 * Compiled in rather than fetched. Both are on the login screen and in the
 * document, shown before the first request comes back, so values arriving over
 * the network would present the software's name and then replace it.
 */
const brand = resolveBrand(import.meta.env);

export const APP_NAME: string = brand.appName;
export const APP_LOGO_URL: string | undefined = brand.logoUrl;
