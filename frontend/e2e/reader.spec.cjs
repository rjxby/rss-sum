"use strict";

const { test: base, expect } = require("@playwright/test");

const test = base.extend({
    browserDiagnostics: [async ({ page, context }, use, testInfo) => {
        const errors = [];
        const diagnostics = [];
        page.on("pageerror", error => errors.push(error.message));
        page.on("console", message => diagnostics.push({ type: message.type(), text: message.text() }));
        page.on("requestfailed", request => diagnostics.push({ url: request.url(), failure: request.failure() }));
        const origins = new Set([process.env.E2E_BASE_URL, process.env.E2E_EMPTY_URL]);
        await context.route("**/*", route => {
            if (origins.has(new URL(route.request().url()).origin)) return route.continue();
            errors.push(`Unexpected external request: ${route.request().url()}`);
            return route.abort();
        });
        await use();
        await testInfo.attach("browser-diagnostics", {
            body: JSON.stringify({ errors, diagnostics }, null, 2), contentType: "application/json",
        });
        expect(errors, "uncaught browser errors or external requests").toEqual([]);
    }, { auto: true }],
});

const readingStories = page => page.locator("#posts-container article.story");
const wallStories = page => page.locator("#wall-stories article.story");

test("fresh visits use the documented dark Wall digest and retain a chosen reading view", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("html")).toHaveAttribute("data-display", "wall");
    await expect(page.locator("html")).toHaveAttribute("data-contrast", "dark");
    await expect(wallStories(page).first()).toBeVisible();
    await expect(page.locator("#posts-container")).toBeHidden();
    await page.getByRole("button", { name: "Reading mode", exact: true }).click();
    await expect(readingStories(page).first()).toBeVisible();
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-display", "reading");
    await expect(readingStories(page).first()).toBeVisible();
});

test("HTMX loads every page in order without duplicate articles", async ({ page }) => {
    await page.goto("/?view=reading");
    await expect(readingStories(page)).toHaveCount(10);
    for (const count of [20, 25]) {
        await page.locator(".pagination-sentinel").scrollIntoViewIfNeeded();
        await expect(readingStories(page)).toHaveCount(count);
    }
    await expect(page.locator(".pagination-sentinel")).toHaveCount(0);
    await expect(readingStories(page).locator(".story-title")).toHaveText(
        Array.from({ length: 25 }, (_, i) => `Fixture article ${String(i + 1).padStart(2, "0")}`),
    );
    await expect(page.locator("#posts-container")).toHaveAttribute("aria-busy", "false");
});

test("JSON and HTMX return the same stored titles, summaries, and source links", async ({ page, request }) => {
    const response = await request.get("/api/v1/posts?page=1&pageSize=10");
    expect(response.ok()).toBe(true);
    expect(response.headers()["content-type"]).toContain("application/json");
    const { posts } = await response.json();
    expect(posts).toHaveLength(10);
    expect(posts[0].id).toBe("e2e-01");
    const fragment = await request.get("/api/v1/posts?page=1&pageSize=10", { headers: { "HX-Request": "true" } });
    expect(fragment.ok()).toBe(true);
    expect(fragment.headers()["content-type"]).toContain("text/html");
    await page.goto("/?view=reading");
    await expect(readingStories(page)).toHaveCount(posts.length);
    const parseArticles = html => {
        const document = new DOMParser().parseFromString(html, "text/html");
        return [...document.querySelectorAll("article.story")].map(story => ({
            title: story.querySelector(".story-title").textContent,
            text: story.querySelector(".story-text").textContent,
            sourceUrl: story.querySelector(".story-link").getAttribute("href"),
        }));
    };
    const expected = posts.map(({ title, text, sourceUrl }) => ({ title, text, sourceUrl }));
    expect(await page.evaluate(parseArticles, await fragment.text())).toEqual(expected);
    expect(await page.evaluate(parseArticles, await page.locator("#posts-container").innerHTML())).toEqual(expected);
});

test("an empty SQLite database renders an empty state in both display modes", async ({ page }) => {
    await page.goto(process.env.E2E_EMPTY_URL);
    await expect(page.locator("#wall-message")).toHaveText("No articles yet. Summaries will appear after your feeds are processed.");
    await expect(wallStories(page)).toHaveCount(0);
    await page.getByRole("button", { name: "Reading mode", exact: true }).click();
    await expect(page.getByRole("heading", { name: "No articles yet" })).toBeVisible();
    await expect(readingStories(page)).toHaveCount(0);
    await expect(page.locator(".pagination-sentinel")).toHaveCount(0);
});

test("a failed initial request can be retried through the visible button", async ({ page }) => {
    let fail = true;
    await page.route("**/api/v1/posts?**", route => fail
        ? route.fulfill({ status: 500, contentType: "application/json", body: '{"error":"fixture outage"}' })
        : route.continue());
    await page.goto("/?view=reading");
    await expect(page.getByRole("alert")).toContainText("Couldn't load articles");
    await expect(page.locator("#posts-container")).toHaveAttribute("aria-busy", "false");
    fail = false;
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(readingStories(page)).toHaveCount(10);
    await expect(page.getByRole("alert")).toHaveCount(0);
});

