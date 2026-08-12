// BlurHash decoder (Wolt's algorithm) — the instant, low-cost placeholder shown
// while the full attachment downloads and decrypts (HLD §9). Pure math over the
// compact ASCII hash: no canvas, no native module. Web rasterizes the returned
// RGBA into a <canvas>; mobile, lacking a cheap per-pixel path, paints just the
// average colour. Kept here (not in a UI package) so both platforms decode the
// same way.

const DIGITS = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~";

/** decodeBlurhash rasterizes `hash` into a `width`×`height` RGBA buffer (4 bytes
 *  per pixel). `punch` (default 1) scales contrast. Throws on a malformed hash. */
export function decodeBlurhash(hash: string, width: number, height: number, punch = 1): Uint8ClampedArray {
  if (hash.length < 6) throw new Error("blurhash too short");
  const sizeFlag = decode83(hash, 0, 1);
  const numY = Math.floor(sizeFlag / 9) + 1;
  const numX = (sizeFlag % 9) + 1;
  if (hash.length !== 4 + 2 * numX * numY) throw new Error("blurhash length does not match its size flag");

  const quantMax = decode83(hash, 1, 2);
  const maxValue = ((quantMax + 1) / 166) * punch;

  // colors[0] is the DC (average) term; the rest are AC coefficients.
  const colors: Array<[number, number, number]> = new Array(numX * numY);
  const dc = decode83(hash, 2, 6);
  colors[0] = [srgbToLinear((dc >> 16) & 255), srgbToLinear((dc >> 8) & 255), srgbToLinear(dc & 255)];
  for (let i = 1; i < numX * numY; i++) {
    const ac = decode83(hash, 4 + i * 2, 6 + i * 2);
    colors[i] = decodeAC(ac, maxValue);
  }

  const out = new Uint8ClampedArray(width * height * 4);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      let r = 0;
      let g = 0;
      let b = 0;
      for (let j = 0; j < numY; j++) {
        for (let i = 0; i < numX; i++) {
          const basis = Math.cos((Math.PI * x * i) / width) * Math.cos((Math.PI * y * j) / height);
          const c = colors[i + j * numX]!;
          r += c[0] * basis;
          g += c[1] * basis;
          b += c[2] * basis;
        }
      }
      const p = (y * width + x) * 4;
      out[p] = linearToSrgb(r);
      out[p + 1] = linearToSrgb(g);
      out[p + 2] = linearToSrgb(b);
      out[p + 3] = 255;
    }
  }
  return out;
}

/** blurhashAverageColor returns the hash's DC term as an sRGB {r,g,b} (0–255) —
 *  the cheap placeholder mobile paints without rasterizing every pixel. */
export function blurhashAverageColor(hash: string): { r: number; g: number; b: number } {
  if (hash.length < 6) throw new Error("blurhash too short");
  const dc = decode83(hash, 2, 6);
  return { r: (dc >> 16) & 255, g: (dc >> 8) & 255, b: dc & 255 };
}

/** blurhashCssColor is `blurhashAverageColor` as a `rgb(...)` string, or a grey
 *  fallback when the hash is absent/invalid. */
export function blurhashCssColor(hash: string | undefined): string {
  if (!hash) return "rgb(200,200,200)";
  try {
    const { r, g, b } = blurhashAverageColor(hash);
    return `rgb(${r},${g},${b})`;
  } catch {
    return "rgb(200,200,200)";
  }
}

function decodeAC(value: number, maxValue: number): [number, number, number] {
  const r = Math.floor(value / (19 * 19));
  const g = Math.floor(value / 19) % 19;
  const b = value % 19;
  return [signPow((r - 9) / 9) * maxValue, signPow((g - 9) / 9) * maxValue, signPow((b - 9) / 9) * maxValue];
}

function signPow(v: number): number {
  return Math.sign(v) * v * v;
}

function srgbToLinear(value: number): number {
  const v = value / 255;
  return v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
}

function linearToSrgb(value: number): number {
  const v = Math.max(0, Math.min(1, value));
  return v <= 0.0031308 ? Math.round(v * 12.92 * 255 + 0.5) : Math.round((1.055 * Math.pow(v, 1 / 2.4) - 0.055) * 255 + 0.5);
}

function decode83(str: string, from: number, to: number): number {
  let value = 0;
  for (let i = from; i < to; i++) {
    const digit = DIGITS.indexOf(str.charAt(i));
    if (digit < 0) throw new Error("invalid character in blurhash");
    value = value * 83 + digit;
  }
  return value;
}
