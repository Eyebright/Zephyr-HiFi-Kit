"use strict";

// Style hygiene guard: keeps dead selectors, new inline styles and raw color
// literals from creeping back in after the UI cleanup. Baselines are fences,
// not targets; lower them when code is cleaned further.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const dir = path.join(__dirname, "assets");
const cssFiles = fs.readdirSync(dir).filter(f => f.endsWith(".css"));
const css = cssFiles.map(f => fs.readFileSync(path.join(dir, f), "utf8")).join("\n");
const srcFiles = fs.readdirSync(dir).filter(f => /\.(js|html)$/.test(f));
const src = srcFiles.map(f => fs.readFileSync(path.join(dir, f), "utf8")).join("\n");

// State/utility classes added at runtime by templates (btn variants, badges…).
const allowed = new Set(["thumb", "big", "sm", "xs", "ok", "warn", "err", "info", "gold",
  "active", "sel", "open", "hidden", "block", "primary", "ghost", "danger", "truncate"]);

test("no unreferenced CSS classes", () => {
  const defined = [...new Set([...css.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)].map(m => m[1]))];
  // class="... sev-${x}" style dynamic prefixes
  const dyn = new Set();
  for (const m of src.matchAll(/class="([^"]*\$\{[^"]*)"/g)) {
    for (const tok of m[1].split(/\s+/)) {
      const i = tok.indexOf("${");
      if (i > 0) dyn.add(tok.slice(0, i));
    }
  }
  const referenced = c => new RegExp("(?<![a-zA-Z0-9-])" + c.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + "(?![a-zA-Z0-9-])").test(src);
  const dead = defined.filter(c => !referenced(c) && !allowed.has(c) && ![...dyn].some(p => c.startsWith(p)));
  assert.deepEqual(dead, [], "unreferenced CSS classes: " + dead.join(", "));
});

test("inline style attribute budget", () => {
  const count = (fs.readFileSync(path.join(dir, "app.js"), "utf8").match(/style="/g) || []).length;
  assert.ok(count <= 28, `inline style attributes grew to ${count} (budget 28)`);
});

test("colors use rgb tokens and no new raw literals", () => {
  // Numeric rgba() literals are banned; use rgba(var(--x-rgb), a).
  const hexBudget = { "favorites.css": 3, "style.css": 38 };
  for (const file of cssFiles) {
    const text = fs.readFileSync(path.join(dir, file), "utf8");
    const rawRgba = (text.match(/rgba\(\s*\d/g) || []).length;
    assert.equal(rawRgba, 0, `${file}: replace raw rgba() with rgb tokens`);
    const hex = (text.match(/#[0-9a-fA-F]{3,8}/g) || []).length;
    const limit = hexBudget[file] || 0;
    assert.ok(hex <= limit, `${file}: ${hex} hex colors (budget ${limit}); prefer CSS variables`);
  }
});
