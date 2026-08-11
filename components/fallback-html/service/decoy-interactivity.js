(() => {
  "use strict";

  const errorMessages = [
    "Invalid credentials or session expired. Please try again.",
    "Service temporarily unavailable. Code: 503.",
    "The request could not be completed at this time."
  ];

  function showFormError(form) {
    let message = form.querySelector(".decoy-injected-error");
    if (!message) {
      message = document.createElement("div");
      message.className = "decoy-injected-error";
      message.setAttribute("role", "status");
      message.style.cssText = "color:#842029;background:#f8d7da;border:1px solid #f5c2c7;padding:10px;margin-top:15px;border-radius:4px;font-size:14px;text-align:center";
      form.appendChild(message);
    }
    message.textContent = errorMessages[Math.floor(Math.random() * errorMessages.length)];
  }

  function makeFormsPassive() {
    document.querySelectorAll("form").forEach((form) => {
      form.addEventListener("submit", (event) => {
        event.preventDefault();
        const button = form.querySelector("button[type='submit'], input[type='submit']");
        const original = button ? (button.tagName === "INPUT" ? button.value : button.textContent) : "";
        if (button) {
          button.disabled = true;
          if (button.tagName === "INPUT") button.value = "Processing...";
          else button.textContent = "Processing...";
        }
        window.setTimeout(() => {
          if (button) {
            button.disabled = false;
            if (button.tagName === "INPUT") button.value = original;
            else button.textContent = original;
          }
          showFormError(form);
        }, 800 + Math.floor(Math.random() * 700));
      });
    });
  }

  function updateCounters() {
    const candidates = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.nodeValue.trim();
      if (!/^[\d,]+(?:\.\d+)?$/.test(text)) continue;
      const value = Number(text.replace(/,/g, ""));
      if (!Number.isFinite(value) || value < 10 || (value >= 1900 && value <= 2100 && text.length === 4)) continue;
      candidates.push({ node, value, decimals: text.includes(".") });
    }
    if (!candidates.length) return;
    window.setInterval(() => {
      const target = candidates[Math.floor(Math.random() * candidates.length)];
      const next = target.value * (0.95 + Math.random() * 0.1);
      target.node.nodeValue = (target.decimals ? next.toFixed(1) : Math.round(next).toString()).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
    }, 3000);
  }

  document.addEventListener("DOMContentLoaded", () => {
    makeFormsPassive();
    updateCounters();
  });
})();
