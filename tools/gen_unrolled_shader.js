const K = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
  0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
  0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
  0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
  0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

const hex = n => `0x${(n >>> 0).toString(16).padStart(8, "0")}u`;
const pbyte = i => `(prefix[${i}u] & 0xffu)`;
const word = (b0, b1, b2, b3) =>
  `((${b0}) << 24u) | ((${b1}) << 16u) | ((${b2}) << 8u) | (${b3})`;

let out = `struct Params {
    prefix_len: u32,
    start_nonce: u32,
    count: u32,
    difficulty: u32,
}

struct Result {
    nonce: atomic<u32>,
}

@group(0) @binding(0) var<storage, read> prefix: array<u32>;
@group(0) @binding(1) var<uniform> params: Params;
@group(0) @binding(2) var<storage, read_write> result: Result;

fn rotr(x: u32, n: u32) -> u32 {
    return (x >> n) | (x << (32u - n));
}

fn ch(x: u32, y: u32, z: u32) -> u32 {
    return (x & y) ^ ((~x) & z);
}

fn maj(x: u32, y: u32, z: u32) -> u32 {
    return (x & y) ^ (x & z) ^ (y & z);
}

fn bsig0(x: u32) -> u32 {
    return rotr(x, 2u) ^ rotr(x, 13u) ^ rotr(x, 22u);
}

fn bsig1(x: u32) -> u32 {
    return rotr(x, 6u) ^ rotr(x, 11u) ^ rotr(x, 25u);
}

fn ssig0(x: u32) -> u32 {
    return rotr(x, 7u) ^ rotr(x, 18u) ^ (x >> 3u);
}

fn ssig1(x: u32) -> u32 {
    return rotr(x, 17u) ^ rotr(x, 19u) ^ (x >> 10u);
}

fn decimal_len(n: u32) -> u32 {
    if (n < 10u) { return 1u; }
    if (n < 100u) { return 2u; }
    if (n < 1000u) { return 3u; }
    if (n < 10000u) { return 4u; }
    if (n < 100000u) { return 5u; }
    if (n < 1000000u) { return 6u; }
    if (n < 10000000u) { return 7u; }
    if (n < 100000000u) { return 8u; }
    if (n < 1000000000u) { return 9u; }
    return 10u;
}

fn pow10(exp: u32) -> u32 {
    if (exp == 0u) { return 1u; }
    if (exp == 1u) { return 10u; }
    if (exp == 2u) { return 100u; }
    if (exp == 3u) { return 1000u; }
    if (exp == 4u) { return 10000u; }
    if (exp == 5u) { return 100000u; }
    if (exp == 6u) { return 1000000u; }
    if (exp == 7u) { return 10000000u; }
    if (exp == 8u) { return 100000000u; }
    return 1000000000u;
}

fn decimal_digit(n: u32, idx: u32, len: u32) -> u32 {
    let div = pow10(len - idx - 1u);
    return 48u + ((n / div) % 10u);
}

fn second_block_byte(pos: u32, nonce: u32, len: u32) -> u32 {
    if (pos == 0u) { return 58u; }
    if (pos >= 1u && pos <= 10u) {
        let idx = pos - 1u;
        if (idx < len) { return decimal_digit(nonce, idx, len); }
        if (idx == len) { return 0x80u; }
        return 0u;
    }
    let bit_len = (65u + len) * 8u;
    if (pos == 62u) { return (bit_len >> 8u) & 0xffu; }
    if (pos == 63u) { return bit_len & 0xffu; }
    return 0u;
}

fn check_nonce(nonce: u32) -> bool {
    let len = decimal_len(nonce);
    var h0 = 0x6a09e667u;
    var h1 = 0xbb67ae85u;
    var h2 = 0x3c6ef372u;
    var h3 = 0xa54ff53au;
    var h4 = 0x510e527fu;
    var h5 = 0x9b05688cu;
    var h6 = 0x1f83d9abu;
    var h7 = 0x5be0cd19u;
`;