test("a pagination failure keeps articles and retries only the failed page", async ({ page }) => {
    let fail = true;
    const requestedPages = [];
    await page.route("**/api/v1/posts?**", route => {
        const number = new URL(route.request().url()).searchParams.get("page");
        requestedPages.push(number);
        return number === "2" && fail
            ? route.fulfill({ status: 500, contentType: "application/json", body: '{"error":"fixture outage"}' })
            : route.continue();
    });
    await page.goto("/?view=reading");
    await expect(readingStories(page)).toHaveCount(10);
    await page.locator(".pagination-sentinel").scrollIntoViewIfNeeded();
    await expect(page.getByRole("alert")).toContainText("Couldn't load more articles");
    await expect(readingStories(page)).toHaveCount(10);
    fail = false;
    await page.getByRole("button", { name: "Try again", exact: true }).click();
    await expect(readingStories(page)).toHaveCount(20);
    expect(requestedPages).toEqual(["1", "2", "2"]);
    await expect(readingStories(page).locator(".story-title")).toHaveText(
        Array.from({ length: 20 }, (_, i) => `Fixture article ${String(i + 1).padStart(2, "0")}`),
    );
});

test("Wall digest and E-ink choices survive reload and can return to reading mode", async ({ page }) => {
    await page.goto("/");
    await expect(wallStories(page).first()).toBeVisible();
    await page.getByRole("button", { name: "E-ink", exact: true }).click();
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-display", "wall");
    await expect(page.locator("html")).toHaveAttribute("data-contrast", "ink");
    await expect(page.getByRole("button", { name: "E-ink", exact: true })).toHaveAttribute("aria-pressed", "true");
    await expect(wallStories(page).first()).toBeVisible();
    await page.getByRole("button", { name: "Reading mode", exact: true }).click();
    await expect(readingStories(page).first()).toBeVisible();
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-display", "reading");
    await expect(readingStories(page).first()).toBeVisible();
});

for (const viewport of [{ width: 390, height: 844 }, { width: 1280, height: 900 }]) {
    test(`summaries and controls fit at ${viewport.width}px in reading, wall, and ink modes`, async ({ page }) => {
        await page.setViewportSize(viewport);
        await page.goto("/?view=reading");
        await expect(readingStories(page)).toHaveCount(10);
        expect(await page.locator(".story-text").first().evaluate(element => element.scrollHeight <= element.clientHeight)).toBe(true);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
        await page.getByRole("button", { name: "Wall digest", exact: true }).click();
        await expect(wallStories(page).first()).toBeVisible();
        for (const ink of [false, true]) {
            if (ink) await page.getByRole("button", { name: "E-ink", exact: true }).click();
            await expect.poll(() => page.evaluate(() => {
                const wall = document.getElementById("wall-digest").getBoundingClientRect();
                const footer = document.querySelector(".site-footer").getBoundingClientRect();
                const header = document.querySelector(".site-header").getBoundingClientRect();
                const controls = [...document.querySelectorAll(".display-controls button")];
                const stories = [...document.querySelectorAll("#wall-stories .story")];
                return stories.length > 0 && stories.every(story => {
                    const rect = story.getBoundingClientRect();
                    const text = story.querySelector(".story-text");
                    return rect.top >= wall.top && rect.bottom <= wall.bottom + 1 &&
                        text.getBoundingClientRect().bottom <= rect.bottom + 1 && text.scrollHeight <= text.clientHeight;
                }) && controls.every(button => {
                    const rect = button.getBoundingClientRect();
                    return rect.left >= 0 && rect.right <= innerWidth && rect.top >= 0 && rect.bottom <= header.bottom;
                }) && header.bottom <= wall.top + 1 && wall.bottom <= footer.top + 1 &&
                    footer.bottom <= innerHeight + 1 && document.documentElement.scrollWidth <= innerWidth;
            })).toBe(true);
        }
        if (viewport.width === 390) {
            await expect(wallStories(page).locator(".story-title")).not.toContainText(["Fixture article 01"]);
        }
    });
}

test("Wall digest rotates, preserves a failed refresh, and recovers automatically", async ({ page }) => {
    await page.clock.install();
    await page.goto("/?view=wall");
    await expect(wallStories(page).first()).toBeVisible();
    const initial = await wallStories(page).locator(".story-title").allTextContents();
    await page.clock.fastForward(3 * 60 * 1000);
    await expect.poll(() => wallStories(page).locator(".story-title").allTextContents()).not.toEqual(initial);

    let release;
    const responseGate = new Promise(resolve => { release = resolve; });
    await page.route("**/api/v1/posts?page=1&pageSize=30", async route => {
        await responseGate;
        await route.fulfill({ status: 500, contentType: "application/json", body: '{"error":"fixture outage"}' });
    });
    const refresh = page.waitForRequest("**/api/v1/posts?page=1&pageSize=30");
    await page.clock.fastForward(10 * 60 * 1000);
    await refresh;
    const edition = await wallStories(page).allTextContents();
    release();
    await expect(page.locator("#wall-edition")).toContainText("Refresh unavailable");
    expect(await wallStories(page).allTextContents()).toEqual(edition);
    await page.unroute("**/api/v1/posts?page=1&pageSize=30");
    await page.clock.fastForward(60 * 1000);
    await expect(page.locator("#wall-edition")).not.toContainText("Refresh unavailable");
    await expect(wallStories(page).first()).toBeVisible();
});
