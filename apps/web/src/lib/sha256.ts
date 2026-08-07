/**
 * Incremental SHA-256 (FIPS 180-4).
 *
 * WebCrypto only digests a buffer handed to it whole, so hashing a file
 * through it means holding every byte in memory at once — up to the 100MB an
 * attachment is allowed to be, on whatever phone happens to be uploading it.
 * This takes the bytes a slice at a time instead, so the cost is one slice
 * rather than the whole file. Anything already resident in memory should go
 * through `crypto.subtle`, which is faster and saves nothing here.
 *
 * Words are read through DataViews rather than typed-array indexing: a
 * DataView read is typed as a number, where an indexed read is number |
 * undefined, and the rounds below would drown in checks for an out-of-range
 * index the loop bounds already rule out.
 */

const BLOCK_BYTES = 64;
const ROUNDS = 64;

/** The first 32 bits of the fractional parts of the cube roots of the first 64 primes. */
const K = new DataView(new ArrayBuffer(ROUNDS * 4));
for (const [i, word] of [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
].entries()) {
  K.setUint32(i * 4, word);
}

function rotr(x: number, n: number): number {
  return (x >>> n) | (x << (32 - n));
}

export class Sha256 {
  private h0 = 0x6a09e667;
  private h1 = 0xbb67ae85;
  private h2 = 0x3c6ef372;
  private h3 = 0xa54ff53a;
  private h4 = 0x510e527f;
  private h5 = 0x9b05688c;
  private h6 = 0x1f83d9ab;
  private h7 = 0x5be0cd19;

  /** Bytes carried over from the last update, short of a full block. */
  private readonly block = new Uint8Array(BLOCK_BYTES);
  private readonly blockView = new DataView(this.block.buffer);
  private readonly schedule = new DataView(new ArrayBuffer(ROUNDS * 4));
  private pending = 0;
  private totalBytes = 0;

  /** Feeds the next run of bytes. Any number of calls, of any sizes. */
  update(bytes: Uint8Array): this {
    this.totalBytes += bytes.length;
    const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    let offset = 0;

    if (this.pending > 0) {
      const take = Math.min(BLOCK_BYTES - this.pending, bytes.length);
      this.block.set(bytes.subarray(0, take), this.pending);
      this.pending += take;
      offset = take;
      if (this.pending === BLOCK_BYTES) {
        this.compress(this.blockView, 0);
        this.pending = 0;
      }
    }
    while (offset + BLOCK_BYTES <= bytes.length) {
      this.compress(view, offset);
      offset += BLOCK_BYTES;
    }
    if (offset < bytes.length) {
      this.block.set(bytes.subarray(offset), 0);
      this.pending = bytes.length - offset;
    }
    return this;
  }

  /** Pads the tail and returns the 32-byte digest. Call once, at the end. */
  digest(): Uint8Array {
    // The 8-byte length field follows the 0x80 marker, so a tail with no room
    // for both spills into one more block.
    const padded = new Uint8Array(this.pending < BLOCK_BYTES - 8 ? BLOCK_BYTES : BLOCK_BYTES * 2);
    padded.set(this.block.subarray(0, this.pending));
    padded[this.pending] = 0x80;
    const paddedView = new DataView(padded.buffer);
    const bits = this.totalBytes * 8;
    paddedView.setUint32(padded.length - 8, Math.floor(bits / 0x1_0000_0000));
    paddedView.setUint32(padded.length - 4, bits % 0x1_0000_0000);
    for (let offset = 0; offset < padded.length; offset += BLOCK_BYTES) {
      this.compress(paddedView, offset);
    }

    const out = new Uint8Array(32);
    const outView = new DataView(out.buffer);
    outView.setUint32(0, this.h0);
    outView.setUint32(4, this.h1);
    outView.setUint32(8, this.h2);
    outView.setUint32(12, this.h3);
    outView.setUint32(16, this.h4);
    outView.setUint32(20, this.h5);
    outView.setUint32(24, this.h6);
    outView.setUint32(28, this.h7);
    return out;
  }

  private compress(view: DataView, offset: number): void {
    const w = this.schedule;
    for (let i = 0; i < 16; i++) {
      w.setUint32(i * 4, view.getUint32(offset + i * 4));
    }
    for (let i = 16; i < ROUNDS; i++) {
      const x = w.getUint32((i - 15) * 4);
      const y = w.getUint32((i - 2) * 4);
      const s0 = rotr(x, 7) ^ rotr(x, 18) ^ (x >>> 3);
      const s1 = rotr(y, 17) ^ rotr(y, 19) ^ (y >>> 10);
      w.setUint32(i * 4, (w.getUint32((i - 16) * 4) + s0 + w.getUint32((i - 7) * 4) + s1) | 0);
    }

    let a = this.h0;
    let b = this.h1;
    let c = this.h2;
    let d = this.h3;
    let e = this.h4;
    let f = this.h5;
    let g = this.h6;
    let h = this.h7;
    for (let i = 0; i < ROUNDS; i++) {
      const s1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (h + s1 + ch + K.getUint32(i * 4) + w.getUint32(i * 4)) | 0;
      const s0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (s0 + maj) | 0;
      h = g;
      g = f;
      f = e;
      e = (d + t1) | 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) | 0;
    }
    this.h0 = (this.h0 + a) | 0;
    this.h1 = (this.h1 + b) | 0;
    this.h2 = (this.h2 + c) | 0;
    this.h3 = (this.h3 + d) | 0;
    this.h4 = (this.h4 + e) | 0;
    this.h5 = (this.h5 + f) | 0;
    this.h6 = (this.h6 + g) | 0;
    this.h7 = (this.h7 + h) | 0;
  }
}

/** Lowercase hex of a digest. */
export function toHex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}
