// Fails if a locale is missing a key, has an extra key, or has an empty value.
// Runs in CI without a browser: i18n.js guards all DOM access.
const assert = require("assert");
const path = require("path");

global.window = global;
require(path.join(__dirname, "..", "i18n.js"));

const I18N = global.I18N;
const locales = I18N.locales;
const codes = Object.keys(locales);
assert(codes.length >= 2, "expected at least two locales");
assert(codes.includes("en"), "the English fallback is missing");

// Language tags resolve to the locale objects above.
const tags = {
  "en-US": "en",
  "zh": "zh-CN",
  "zh-Hans": "zh-CN",
  "ja-JP": "ja",
  "vi-VN": "vi"
};
for (const [tag, code] of Object.entries(tags)) {
  assert.strictEqual(I18N.normalize(tag), code, `${tag} should map to ${code}`);
  assert(locales[code], `${code}: locale missing`);
}
assert.strictEqual(I18N.normalize("de-DE"), "", "unknown tags must fall through");

const base = Object.keys(locales.en.strings).sort();
for (const code of codes) {
  const strings = locales[code].strings;
  assert(strings, `${code}: no strings object`);
  const keys = Object.keys(strings).sort();
  assert.deepStrictEqual(keys, base, `${code}: keys differ from en`);
  for (const key of keys) {
    assert(typeof strings[key] === "string" && strings[key].length > 0,
      `${code}.${key}: empty or non-string`);
  }
}

console.log(`i18n ok: ${codes.join(", ")} (${base.length} keys)`);
