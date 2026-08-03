(() => {
  const dialog = document.querySelector("#command-palette");
  const trigger = document.querySelector("#command-trigger");
  const closeButton = document.querySelector("#command-close");
  const input = document.querySelector("#command-input");
  const empty = document.querySelector("#command-empty");
  const items = Array.from(document.querySelectorAll(".palette__item"));
  const countItems = Array.from(document.querySelectorAll("[data-count]"));
  const uploadMark = document.querySelector(".upload-mark");
  const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
  const narrowViewport = window.matchMedia("(max-width: 39.99rem)");
  let activeIndex = 0;

  if (!dialog || !trigger || !closeButton || !input) return;

  const visibleItems = () => items.filter((item) => !item.hidden);

  const setActive = (index) => {
    const visible = visibleItems();
    if (!visible.length) return;
    activeIndex = (index + visible.length) % visible.length;
    items.forEach((item) => item.classList.remove("is-active"));
    visible[activeIndex].classList.add("is-active");
    visible[activeIndex].scrollIntoView({ block: "nearest" });
  };

  const filterItems = () => {
    const query = input.value.trim().toLowerCase();
    items.forEach((item) => {
      item.hidden = !item.textContent.toLowerCase().includes(query);
    });
    empty.hidden = visibleItems().length !== 0;
    activeIndex = 0;
    setActive(0);
  };

  const open = () => {
    if (dialog.open) return;
    dialog.showModal();
    document.body.classList.add("is-locked");
    input.value = "";
    filterItems();
    input.focus();
  };

  const close = () => {
    if (!dialog.open) return;
    dialog.close();
  };

  const burstAt = (element) => {
    if (reducedMotion.matches) return;
    const rect = element.getBoundingClientRect();
    const burst = document.createElement("span");
    burst.className = "star-burst";
    burst.setAttribute("aria-hidden", "true");
    burst.style.left = `${rect.left + rect.width / 2}px`;
    burst.style.top = `${rect.top + rect.height / 2}px`;
    document.body.appendChild(burst);
    burst.addEventListener("animationend", () => burst.remove(), { once: true });
  };

  trigger.addEventListener("click", (event) => {
    open();
    burstAt(event.currentTarget);
    if (uploadMark) {
      uploadMark.classList.remove("is-excited");
      requestAnimationFrame(() => uploadMark.classList.add("is-excited"));
    }
  });
  closeButton.addEventListener("click", close);
  input.addEventListener("input", filterItems);

  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      dialog.open ? close() : open();
      return;
    }
    if (!dialog.open) return;
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActive(activeIndex + 1);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive(activeIndex - 1);
    } else if (event.key === "Enter") {
      const active = visibleItems()[activeIndex];
      if (active && document.activeElement === input) {
        event.preventDefault();
        active.click();
      }
    }
  });

  dialog.addEventListener("click", (event) => {
    if (event.target === dialog) close();
  });
  dialog.addEventListener("close", () => {
    document.body.classList.remove("is-locked");
    trigger.focus();
  });

  const revealCount = (element) => {
    const target = Number(element.dataset.count);
    const duration = 1200;
    const startedAt = performance.now();
    const tick = (now) => {
      const progress = Math.min((now - startedAt) / duration, 1);
      const eased = 1 - Math.pow(1 - progress, 3);
      element.textContent = String(Math.round(target * eased));
      if (progress < 1) {
        requestAnimationFrame(tick);
      } else {
        element.classList.add("did-count");
      }
    };
    requestAnimationFrame(tick);
  };

  if (!reducedMotion.matches && !narrowViewport.matches && "IntersectionObserver" in window) {
    countItems.forEach((item) => {
      item.textContent = "0";
    });
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        revealCount(entry.target);
        observer.unobserve(entry.target);
      });
    }, { threshold: 0.7 });
    countItems.forEach((item) => observer.observe(item));
  }
})();
