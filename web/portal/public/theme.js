(function () {
        try {
          var stored = localStorage.getItem("hubchat.portal.mode");
          var mode = stored ? JSON.parse(stored) : "auto";
          if (mode === "auto") {
            mode = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
          }
          document.documentElement.setAttribute("data-theme", mode);
        } catch (e) {}
      })();
