(function () {
  "use strict";

  function fireInput(textarea) {
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
  }

  function fetchWithTimeout(url, options, timeoutMs) {
    if (typeof AbortController === "undefined") {
      return fetch(url, options);
    }
    var controller = new AbortController();
    var timer = setTimeout(function () {
      controller.abort();
    }, timeoutMs);
    var request = Object.assign({}, options, { signal: controller.signal });
    return fetch(url, request).finally(function () {
      clearTimeout(timer);
    });
  }

  function wrap(textarea, before, after, placeholder) {
    var start = textarea.selectionStart || 0;
    var end = textarea.selectionEnd || 0;
    var selected = textarea.value.slice(start, end);
    var inner = selected || placeholder;
    textarea.setRangeText(before + inner + after, start, end, "end");
    if (!selected) {
      textarea.selectionStart = start + before.length;
      textarea.selectionEnd = start + before.length + placeholder.length;
    }
    fireInput(textarea);
  }

  function prefixLines(textarea, prefix) {
    var start = textarea.selectionStart || 0;
    var end = textarea.selectionEnd || 0;
    var value = textarea.value;
    var lineStart = value.lastIndexOf("\n", start - 1) + 1;
    var lineEnd = value.indexOf("\n", end);
    if (lineEnd === -1) {
      lineEnd = value.length;
    }
    var segment = value.slice(lineStart, lineEnd);
    var replacement = segment.split("\n").map(function (line) {
      return prefix + line;
    }).join("\n");
    textarea.setRangeText(replacement, lineStart, lineEnd, "end");
    textarea.selectionStart = lineStart;
    textarea.selectionEnd = lineStart + replacement.length;
    fireInput(textarea);
  }

  function insertBlock(textarea, text) {
    var start = textarea.selectionStart || 0;
    var end = textarea.selectionEnd || 0;
    var value = textarea.value;
    var before = start > 0 && value[start - 1] !== "\n" ? "\n\n" : "";
    var after = end < value.length && value[end] !== "\n" ? "\n\n" : "";
    textarea.setRangeText(before + text + after, start, end, "end");
    fireInput(textarea);
  }

  function initEditor() {
    var textarea = document.getElementById("body");
    var toolbar = document.querySelector(".md-toolbar");
    var preview = document.getElementById("preview");
    var toggle = document.getElementById("preview-toggle");
    if (!textarea || !toolbar || !preview || !toggle) {
      return;
    }
    var form = textarea.closest("form");
    var shell = textarea.closest(".editor-shell");
    var counter = document.getElementById("editor-count");
    var charsLabel = shell ? (shell.getAttribute("data-chars-label") || "字符数") : "字符数";
    var csrf = form ? form.querySelector('input[name="csrf_token"]') : null;
    var previewLabel = toolbar.getAttribute("data-preview-label") || "预览";
    var closeLabel = toolbar.getAttribute("data-close-label") || "关闭预览";
    var errorLabel = toolbar.getAttribute("data-preview-error") || "预览加载失败.";
    var timer = null;
    var sequence = 0;

    var actions = {
      bold: function () { wrap(textarea, "**", "**", "粗体"); },
      italic: function () { wrap(textarea, "*", "*", "斜体"); },
      strike: function () { wrap(textarea, "~~", "~~", "删除线"); },
      heading: function () { prefixLines(textarea, "## "); },
      link: function () { wrap(textarea, "[", "](https://)", "链接文字"); },
      list: function () { prefixLines(textarea, "- "); },
      task: function () { prefixLines(textarea, "- [ ] "); },
      quote: function () { prefixLines(textarea, "> "); },
      hr: function () { insertBlock(textarea, "---"); },
      code: function () {
        var selected = textarea.value.slice(textarea.selectionStart || 0, textarea.selectionEnd || 0);
        if (selected && selected.indexOf("\n") === -1) {
          wrap(textarea, "`", "`", "代码");
          return;
        }
        insertBlock(textarea, "```\n" + (selected || "代码") + "\n```");
      },
      table: function () {
        insertBlock(textarea, "| 列 1 | 列 2 |\n| --- | --- |\n| 内容 | 内容 |");
      },
      infobox: function () {
        insertBlock(textarea, "```wx-infobox\ntitle: 标题\nrows:\n  - label: 属性\n    value: 数值\n```");
      },
      notice: function () {
        insertBlock(textarea, "```wx-notice\ntype: info\ntext: 说明文字\n```");
      },
      gallery: function () {
        insertBlock(textarea, "```wx-gallery\nimages:\n  - image: /media/abc.png\n    caption: 说明\n```");
      },
      collapse: function () {
        insertBlock(textarea, "```wx-collapse\nitems:\n  - title: 问题\n    text: 答案\n```");
      },
      audio: function () {
        insertBlock(textarea, "```wx-audio\nsrc: /media/audio.mp3\ntitle: 音频标题\n```");
      }
    };

    toolbar.addEventListener("click", function (event) {
      var button = event.target.closest("button[data-md]");
      if (!button) {
        return;
      }
      var action = actions[button.getAttribute("data-md")];
      if (!action) {
        return;
      }
      action();
      textarea.focus();
    });

    function updateCount() {
      if (counter) {
        counter.textContent = charsLabel + ": " + textarea.value.length;
      }
    }

    textarea.addEventListener("input", function () {
      updateCount();
      if (preview.hidden) {
        return;
      }
      clearTimeout(timer);
      timer = setTimeout(renderPreview, 400);
    });

    textarea.addEventListener("keydown", function (event) {
      if (!(event.ctrlKey || event.metaKey) || event.altKey) {
        return;
      }
      var key = (event.key || "").toLowerCase();
      var shortcut = key === "b" ? "bold" : key === "i" ? "italic" : key === "k" ? "link" : "";
      if (!shortcut || !actions[shortcut]) {
        return;
      }
      event.preventDefault();
      actions[shortcut]();
    });

    toggle.addEventListener("click", function () {
      preview.hidden = !preview.hidden;
      toggle.setAttribute("aria-pressed", preview.hidden ? "false" : "true");
      toggle.textContent = preview.hidden ? previewLabel : closeLabel;
      if (shell) {
        shell.classList.toggle("preview-open", !preview.hidden);
      }
      if (!preview.hidden) {
        renderPreview();
      }
    });

    updateCount();

    var loadingLabel = preview.getAttribute("data-loading-label") || "预览加载中";

    function previewSkeleton() {
      return '<div class="skeleton skeleton-title"></div>' +
        '<div class="skeleton skeleton-line"></div>' +
        '<div class="skeleton skeleton-line mid"></div>' +
        '<div class="skeleton skeleton-line short"></div>' +
        '<div class="skeleton skeleton-block"></div>';
    }

    function setPreviewBusy(busy) {
      preview.classList.toggle("preview-loading", busy);
      if (busy) {
        preview.setAttribute("aria-busy", "true");
        preview.setAttribute("aria-label", loadingLabel);
      } else {
        preview.removeAttribute("aria-busy");
        preview.removeAttribute("aria-label");
      }
    }

    async function renderPreview() {
      var current = ++sequence;
      if (!preview.hidden) {
        if (preview.getAttribute("data-loaded") !== "1") {
          preview.innerHTML = previewSkeleton();
        }
        setPreviewBusy(true);
      }
      try {
        var params = new URLSearchParams();
        params.set("csrf_token", csrf ? csrf.value : "");
        params.set("body", textarea.value);
        var response = await fetchWithTimeout("/edit/preview", {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
          body: params.toString(),
          credentials: "same-origin"
        }, 15000);
        var data = await response.json().catch(function () {
          return {};
        });
        if (current !== sequence) {
          return;
        }
        if (!response.ok) {
          throw new Error(data.error || errorLabel);
        }
        preview.innerHTML = data.html || "";
        preview.setAttribute("data-loaded", "1");
        renderMath(preview);
        renderMermaid(preview);
        setPreviewBusy(false);
      } catch (error) {
        if (current === sequence) {
          preview.textContent = errorLabel;
          preview.setAttribute("data-loaded", "0");
          setPreviewBusy(false);
        }
      }
    }
  }

  function initUpload() {
    var form = document.getElementById("upload-form");
    if (!form) {
      return;
    }
    var status = document.getElementById("upload-status");
    var textarea = document.getElementById("body");
    var root = document.documentElement;
    var labels = {
      uploading: root.getAttribute("data-uploading") || "上传中...",
      inserted: root.getAttribute("data-uploaded") || "已插入",
      failed: root.getAttribute("data-upload-failed") || "上传失败"
    };
    if (!textarea) {
      return;
    }

    form.addEventListener("submit", async function (event) {
      event.preventDefault();
      var button = form.querySelector("button[type=submit]");
      if (status) {
        status.textContent = "";
        var bar = document.createElement("span");
        bar.className = "skeleton skeleton-inline";
        status.appendChild(bar);
        status.appendChild(document.createTextNode(labels.uploading));
      }
      if (button) {
        button.disabled = true;
        button.classList.add("is-loading");
        button.setAttribute("aria-busy", "true");
      }
      try {
        var response = await fetchWithTimeout(form.action, {
          method: "POST",
          body: new FormData(form),
          credentials: "same-origin"
        }, 30000);
        var data = await response.json().catch(function () {
          return {};
        });
        if (!response.ok) {
          throw new Error(data.error || labels.failed);
        }
        var markdown = "![图片](" + data.url + ")";
        var start = textarea.selectionStart === null ? textarea.value.length : textarea.selectionStart;
        var end = textarea.selectionEnd === null ? start : textarea.selectionEnd;
        textarea.value = textarea.value.slice(0, start) + markdown + textarea.value.slice(end);
        textarea.focus();
        textarea.selectionStart = start + markdown.length;
        textarea.selectionEnd = start + markdown.length;
        fireInput(textarea);
        if (status) {
          status.textContent = labels.inserted;
        }
      } catch (error) {
        if (status) {
          status.textContent = labels.failed;
        }
      } finally {
        if (button) {
          button.disabled = false;
          button.classList.remove("is-loading");
          button.removeAttribute("aria-busy");
        }
      }
    });
  }

  function resetLoadingButtons() {
    var buttons = document.querySelectorAll(".is-loading");
    for (var i = 0; i < buttons.length; i++) {
      buttons[i].classList.remove("is-loading");
      buttons[i].removeAttribute("aria-busy");
      if (buttons[i].tagName === "BUTTON") {
        buttons[i].disabled = false;
      }
    }
  }

  function initFormLoading() {
    document.addEventListener("submit", function (event) {
      var form = event.target;
      if (!form || form.tagName !== "FORM" || form.id === "upload-form") {
        return;
      }
      var method = (form.getAttribute("method") || "get").toLowerCase();
      if (method !== "post") {
        return;
      }
      var button = event.submitter || form.querySelector('button[type="submit"]');
      if (!button) {
        return;
      }
      button.classList.add("is-loading");
      button.setAttribute("aria-busy", "true");
      if (!button.name) {
        setTimeout(function () {
          button.disabled = true;
        }, 0);
      }
    });
    window.addEventListener("pageshow", function (event) {
      if (event.persisted) {
        resetLoadingButtons();
      }
    });
  }

  var mermaidPromise = null;
  var katexPromise = null;
  var mermaidReady = false;

  function loadScript(src) {
    return new Promise(function (resolve, reject) {
      var script = document.createElement("script");
      script.src = src;
      script.async = true;
      script.onload = resolve;
      script.onerror = function () {
        console.error("failed to load " + src);
        reject(new Error("failed to load " + src));
      };
      document.head.appendChild(script);
    });
  }

  function ensureMermaid() {
    if (window.mermaid) {
      return Promise.resolve(window.mermaid);
    }
    if (!mermaidPromise) {
      var name = document.documentElement.getAttribute("data-mermaid");
      if (!name) {
        return Promise.reject(new Error("mermaid asset is missing"));
      }
      mermaidPromise = loadScript("/-/assets/" + name).then(function () {
        return window.mermaid;
      });
    }
    return mermaidPromise;
  }

  function ensureKatex() {
    if (window.katex) {
      return Promise.resolve(window.katex);
    }
    if (!katexPromise) {
      var name = document.documentElement.getAttribute("data-katex");
      if (!name) {
        return Promise.reject(new Error("katex asset is missing"));
      }
      katexPromise = loadScript("/-/assets/" + name).then(function () {
        return window.katex;
      });
    }
    return katexPromise;
  }

  function renderMermaid(root) {
    if (!root || !root.querySelector(".mermaid")) {
      return;
    }
    ensureMermaid().then(function (mermaid) {
      if (!mermaidReady) {
        var dark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: dark ? "dark" : "default",
        });
        mermaidReady = true;
      }
      var nodes = root.querySelectorAll(".mermaid");
      if (!nodes.length) {
        return;
      }
      try {
        if (typeof mermaid.run === "function") {
          var running = mermaid.run({ nodes: Array.prototype.slice.call(nodes), suppressErrors: true });
          if (running && typeof running.catch === "function") {
            running.catch(function (err) {
              console.error("mermaid render failed", err);
            });
          }
        } else {
          mermaid.init(undefined, nodes);
        }
      } catch (err) {
        console.error("mermaid render failed", err);
      }
    }).catch(function (err) {
      console.error("mermaid load failed", err);
    });
  }

  function renderMath(root) {
    if (!root || !root.querySelector(".math")) {
      return;
    }
    ensureKatex().then(function (katex) {
      var nodes = root.querySelectorAll(".math");
      for (var i = 0; i < nodes.length; i++) {
        var node = nodes[i];
        if (node.querySelector("math")) {
          continue;
        }
        try {
          katex.render(node.textContent, node, {
            displayMode: node.classList.contains("display"),
            throwOnError: false,
            output: "mathml",
            trust: false,
          });
        } catch (err) {
          console.error("math render failed", err);
        }
      }
    }).catch(function (err) {
      console.error("math load failed", err);
    });
  }

  initEditor();
  initUpload();
  initFormLoading();
  renderMermaid(document);
  renderMath(document);
})();
