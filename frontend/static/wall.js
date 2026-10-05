"use strict";

(() => {
    const storageKey = "rss-sum-display";
    const root = document.documentElement;
    let settings = { mode: "wall", contrast: "dark" };
    try {
        const saved = JSON.parse(localStorage.getItem(storageKey));
        if (saved?.mode === "wall" || saved?.mode === "reading") settings.mode = saved.mode;
        if (saved?.contrast === "ink") settings.contrast = "ink";
    } catch { /* Storage can be unavailable on dedicated displays. */ }
    const view = new URLSearchParams(location.search).get("view");
    if (view === "reading") settings.mode = "reading";
    if (view === "wall" || view === "ink") {
        settings.mode = "wall";
        settings.contrast = view === "ink" ? "ink" : "dark";
    }
    root.dataset.display = settings.mode;
    root.dataset.contrast = settings.contrast;

    document.addEventListener("DOMContentLoaded", () => {
        const wall = document.getElementById("wall-digest");
        const stories = document.getElementById("wall-stories");
        const message = document.getElementById("wall-message");
        const edition = document.getElementById("wall-edition");
        const editionStatus = document.createElement("span");
        edition.replaceChildren(editionStatus);
        const toggle = document.getElementById("display-toggle");
        const contrast = document.getElementById("display-contrast");
        const feed = document.getElementById("posts-container");
        const skipLink = document.querySelector(".skip-link");
        const holdTime = 3 * 60 * 1000;
        const refreshTime = 10 * 60 * 1000;
        let articles = [];
        let offset = 0;
        let nextOffset = 0;
        let lastRefresh = 0;
        let lastEdition = 0;
        let timer;
        let controller;
        let refreshFailed = false;
        let resizeFrame;

        function reserveEditionSpace() {
            const labels = {
                reserveCount: articles.length ? `${articles.length} stories / ${articles.length} recent articles · Refresh unavailable` : "",
                reserveError: articles.length ? "Refresh unavailable · Showing the last edition" : "",
            };
            for (const [name, label] of Object.entries(labels)) {
                if (edition.dataset[name] !== label) edition.dataset[name] = label;
            }
        }

        function showEdition() {
            if (settings.mode !== "wall" || !articles.length) return;
            // Reserve every status before fitting so refresh failures keep the last edition visible.
            reserveEditionSpace();
            const measure = document.createElement("div");
            measure.className = "wall-measure";
            wall.append(measure);
            let count = 0;
            let cursor = offset;
            for (let scanned = 0; scanned < articles.length; scanned++, cursor = (cursor + 1) % articles.length) {
                if (cursor === 0 && count) break;
                const article = articles[cursor].cloneNode(true);
                article.dataset.dispatch = String(cursor + 1).padStart(2, "0");
                measure.append(article);
                if (measure.getBoundingClientRect().height > wall.clientHeight - 6) {
                    article.remove();
                    if (count) break;
                    continue;
                }
                count++;
            }
            nextOffset = cursor;
            if (count) {
                const unchanged = stories.childElementCount === count && Array.from(measure.children).every((article, i) => article.isEqualNode(stories.children[i]));
                if (!unchanged) stories.replaceChildren(...measure.childNodes);
                message.hidden = true;
                const label = `${count} ${count === 1 ? "story" : "stories"} / ${articles.length} recent articles${refreshFailed ? " · Refresh unavailable" : ""}`;
                if (editionStatus.textContent !== label) editionStatus.textContent = label;
            } else {
                stories.replaceChildren();
                message.hidden = false;
                message.textContent = "This screen is too small for a complete summary. Use reading mode or a larger display.";
                editionStatus.textContent = "";
            }
            measure.remove();
        }

        async function refresh() {
            if (controller || settings.mode !== "wall" || document.hidden) return;
            const request = new AbortController();
            controller = request;
            const timeout = setTimeout(() => request.abort(), 15000);
            wall.setAttribute("aria-busy", "true");
            try {
                const response = await fetch("/api/v1/posts?page=1&pageSize=30", {
                    headers: { "HX-Request": "true" },
                    cache: "no-store",
                    signal: request.signal,
                });
                if (!response.ok) throw new Error("Failed to load digest");
                const html = new DOMParser().parseFromString(await response.text(), "text/html");
                if (settings.mode !== "wall") return;
                const loaded = Array.from(html.querySelectorAll("article.story"));
                if (!loaded.length && !html.querySelector(".empty-state")) throw new Error("Invalid digest");
                const unchanged = loaded.length === articles.length && loaded.every((article, i) => article.isEqualNode(articles[i]));
                articles = loaded;
                reserveEditionSpace();
                lastRefresh = Date.now();
                refreshFailed = false;
                if (!unchanged) {
                    offset = 0;
                    lastEdition = Date.now();
                }
                if (articles.length) {
                    showEdition();
                } else {
                    stories.replaceChildren();
                    message.hidden = false;
                    message.textContent = "No articles yet. Summaries will appear after your feeds are processed.";
                    editionStatus.textContent = "";
                }
            } catch {
                if (settings.mode !== "wall") return;
                refreshFailed = true;
                if (stories.childElementCount) {
                    editionStatus.textContent = "Refresh unavailable · Showing the last edition";
                } else {
                    message.hidden = false;
                    message.textContent = "Couldn't load articles. Retrying automatically.";
                }
            } finally {
                clearTimeout(timeout);
                controller = null;
                wall.setAttribute("aria-busy", "false");
            }
        }

        function tick() {
            if (settings.mode !== "wall" || document.hidden) return;
            const now = Date.now();
            if (now - lastEdition >= holdTime && articles.length) {
                offset = nextOffset;
                lastEdition = now;
                showEdition();
            }
            if (now - lastRefresh >= (refreshFailed ? 60 * 1000 : refreshTime)) {
                lastRefresh = now;
                void refresh();
            }
        }

        function applyMode() {
            const active = settings.mode === "wall";
            root.dataset.display = settings.mode;
            root.dataset.contrast = settings.contrast;
            toggle.hidden = false;
            toggle.textContent = active ? "Reading mode" : "Wall digest";
            toggle.setAttribute("aria-pressed", String(active));
            contrast.hidden = !active;
            contrast.setAttribute("aria-pressed", String(settings.contrast === "ink"));
            wall.hidden = !active;
            feed.hidden = active;
            edition.hidden = !active;
            skipLink.href = active ? "#wall-digest" : "#latest-articles";
            clearInterval(timer);
            if (active) {
                lastEdition = Date.now();
                showEdition();
                void refresh();
                timer = setInterval(tick, 15000);
            } else {
                controller?.abort();
            }
        }

        function save() {
            try { localStorage.setItem(storageKey, JSON.stringify(settings)); } catch {}
        }
        toggle.addEventListener("click", () => {
            settings.mode = settings.mode === "wall" ? "reading" : "wall";
            const url = new URL(location.href);
            if (url.searchParams.has("view")) {
                url.searchParams.delete("view");
                history.replaceState(null, "", url);
            }
            save();
            applyMode();
        });
        contrast.addEventListener("click", () => {
            settings.contrast = settings.contrast === "ink" ? "dark" : "ink";
            root.dataset.contrast = settings.contrast;
            contrast.setAttribute("aria-pressed", String(settings.contrast === "ink"));
            save();
            showEdition();
        });
        window.addEventListener("resize", () => {
            cancelAnimationFrame(resizeFrame);
            resizeFrame = requestAnimationFrame(showEdition);
        });
        document.addEventListener("visibilitychange", tick);
        applyMode();
    });
})();
