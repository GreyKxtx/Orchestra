  function hideOverlay() {
    overlay?.classList.add("hidden");
    if (overlayOptions) overlayOptions.innerHTML = "";
    if (overlayActions) overlayActions.innerHTML = "";
    overlayInput?.classList.add("hidden");
    questionState.mode = "";
  }

  /** @param {any} request */
  function showPermissionOverlay(request) {
    if (!overlay || !overlayTitle || !overlayBody || !overlayActions) return;
    const isLSP = request.kind === "lsp.install" || request.tool === "lsp.install";
    overlayTitle.textContent = isLSP
      ? i18n("perm.install_lsp")
      : i18n("perm.allow_tool", { tool: request.tool || i18n("perm.tool") });
    const extra = isLSP ? i18n("perm.install_extra") : "";
    overlayBody.textContent = [request.description, request.reason, extra]
      .filter(Boolean)
      .join("\n\n");
    overlayActions.innerHTML = "";
    const buttons = isLSP
      ? [
          { label: i18n("perm.skip"), approved: false },
          { label: i18n("perm.install_once"), approved: true },
          { label: i18n("perm.install_always"), approved: true, always: true },
        ]
      : [
          { label: i18n("perm.deny"), approved: false },
          { label: i18n("perm.allow_once"), approved: true },
          { label: i18n("perm.allow_always"), approved: true, always: true },
        ];
    buttons.forEach((btn) => {
      const el = document.createElement("button");
      el.type = "button";
      el.className = "pill" + (btn.approved ? " primary" : "");
      el.textContent = btn.label;
      el.addEventListener("click", () => {
        hideOverlay();
        host.postMessage({
          type: "permissionReply",
          approved: btn.approved,
          always: Boolean(btn.always),
        });
      });
      overlayActions.appendChild(el);
    });
    overlay.classList.remove("hidden");
  }

  /** @param {any[]} questions */
  function showQuestionOverlay(questions) {
    if (!questions.length) {
      host.postMessage({ type: "questionReply", answers: [] });
      return;
    }
    questionState = { questions, index: 0, answers: [], mode: "question" };
    renderQuestionStep();
  }

  function renderQuestionStep() {
    const q = questionState.questions[questionState.index];
    if (!q || !overlay || !overlayTitle || !overlayBody || !overlayOptions || !overlayActions) {
      host.postMessage({ type: "questionReply", answers: questionState.answers });
      hideOverlay();
      return;
    }
    overlayTitle.textContent = i18n("question.step", {
      n: questionState.index + 1,
      total: questionState.questions.length,
    });
    overlayBody.textContent = q.question || "";
    overlayOptions.innerHTML = "";
    overlayActions.innerHTML = "";
    if (q.options && q.options.length) {
      q.options.forEach((opt, i) => {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "pill";
        if (i < 9) {
          const key = document.createElement("span");
          key.className = "opt-key";
          key.textContent = String(i + 1);
          btn.appendChild(key);
        }
        const label = document.createElement("span");
        label.textContent = opt;
        btn.appendChild(label);
        btn.addEventListener("click", () => {
          questionState.answers.push(opt);
          questionState.index += 1;
          renderQuestionStep();
        });
        overlayOptions.appendChild(btn);
      });
    } else {
      overlayInput?.classList.remove("hidden");
      if (overlayInput) overlayInput.value = "";
      const next = document.createElement("button");
      next.type = "button";
      next.className = "pill primary";
      next.textContent = i18n("question.next");
      next.addEventListener("click", () => {
        questionState.answers.push(overlayInput?.value || "");
        questionState.index += 1;
        overlayInput?.classList.add("hidden");
        renderQuestionStep();
      });
      overlayActions.appendChild(next);
    }
    overlay.classList.remove("hidden");
    // The panel sits above the composer; take the focus so the keyboard
    // answers it (a digit picks an option, Enter sends a typed answer).
    if (q.options && q.options.length) {
      /** @type {HTMLElement | null} */ (overlayOptions.querySelector("button"))?.focus({ preventScroll: true });
    } else {
      overlayInput?.focus({ preventScroll: true });
    }
  }

  overlayInput?.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.isComposing) {
      ev.preventDefault();
      /** @type {HTMLElement | null} */ (overlayActions?.querySelector("button.primary"))?.click();
    }
  });

  // 1…9 picks the numbered option of the question on screen, unless the key is
  // meant for a text field (the composer, the answer input).
  document.addEventListener("keydown", (ev) => {
    if (!overlay || overlay.classList.contains("hidden") || questionState.mode !== "question") return;
    if (ev.ctrlKey || ev.metaKey || ev.altKey || ev.isComposing) return;
    const t = /** @type {HTMLElement | null} */ (ev.target);
    if (t && (t.tagName === "TEXTAREA" || t.tagName === "INPUT" || t.isContentEditable)) return;
    if (!/^[1-9]$/.test(ev.key)) return;
    const btn = overlayOptions?.querySelectorAll("button")[Number(ev.key) - 1];
    if (btn) {
      ev.preventDefault();
      /** @type {HTMLElement} */ (btn).click();
    }
  });

  function matchJSONObject(s, start) {
    let depth = 0;
    let inStr = false;
    let esc = false;
    for (let j = start; j < s.length; j++) {
      const c = s[j];
      if (esc) {
        esc = false;
        continue;
      }
      if (c === "\\") {
        esc = true;
        continue;
      }
      if (c === '"') {
        inStr = !inStr;
        continue;
      }
      if (inStr) {
        continue;
      }
      if (c === "{") {
        depth++;
      } else if (c === "}") {
        depth--;
        if (depth === 0) {
          return j;
        }
      }
    }
    return -1;
  }

  /** @param {string} text */
  function sanitizeAssistantStream(text) {
    let t = String(text || "").trim();
    if (!t) return "";
    if (t.startsWith('"') && !t.endsWith('"') && t.length < 400) {
      t = t.replace(/^"+/, "").trim();
    }
    function digitLikeRatio(s) {
      if (!s.length) return 0;
      let n = 0;
      for (const c of s) {
        if (/[\d.eE+\-]/.test(c)) n++;
      }
      return n / s.length;
    }
    function looksCorrupted(s) {
      const x = s.trim();
      if (x.length < 40) return false;
      if (/0{48,}/.test(x)) return true;
      if (x.length >= 120 && digitLikeRatio(x) > 0.75) return true;
      if (/Serving user request/i.test(x) && digitLikeRatio(x.slice(30)) > 0.6) return true;
      return false;
    }
    const numericRun = t.match(/([\d.eE+\-]{80,}|0{32,})/);
    if (numericRun && numericRun.index > 0) {
      t = t.slice(0, numericRun.index).trimEnd().replace(/^"+|"+$/g, "").trim();
    }
    if (looksCorrupted(t)) {
      const prefix = (t.split(/[\d.eE]{20,}/)[0] || "").trim().replace(/^"+|"+$/g, "").trim();
      if (prefix.length > 0 && prefix.length < 240 && !looksCorrupted(prefix)) return prefix;
      return "";
    }
    return t;
  }

  function stripFinalEnvelope(text) {
    let out = text;
    for (;;) {
      const i = out.indexOf("{");
      if (i < 0) {
        return out;
      }
      const end = matchJSONObject(out, i);
      if (end < 0) {
        const tail = out.slice(i);
        if (
          tail.includes('"patches"') ||
          (tail.includes('"type"') && tail.includes('"final"'))
        ) {
          return out.slice(0, i).trimEnd();
        }
        return out;
      }
      const blob = out.slice(i, end + 1);
      if (blob.includes('"patches"')) {
        out = (out.slice(0, i) + out.slice(end + 1)).trim();
        continue;
      }
      return out.slice(0, end + 1) + stripFinalEnvelope(out.slice(end + 1));
    }
  }
