/**
 * Writes text to the clipboard and reports whether it landed. The API is
 * missing outside secure contexts and rejects when permission is denied; a
 * caller that acknowledges the copy without checking tells the user a link is
 * on the clipboard when it is not.
 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (!navigator.clipboard) return false;
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
