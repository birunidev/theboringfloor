import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
const css = await readFile(
  new URL("assets/style.css", import.meta.url),
  "utf8",
);
const blocks = [
  ...css.matchAll(/:root(?:\[data-theme="?dark"?\])?\s*\{([^}]+)\}/g),
];
const pairs = [
  ...["canvas", "surface", "surface-subtle", "accent-soft"].flatMap((bg) => [
    ["text", bg, 4.5],
    ["muted", bg, 4.5],
  ]),
  ["accent", "accent-soft", 4.5],
  ["success", "success-soft", 4.5],
  ["warning", "warning-soft", 4.5],
  ["success", "surface", 4.5],
  ["warning", "surface", 4.5],
  ["danger", "danger-soft", 4.5],
  ["on-accent", "accent", 4.5],
  ["on-danger", "danger", 4.5],
  ...["canvas", "surface", "surface-subtle"].flatMap((bg) => [
    ["border", bg, 3],
    ["accent", bg, 3],
    ["danger", bg, 3],
  ]),
];
function luminance(hex) {
  const channels = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return channels.reduce(
    (sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i],
    0,
  );
}
assert.equal(blocks.length, 2, "light and dark token blocks");
const output = [];
for (const [index, block] of blocks.entries()) {
  const tokens = Object.fromEntries(
    [...block[1].matchAll(/--([a-z-]+):\s*(#[A-Fa-f0-9]{6})\s*;/g)].map((m) => [
      m[1],
      m[2],
    ]),
  );
  for (const [fg, bg, minimum] of pairs) {
    assert(tokens[fg] && tokens[bg]);
    const [low, high] = [luminance(tokens[fg]), luminance(tokens[bg])].sort(
        (a, b) => a - b,
      ),
      ratio = (high + 0.05) / (low + 0.05);
    assert(ratio >= minimum, `${fg}/${bg}: ${ratio}`);
    output.push(
      `${index ? "dark" : "light"} ${fg} on ${bg}: ${ratio.toFixed(2)}:1 PASS (>=${minimum})`,
    );
  }
}
console.log(output.join("\n"));
if (process.env.OFFICEVIEW_CONTRAST_OUTPUT)
  await writeFile(
    process.env.OFFICEVIEW_CONTRAST_OUTPUT,
    output.join("\n") + "\n",
  );
