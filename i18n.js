/* Phone Wall UI strings and locale resolution.
 *
 * To add a language, add an entry to LOCALES with the same keys as `en` and
 * give it a native `name`. Nothing else is required: the language menu, the
 * `?lang=` override and the Auto option all read from this object.
 *
 * Resolution order: ?lang= > localStorage > browser languages > English.
 * `Auto` follows the browser; a concrete choice is remembered.
 */
(function () {
  const LOCALES = {
    en: {
      name: "English",
      strings: {
        title: "Phone Wall",
        gridTitle: "Phone Wall · Grid",
        language: "Language",
        auto: "Auto",
        single: "Single view",
        grid: "Grid",
        back: "← Back",
        recents: "Recents",
        home: "Home",
        enter: "Enter",
        wake: "Wake",
        rename: "✎ Rename",
        type: "Type",
        typePlaceholder: "type text…",
        openSingle: "Open single view",
        gridSubtitle: "every tile is independently controllable",
        connectingMsg: "Connecting to {name} …",
        reconnectingMsg: "Reconnecting to {name} …",
        detectingMsg: "Checking {name} …",
        offlineMsg: "Device disconnected: {name}\nCheck the USB cable or hub",
        renamePrompt: "Display name (leave empty to reset):",
        insecure: "This origin is not a secure context, so only the low-quality fallback works. Open",
        noDevices: "No devices found. Connect a phone over USB with debugging enabled, or create devices.json."
      }
    },
    "zh-CN": {
      name: "简体中文",
      strings: {
        title: "手机墙",
        gridTitle: "手机墙 · 平铺",
        language: "语言",
        auto: "自动",
        single: "单屏",
        grid: "平铺",
        back: "← 返回",
        recents: "后台",
        home: "主页",
        enter: "回车",
        wake: "唤醒",
        rename: "✎ 改名",
        type: "输入",
        typePlaceholder: "输入文字…",
        openSingle: "打开单屏",
        gridSubtitle: "每个画面独立控制",
        connectingMsg: "正在连接 {name} …",
        reconnectingMsg: "正在重连 {name} …",
        detectingMsg: "正在检测 {name} …",
        offlineMsg: "设备已断开：{name}\n请检查 USB 线 / USB 拓展坞",
        renamePrompt: "设备显示名称（留空恢复默认）：",
        insecure: "当前地址不是安全上下文，只能回退低清画面。请改用",
        noDevices: "没有检测到设备。用 USB 连接手机并开启 USB 调试，或创建 devices.json。"
      }
    }
  };

  const FALLBACK = "en";
  const STORAGE_KEY = "phoneWallLang";
  const root = typeof window !== "undefined" ? window : globalThis;
  const hasDOM = typeof document !== "undefined";
  const search = (typeof location !== "undefined" && location.search) ? location.search : "";
  const params = new URLSearchParams(search);

  function normalize(tag) {
    if (!tag) return "";
    const t = String(tag).toLowerCase();
    if (t === "zh" || t.startsWith("zh-")) return "zh-CN";
    if (t.startsWith("en")) return "en";
    return "";
  }

  function browserLang() {
    const list = (typeof navigator !== "undefined" && navigator.languages && navigator.languages.length)
      ? navigator.languages
      : [(typeof navigator !== "undefined" && navigator.language) || ""];
    for (const tag of list) {
      const code = normalize(tag);
      if (code && LOCALES[code]) return code;
    }
    return FALLBACK;
  }

  function stored() {
    try {
      return localStorage.getItem(STORAGE_KEY);
    } catch (e) {
      return null;
    }
  }

  let preference = "auto";
  let lang;

  // ?lang= wins, is persisted, and is removed from the address bar.
  const queryLang = params.get("lang");
  if (queryLang) {
    if (queryLang === "auto") {
      preference = "auto";
      lang = browserLang();
    } else {
      const code = normalize(queryLang);
      if (code && LOCALES[code]) {
        preference = code;
        lang = code;
      }
    }
    if (lang) {
      try { localStorage.setItem(STORAGE_KEY, preference); } catch (e) { /* private mode */ }
      try {
        const url = new URL(location.href);
        url.searchParams.delete("lang");
        history.replaceState(null, "", url);
      } catch (e) { /* no history or no DOM */ }
    }
  }

  if (!lang) {
    const saved = stored();
    if (saved && saved !== "auto") {
      const code = normalize(saved);
      if (code && LOCALES[code]) {
        preference = code;
        lang = code;
      }
    }
    if (!lang) {
      preference = "auto";
      lang = browserLang();
    }
  }

  function t(key, vars) {
    const table = (LOCALES[lang] && LOCALES[lang].strings) || {};
    const fallback = (LOCALES[FALLBACK] && LOCALES[FALLBACK].strings) || {};
    let value = table[key];
    if (value == null) value = fallback[key];
    if (value == null) value = key;
    if (vars) {
      value = value.replace(/\{(\w+)\}/g, (match, name) =>
        vars[name] == null ? match : String(vars[name]));
    }
    return value;
  }

  function apply(node) {
    if (!hasDOM) return;
    const scope = node || document;
    scope.querySelectorAll("[data-i18n]").forEach((el) => {
      el.textContent = t(el.getAttribute("data-i18n"));
    });
    scope.querySelectorAll("[data-i18n-placeholder]").forEach((el) => {
      el.placeholder = t(el.getAttribute("data-i18n-placeholder"));
    });
    scope.querySelectorAll("[data-i18n-title]").forEach((el) => {
      el.title = t(el.getAttribute("data-i18n-title"));
    });
  }

  function mountSelect(el) {
    if (!hasDOM || !el) return;
    el.setAttribute("aria-label", t("language"));
    el.title = t("language");
    const auto = document.createElement("option");
    auto.value = "auto";
    auto.textContent = t("auto");
    el.appendChild(auto);
    Object.keys(LOCALES).forEach((code) => {
      const option = document.createElement("option");
      option.value = code;
      option.textContent = LOCALES[code].name;
      el.appendChild(option);
    });
    el.value = preference;
    el.addEventListener("change", () => setLang(el.value));
  }

  function setLang(value) {
    try { localStorage.setItem(STORAGE_KEY, value); } catch (e) { /* private mode */ }
    try {
      const url = new URL(location.href);
      url.searchParams.delete("lang");
      location.href = url.toString();
    } catch (e) {
      if (typeof location !== "undefined") location.reload();
    }
  }

  if (hasDOM) document.documentElement.lang = lang;

  root.I18N = {
    lang: lang,
    preference: preference,
    locales: LOCALES,
    available: Object.keys(LOCALES).map((code) => ({ code: code, name: LOCALES[code].name })),
    t: t,
    apply: apply,
    mountSelect: mountSelect,
    setLang: setLang
  };
})();
