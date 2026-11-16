/**
 * A small QR code encoder (ISO/IEC 18004): byte mode, error correction level
 * M, versions 1-10 (up to 213 bytes). Enough for a Telegram deep link, with no
 * third-party dependency. Structure follows Project Nayuki's reference
 * implementation; the output is checked with OpenCV's QR reader.
 */

// [total codewords, error-correction codewords per block, blocks] for level M.
const VERSIONS: readonly (readonly [number, number, number])[] = [
  [26, 10, 1], [44, 16, 1], [70, 26, 1], [100, 18, 2], [134, 24, 2],
  [172, 16, 4], [196, 18, 4], [242, 22, 4], [292, 22, 5], [346, 26, 5],
];

function multiply(x: number, y: number) {
  let z = 0;
  for (let i = 7; i >= 0; i -= 1) {
    z = (z << 1) ^ ((z >>> 7) * 0x11d);
    z ^= ((y >>> i) & 1) * x;
  }
  return z;
}

function divisor(degree: number) {
  const result = new Array<number>(degree).fill(0);
  result[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i += 1) {
    for (let j = 0; j < result.length; j += 1) {
      result[j] = multiply(result[j]!, root);
      if (j + 1 < result.length) result[j] = result[j]! ^ result[j + 1]!;
    }
    root = multiply(root, 0x02);
  }
  return result;
}

function remainder(data: number[], generator: number[]) {
  const result = new Array<number>(generator.length).fill(0);
  for (const byte of data) {
    const factor = byte ^ result.shift()!;
    result.push(0);
    generator.forEach((coefficient, index) => { result[index] = result[index]! ^ multiply(coefficient, factor); });
  }
  return result;
}

function alignmentPositions(version: number) {
  if (version === 1) return [];
  const count = Math.floor(version / 7) + 2;
  const size = version * 4 + 17;
  const step = Math.ceil((version * 4 + 4) / (count * 2 - 2)) * 2;
  const positions = [6];
  for (let position = size - 7; positions.length < count; position -= step) positions.splice(1, 0, position);
  return positions;
}

function bit(value: number, index: number) {
  return ((value >>> index) & 1) !== 0;
}

