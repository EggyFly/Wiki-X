(function () {
  "use strict";

  function fireInput(textarea) {
    textarea.dispatchEvent(new Event("input", { bubbles: true }));
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
    var csrf = form ? form.querySelector('input[name="csrf_token"]') : null;
    var previewLabel = toolbar.getAttribute("data-preview-label") || "预览";
    var closeLabel = toolbar.getAttribute("data-close-label") || "关闭预览";
    var errorLabel = toolbar.getAttribute("data-preview-error") || "预览加载失败.";
    var timer = null;
    var sequence = 0;

    var actions = {
      bold: function () { wrap(textarea, "**", "**", "粗体"); },
      italic: function () { wrap(textarea, "*", "*", "斜体"); },
      heading: function () { prefixLines(textarea, "## "); },
      link: function () { wrap(textarea, "[", "](https://)", "链接文字"); },
      list: function () { prefixLines(textarea, "- "); },
      quote: function () { prefixLines(textarea, "> "); },
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

    textarea.addEventListener("input", function () {
      if (preview.hidden) {
        return;
      }
      clearTimeout(timer);
      timer = setTimeout(renderPreview, 400);
    });

    toggle.addEventListener("click", function () {
      preview.hidden = !preview.hidden;
      toggle.setAttribute("aria-pressed", preview.hidden ? "false" : "true");
      toggle.textContent = preview.hidden ? previewLabel : closeLabel;
      if (!preview.hidden) {
        renderPreview();
      }
    });

    async function renderPreview() {
      var current = ++sequence;
      try {
        var params = new URLSearchParams();
        params.set("csrf_token", csrf ? csrf.value : "");
        params.set("body", textarea.value);
        var response = await fetch("/edit/preview", {
          method: "POST",
          headers: { "Content-Type": "application/x-www-form-urlencoded" },
          body: params.toString(),
          credentials: "same-origin"
        });
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
      } catch (error) {
        if (current === sequence) {
          preview.textContent = errorLabel;
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
        status.textContent = labels.uploading;
      }
      if (button) {
        button.disabled = true;
      }
      try {
        var response = await fetch(form.action, {
          method: "POST",
          body: new FormData(form),
          credentials: "same-origin"
        });
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
        }
      }
    });
  }

  initEditor();
  initUpload();
})();
