// Renders the PNG icons of the PWA from the SVGs in this directory into
// public/icons, with headless Chromium. A development tool: run it after
// changing an SVG and commit the PNGs. Usage:
//
//   CHROMIUM=/path/to/chromium node icons/render.mjs
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "..", "public", "icons");
const chromium = process.env.CHROMIUM ?? "chromium";

const icons = [
  { svg: "icon.svg", png: "icon-192.png", size: 192 },
  { svg: "icon.svg", png: "icon-512.png", size: 512 },
  { svg: "maskable.svg", png: "maskable-512.png", size: 512 },
  { svg: "icon.svg", png: "apple-touch-icon.png", size: 180 },
  { svg: "badge.svg", png: "badge-96.png", size: 96 },
];

const work = mkdtempSync(join(tmpdir(), "icons-"));
for (const icon of icons) {
  const svg = readFileSync(join(here, icon.svg), "utf8");
  const page = join(work, `${icon.png}.html`);
  writeFileSync(
    page,
    `<!doctype html><html><body style="margin:0;background:transparent">` +
      `<div style="width:${icon.size}px;height:${icon.size}px">${svg.replace("<svg ", '<svg width="100%" height="100%" ')}</div>` +
      `</body></html>`,
  );
  execFileSync(chromium, [
    "--headless=new",
    "--no-sandbox",
    "--disable-gpu",
    "--hide-scrollbars",
    "--default-background-color=00000000",
    `--window-size=${icon.size},${icon.size}`,
    `--screenshot=${join(out, icon.png)}`,
    `file://${page}`,
  ]);
  process.stdout.write(`${icon.png} (${icon.size}px)\n`);
}