/** The modules (true = dark) of a QR code for `text`, without the quiet zone. */
export function qrMatrix(text: string): boolean[][] {
  const bytes = Array.from(new TextEncoder().encode(text));
  let version = 0;
  for (let candidate = 1; candidate <= VERSIONS.length; candidate += 1) {
    const [total, ecc, blocks] = VERSIONS[candidate - 1]!;
    const capacityBits = (total - ecc * blocks) * 8;
    if (4 + (candidate < 10 ? 8 : 16) + bytes.length * 8 <= capacityBits) {
      version = candidate;
      break;
    }
  }
  if (!version) throw new Error("Text is too long for a QR code.");
  const [total, eccLength, blockCount] = VERSIONS[version - 1]!;
  const dataLength = total - eccLength * blockCount;

  // Data codewords: mode, length, bytes, terminator, padding.
  const bits: number[] = [];
  const push = (value: number, length: number) => { for (let i = length - 1; i >= 0; i -= 1) bits.push((value >>> i) & 1); };
  push(0b0100, 4);
  push(bytes.length, version < 10 ? 8 : 16);
  bytes.forEach((byte) => push(byte, 8));
  push(0, Math.min(4, dataLength * 8 - bits.length));
  push(0, (8 - (bits.length % 8)) % 8);
  const data: number[] = [];
  for (let i = 0; i < bits.length; i += 8) data.push(bits.slice(i, i + 8).reduce((value, item) => (value << 1) | item, 0));
  for (let pad = 0xec; data.length < dataLength; pad ^= 0xec ^ 0x11) data.push(pad);

  // Split into blocks, add error correction, interleave.
  const generator = divisor(eccLength);
  const shortBlocks = blockCount - (total % blockCount);
  const shortLength = Math.floor(total / blockCount);
  const blocks: number[][] = [];
  for (let index = 0, offset = 0; index < blockCount; index += 1) {
    const length = shortLength - eccLength + (index < shortBlocks ? 0 : 1);
    const chunk = data.slice(offset, offset + length);
    offset += length;
    const ecc = remainder(chunk, generator);
    if (index < shortBlocks) chunk.push(0);
    blocks.push([...chunk, ...ecc]);
  }
  const codewords: number[] = [];
  for (let i = 0; i < blocks[0]!.length; i += 1) {
    blocks.forEach((block, index) => {
      if (i !== shortLength - eccLength || index >= shortBlocks) codewords.push(block[i]!);
    });
  }

  const size = version * 4 + 17;
  const modules = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const fixed = Array.from({ length: size }, () => new Array<boolean>(size).fill(false));
  const set = (x: number, y: number, dark: boolean) => { modules[y]![x] = dark; fixed[y]![x] = true; };

  for (let i = 0; i < size; i += 1) {
    set(6, i, i % 2 === 0);
    set(i, 6, i % 2 === 0);
  }
  for (const [cx, cy] of [[3, 3], [size - 4, 3], [3, size - 4]] as const) {
    for (let dy = -4; dy <= 4; dy += 1) {
      for (let dx = -4; dx <= 4; dx += 1) {
        const x = cx + dx;
        const y = cy + dy;
        const distance = Math.max(Math.abs(dx), Math.abs(dy));
        if (x >= 0 && x < size && y >= 0 && y < size) set(x, y, distance !== 2 && distance !== 4);
      }
    }
  }
  const aligns = alignmentPositions(version);
  aligns.forEach((cx, i) => aligns.forEach((cy, j) => {
    const corner = (i === 0 && j === 0) || (i === 0 && j === aligns.length - 1) || (i === aligns.length - 1 && j === 0);
    if (corner) return;
    for (let dy = -2; dy <= 2; dy += 1) {
      for (let dx = -2; dx <= 2; dx += 1) set(cx + dx, cy + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
    }
  }));

  const drawFormat = (mask: number) => {
    const value = mask; // level M has format bits 00
    let rest = value;
    for (let i = 0; i < 10; i += 1) rest = (rest << 1) ^ ((rest >>> 9) * 0x537);
    const format = ((value << 10) | rest) ^ 0x5412;
    for (let i = 0; i <= 5; i += 1) set(8, i, bit(format, i));
    set(8, 7, bit(format, 6));
    set(8, 8, bit(format, 7));
    set(7, 8, bit(format, 8));
    for (let i = 9; i < 15; i += 1) set(14 - i, 8, bit(format, i));
    for (let i = 0; i < 8; i += 1) set(size - 1 - i, 8, bit(format, i));
    for (let i = 8; i < 15; i += 1) set(8, size - 15 + i, bit(format, i));
    set(8, size - 8, true);
  };
  drawFormat(0);
  if (version >= 7) {
    let rest = version;
    for (let i = 0; i < 12; i += 1) rest = (rest << 1) ^ ((rest >>> 11) * 0x1f25);
    const value = (version << 12) | rest;
    for (let i = 0; i < 18; i += 1) {
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      set(a, b, bit(value, i));
      set(b, a, bit(value, i));
    }
  }

  let index = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vertical = 0; vertical < size; vertical += 1) {
      for (let j = 0; j < 2; j += 1) {
        const x = right - j;
        const y = ((right + 1) & 2) === 0 ? size - 1 - vertical : vertical;
        if (!fixed[y]![x] && index < codewords.length * 8) {
          modules[y]![x] = bit(codewords[index >>> 3]!, 7 - (index & 7));
          index += 1;
        }
      }
    }
  }

  const masks: ((x: number, y: number) => boolean)[] = [
    (x, y) => (x + y) % 2 === 0,
    (_x, y) => y % 2 === 0,
    (x) => x % 3 === 0,
    (x, y) => (x + y) % 3 === 0,
    (x, y) => (Math.floor(x / 3) + Math.floor(y / 2)) % 2 === 0,
    (x, y) => ((x * y) % 2) + ((x * y) % 3) === 0,
    (x, y) => (((x * y) % 2) + ((x * y) % 3)) % 2 === 0,
    (x, y) => (((x + y) % 2) + ((x * y) % 3)) % 2 === 0,
  ];
  const applyMask = (mask: number) => {
    for (let y = 0; y < size; y += 1) {
      for (let x = 0; x < size; x += 1) {
        if (!fixed[y]![x] && masks[mask]!(x, y)) modules[y]![x] = !modules[y]![x];
      }
    }
  };
  let best = 0;
  let bestPenalty = Infinity;
  for (let mask = 0; mask < 8; mask += 1) {
    applyMask(mask);
    drawFormat(mask);
    const score = penalty(modules);
    if (score < bestPenalty) {
      best = mask;
      bestPenalty = score;
    }
    applyMask(mask);
  }
  applyMask(best);
  drawFormat(best);
  return modules;
}

/** Penalty rules 1, 2 and 4 of the standard, plus finder-like runs. */
function penalty(modules: boolean[][]) {
  const size = modules.length;
  let score = 0;
  const lines = [
    ...modules,
    ...modules.map((_row, x) => modules.map((row) => row[x]!)),
  ];
  for (const line of lines) {
    let run = 1;
    for (let i = 1; i <= size; i += 1) {
      if (i < size && line[i] === line[i - 1]) {
        run += 1;
      } else {
        if (run >= 5) score += run - 2;
        run = 1;
      }
    }
    const text = line.map((dark) => (dark ? "1" : "0")).join("");
    for (const pattern of ["10111010000", "00001011101"]) {
      for (let at = text.indexOf(pattern); at >= 0; at = text.indexOf(pattern, at + 1)) score += 40;
    }
  }
  for (let y = 0; y < size - 1; y += 1) {
    for (let x = 0; x < size - 1; x += 1) {
      const color = modules[y]![x];
      if (color === modules[y]![x + 1] && color === modules[y + 1]![x] && color === modules[y + 1]![x + 1]) score += 3;
    }
  }
  const dark = modules.reduce((count, row) => count + row.filter(Boolean).length, 0);
  score += Math.floor(Math.abs(dark * 20 - size * size * 10) / (size * size)) * 10;
  return score;
}

/** An SVG path ("M x y h1 v1 h-1 z" per dark module) with a 4-module quiet zone. */
export function qrSvgPath(modules: boolean[][]) {
  const parts: string[] = [];
  modules.forEach((row, y) => row.forEach((dark, x) => { if (dark) parts.push(`M${x + 4} ${y + 4}h1v1h-1z`); }));
  return { path: parts.join(""), size: modules.length + 8 };
}
