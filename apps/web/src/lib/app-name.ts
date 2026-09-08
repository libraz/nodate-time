/**
 * What this deployment calls itself.
 *
 * Compiled in rather than fetched. The name is on the login screen and in the
 * document title, both of which are shown before the first request comes back,
 * so a name arriving over the network would present the software's name and
 * then replace it.
 *
 * The fallback is repeated in vite.config.ts, which substitutes the same name
 * into index.html at build time. index.html is not JavaScript and cannot read
 * this.
 */
export const APP_NAME: string = import.meta.env.VITE_APP_NAME?.trim() || 'Nodate Time';
