(function () {
  "use strict";

  function loadScript(src, onload) {
    var script = document.createElement("script");
    script.src = src;
    script.async = true;
    script.onload = onload;
    script.onerror = function () {
      console.error("failed to load " + src);
    };
    document.head.appendChild(script);
  }

  function initMermaid() {
    var name = document.documentElement.getAttribute("data-mermaid");
    var nodes = document.querySelectorAll(".mermaid");
    if (!name || !nodes.length) {
      return;
    }
    loadScript("/-/assets/" + name, function () {
      if (!window.mermaid) {
        return;
      }
      var dark = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
      window.mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        theme: dark ? "dark" : "default",
      });
      try {
        if (typeof window.mermaid.run === "function") {
          var running = window.mermaid.run({ nodes: Array.prototype.slice.call(nodes), suppressErrors: true });
          if (running && typeof running.catch === "function") {
            running.catch(function (err) {
              console.error("mermaid render failed", err);
            });
          }
        } else {
          window.mermaid.init(undefined, nodes);
        }
      } catch (err) {
        console.error("mermaid render failed", err);
      }
    });
  }

  function initMath() {
    var name = document.documentElement.getAttribute("data-katex");
    var nodes = document.querySelectorAll(".math");
    if (!name || !nodes.length) {
      return;
    }
    loadScript("/-/assets/" + name, function () {
      if (!window.katex) {
        return;
      }
      for (var i = 0; i < nodes.length; i++) {
        var node = nodes[i];
        try {
          window.katex.render(node.textContent, node, {
            displayMode: node.classList.contains("display"),
            throwOnError: false,
            output: "mathml",
            trust: false,
          });
        } catch (err) {
          console.error("math render failed", err);
        }
      }
    });
  }

  initMermaid();
  initMath();
})();
