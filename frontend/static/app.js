"use strict";

(() => {
    const feed = document.getElementById("posts-container");
    const requests = new Map();

    function requestSource(event) {
        const context = event.detail?.ctx;
        const source = requests.get(context) || context?.sourceElement;
        return source instanceof Element && source.hasAttribute("data-feed-request") ? source : null;
    }

    document.addEventListener("htmx:before:request", event => {
        const source = requestSource(event);
        if (!source) return;
        // HTMX can replace sourceElement during an outerHTML swap.
        requests.set(event.detail.ctx, source);
        feed.setAttribute("aria-busy", "true");
        source.classList.remove("is-error");
        source.classList.add("is-loading");
        const status = source.querySelector(".feed-status");
        status.setAttribute("role", "status");
        status.querySelector("[data-status-text]").textContent = status.dataset.loadingMessage;
        status.querySelector("[data-retry]").disabled = true;
    });

    function showError(event) {
        const source = requestSource(event);
        if (!source) return;
        source.classList.add("is-error");
        const status = source.querySelector(".feed-status");
        status.setAttribute("role", "alert");
        status.querySelector("[data-status-text]").textContent = source === feed
            ? "Couldn't load articles. Please try again."
            : "Couldn't load more articles. Please try again.";
        const retry = status.querySelector("[data-retry]");
        retry.hidden = false;
        retry.textContent = "Try again";
    }

    document.addEventListener("htmx:response:error", showError);
    document.addEventListener("htmx:error", showError);
    document.addEventListener("htmx:finally:request", event => {
        const source = requestSource(event);
        if (!source) return;
        requests.delete(event.detail.ctx);
        feed.setAttribute("aria-busy", String(requests.size > 0));
        source.classList.remove("is-loading");
        source.querySelectorAll("[data-retry]").forEach(button => { button.disabled = false; });
    });

    document.addEventListener("click", event => {
        const button = event.target.closest("[data-retry]");
        if (!button || button.disabled) return;
        const source = button.closest("[data-feed-request]");
        // Pagination retries must not reach the first-page loader above them.
        if (source && !source.classList.contains("is-loading")) htmx.trigger(source, "retry", {}, false);
    });
})();
