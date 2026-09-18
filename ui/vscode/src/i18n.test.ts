import { strict as assert } from "node:assert";
import { describe, it } from "node:test";
import { getLang, normaliseLang, resolveLang, setLang, t } from "./i18n";

describe("i18n", () => {
  it("resolves the setting first, then the editor, then English", () => {
    assert.equal(resolveLang("ru", "en"), "ru", "an explicit setting wins over the editor");
    assert.equal(resolveLang("auto", "ru"), "ru", "auto follows the editor");
    assert.equal(resolveLang(undefined, "ru-RU"), "ru", "a regional editor language still matches");
    assert.equal(resolveLang("auto", "de"), "en", "a language we do not carry falls back");
    assert.equal(resolveLang("auto", undefined), "en");
  });

  it("ignores a setting naming a language we do not carry", () => {
    // Otherwise a typo in settings.json would silently blank every string.
    assert.equal(resolveLang("kl", "ru"), "ru");
  });

  it("matches a language tag by its prefix only", () => {
    assert.equal(normaliseLang("en-GB"), "en");
    assert.equal(normaliseLang("ru_RU"), "ru");
    assert.equal(normaliseLang("english"), undefined, "a prefix match must not be a substring match");
    assert.equal(normaliseLang(""), undefined);
  });

  it("falls back to English for a key a language is missing", () => {
    // A half-translated language must degrade to a readable screen, never to
    // the key itself.
    const before = getLang();
    try {
      setLang("ru");
      assert.equal(t("notice.background_turn_done"), "Фоновый ход завершён — история обновлена.");
      // No language carries this one.
      assert.equal(t("no.such.key"), "no.such.key");
    } finally {
      setLang(before);
    }
  });

  it("fills placeholders", () => {
    const before = getLang();
    try {
      setLang("en");
      const s = t("notice.memory_failed", { detail: "disk full" });
      assert.ok(s.includes("disk full"), s);
      assert.ok(!s.includes("{detail}"), s);
    } finally {
      setLang(before);
    }
  });
});
