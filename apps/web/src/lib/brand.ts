import { type BrandAsset, resolveBrand } from './brand-rules';

/**
 * What this deployment calls itself, and its logo and icon if it has them.
 *
 * Compiled in rather than fetched. Both are on the login screen and in the
 * document, shown before the first request comes back, so values arriving over
 * the network would present the software's name and then replace it.
 */
const brand = resolveBrand(import.meta.env);

export const APP_NAME: string = brand.appName;
export const APP_LOGO: BrandAsset | undefined = brand.logo;
export const APP_ICON: BrandAsset | undefined = brand.icon;
