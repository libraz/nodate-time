/**
 * Starts a download by clicking a throwaway same-tab anchor. Unlike
 * `window.open` after an await, iOS Safari does not treat this as a popup.
 * Pass a URL minted immediately before the call.
 */
export function startDownload(url: string): void {
  const a = document.createElement('a');
  a.href = url;
  a.style.display = 'none';
  document.body.appendChild(a);
  a.click();
  a.remove();
}