function emitBlock(words, suffix) {
  const w = i => `w${suffix}_${i}`;
  for (let i = 0; i < 16; i++) out += `    var ${w(i)} = ${words[i]};\n`;
  for (let i = 16; i < 64; i++) {
    out += `    var ${w(i)} = ssig1(${w(i - 2)}) + ${w(i - 7)} + ssig0(${w(i - 15)}) + ${w(i - 16)};\n`;
  }
  out += `    var a${suffix} = h0;\n    var b${suffix} = h1;\n    var c${suffix} = h2;\n    var d${suffix} = h3;\n    var e${suffix} = h4;\n    var f${suffix} = h5;\n    var g${suffix} = h6;\n    var hh${suffix} = h7;\n`;
  for (let i = 0; i < 64; i++) {
    out += `    let t1_${suffix}_${i} = hh${suffix} + bsig1(e${suffix}) + ch(e${suffix}, f${suffix}, g${suffix}) + ${hex(K[i])} + ${w(i)};\n`;
    out += `    let t2_${suffix}_${i} = bsig0(a${suffix}) + maj(a${suffix}, b${suffix}, c${suffix});\n`;
    out += `    hh${suffix} = g${suffix};\n    g${suffix} = f${suffix};\n    f${suffix} = e${suffix};\n    e${suffix} = d${suffix} + t1_${suffix}_${i};\n    d${suffix} = c${suffix};\n    c${suffix} = b${suffix};\n    b${suffix} = a${suffix};\n    a${suffix} = t1_${suffix}_${i} + t2_${suffix}_${i};\n`;
  }
  out += `    h0 = h0 + a${suffix};\n    h1 = h1 + b${suffix};\n    h2 = h2 + c${suffix};\n    h3 = h3 + d${suffix};\n    h4 = h4 + e${suffix};\n    h5 = h5 + f${suffix};\n    h6 = h6 + g${suffix};\n    h7 = h7 + hh${suffix};\n`;
}

let first = [];
for (let i = 0; i < 16; i++) {
  first.push(word(pbyte(i * 4), pbyte(i * 4 + 1), pbyte(i * 4 + 2), pbyte(i * 4 + 3)));
}
emitBlock(first, "a");

let second = [];
for (let i = 0; i < 16; i++) {
  second.push(word(
    `second_block_byte(${i * 4}u, nonce, len)`,
    `second_block_byte(${i * 4 + 1}u, nonce, len)`,
    `second_block_byte(${i * 4 + 2}u, nonce, len)`,
    `second_block_byte(${i * 4 + 3}u, nonce, len)`,
  ));
}
emitBlock(second, "b");

out += `
    var bits = 0u;
    if (h0 == 0u) { bits = bits + 32u; } else { return countLeadingZeros(h0) >= params.difficulty; }
    if (h1 == 0u) { bits = bits + 32u; } else { return bits + countLeadingZeros(h1) >= params.difficulty; }
    if (h2 == 0u) { bits = bits + 32u; } else { return bits + countLeadingZeros(h2) >= params.difficulty; }
    if (h3 == 0u) { bits = bits + 32u; } else { return bits + countLeadingZeros(h3) >= params.difficulty; }
    if (h4 == 0u) { bits = bits + 32u; } else { return bits + countLeadingZeros(h4) >= params.difficulty; }
    if (h5 == 0u) { bits = bits + 32u; } else { return bits + countLeadingZeros(h5) >= params.difficulty; }
    if (h6 == 0u) { bits = bits + 32u; } else { return bits + countLeadingZeros(h6) >= params.difficulty; }
    return bits + countLeadingZeros(h7) >= params.difficulty;
}

@compute @workgroup_size(256)
fn main(@builtin(global_invocation_id) id: vec3<u32>) {
    let idx = id.x;
    if (idx >= params.count) {
        return;
    }

    if (atomicLoad(&result.nonce) != 0xffffffffu) {
        return;
    }

    let nonce = params.start_nonce + idx;
    if (check_nonce(nonce)) {
        atomicMin(&result.nonce, nonce);
    }
}
`;

process.stdout.write(out);
