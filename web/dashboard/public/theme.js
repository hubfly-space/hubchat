(function () {
        try {
          var mode = JSON.parse(localStorage.getItem("hubchat.appearance.mode") || '"dark"');
          var density = JSON.parse(
            localStorage.getItem("hubchat.appearance.density") || '"comfortable"'
          );
          if (mode === "system") {
            mode = window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
          }
          document.documentElement.setAttribute("data-theme", mode);
          document.documentElement.setAttribute("data-density", density);
        } catch (e) {}
      })();
