"use strict";

const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const test = require("node:test");
const { runInNewContext } = require("node:vm");

const script = readFileSync(join(__dirname, "../static/wall.js"), "utf8");

class TestElement {
    constructor(id, height = 0) {
        this.id = id;
        this.height = height;
        this.children = [];
        this.dataset = {};
        this.hidden = false;
        this._textContent = "";
        this.textWrites = 0;
        this.replacements = 0;
    }
    get textContent() { return this._textContent + this.children.map(child => child.textContent).join(""); }
    set textContent(value) {
        this.replaceChildren();
        this._textContent = value;
        this.textWrites++;
    }
    get childElementCount() { return this.children.length; }
    get childNodes() { return this.children; }
    append(...nodes) {
        for (const node of nodes) {
            node.remove();
            this.children.push(node);
            node.parent = this;
        }
    }
    replaceChildren(...nodes) {
        this.replacements++;
        this._textContent = "";
        for (const node of this.children) node.parent = null;
        this.children = [];
        this.append(...nodes);
    }
    remove() {
        if (this.parent) this.parent.children = this.parent.children.filter(node => node !== this);
        this.parent = null;
    }
    getBoundingClientRect() {
        return { height: this.children.reduce((height, child) => height + child.height, 0) };
    }
    cloneNode() {
        const clone = new TestElement(this.id, this.height);
        clone.dataset = { ...this.dataset };
        return clone;
    }
    isEqualNode(other) {
        return other && this.id === other.id && JSON.stringify(this.dataset) === JSON.stringify(other.dataset);
    }
    setAttribute() {}
    addEventListener() {}
}

async function startDigest(heights, { footerHeight = () => 0 } = {}) {
    let now = 1_000_000;
    let ready;
    let tick;
    let failNextRefresh = false;
    const ids = ["wall-digest", "wall-stories", "wall-message", "wall-edition", "display-toggle", "display-contrast", "posts-container"];
    const elements = Object.fromEntries(ids.map(id => [id, new TestElement(id)]));
    Object.defineProperty(elements["wall-digest"], "clientHeight", {
        get() {
            const edition = elements["wall-edition"];
            const labels = [edition.textContent, edition.dataset.reserveCount || "", edition.dataset.reserveError || ""];
            return 100 - Math.max(...labels.map(footerHeight));
        },
    });
    const articles = heights.map((height, i) => new TestElement(`story-${i + 1}`, height));
    const document = {
        documentElement: new TestElement("root"),
        hidden: false,
        getElementById: id => elements[id],
        querySelector: () => new TestElement("skip-link"),
        createElement: () => new TestElement("measure"),
        addEventListener(event, callback) {
            if (event === "DOMContentLoaded") ready = callback;
        },
    };
    runInNewContext(script, {
        document,
        localStorage: { getItem: () => null },
        location: { search: "?view=wall" },
        URLSearchParams,
        AbortController,
        Date: { now: () => now },
        window: { addEventListener() {} },
        setTimeout: () => 1,
        clearTimeout() {},
        clearInterval() {},
        setInterval(callback) { tick = callback; },
        fetch: async () => {
            if (failNextRefresh) {
                failNextRefresh = false;
                throw new Error("Unavailable");
            }
            return { ok: true, text: async () => "fixture" };
        },
        DOMParser: class {
            parseFromString() {
                return { querySelectorAll: () => articles, querySelector: () => null };
            }
        },
    });
    ready();
    await new Promise(resolve => setImmediate(resolve));
    return {
        stories: elements["wall-stories"],
        message: elements["wall-message"],
        edition: elements["wall-edition"],
        availableHeight: () => elements["wall-digest"].clientHeight - 6,
        storyHeight: () => elements["wall-stories"].getBoundingClientRect().height,
        numbers: () => elements["wall-stories"].children.map(article => article.dataset.dispatch),
        rotate() { now += 3 * 60 * 1000; tick(); },
        async refresh({ fail = false } = {}) {
            failNextRefresh = fail;
            now += 10 * 60 * 1000;
            tick();
            const requestedEdition = [...elements["wall-stories"].children];
            await new Promise(resolve => setImmediate(resolve));
            return requestedEdition;
        },
    };
}

test("oversized trailing stories do not blank a digest with a fitting story", async () => {
    const digest = await startDigest([40, 150, 150]);
    assert.deepEqual(digest.numbers(), ["01"]);
    const original = digest.stories.children[0];
    for (let rotation = 0; rotation < 3; rotation++) {
        digest.rotate();
        assert.deepEqual(digest.numbers(), ["01"]);
        assert.equal(digest.message.hidden, true);
        assert.equal(digest.stories.children[0], original);
    }
});

test("all oversized stories produce the screen-size message after bounded scans", async () => {
    const digest = await startDigest([150, 150, 150]);
    assert.deepEqual(digest.numbers(), []);
    assert.equal(digest.message.hidden, false);
    assert.match(digest.message.textContent, /too small for a complete summary/);
    digest.rotate();
    assert.deepEqual(digest.numbers(), []);
    assert.equal(digest.message.hidden, false);
});

test("rotation keeps source numbers ordered and wraps after the final edition", async () => {
    const digest = await startDigest([40, 40, 40]);
    assert.deepEqual(digest.numbers(), ["01", "02"]);
    digest.rotate();
    assert.deepEqual(digest.numbers(), ["03"]);
    digest.rotate();
    assert.deepEqual(digest.numbers(), ["01", "02"]);
});

test("mixed fitting and oversized stories rotate without dropping fitting stories", async () => {
    const digest = await startDigest([40, 150, 40, 150]);
    assert.deepEqual(digest.numbers(), ["01"]);
    digest.rotate();
    assert.deepEqual(digest.numbers(), ["03"]);
    digest.rotate();
    assert.deepEqual(digest.numbers(), ["01"]);
});

test("initial success fits stories within a wrapped footer's final height", async () => {
    const digest = await startDigest([40, 40, 40], {
        footerHeight: label => label ? 20 : 0,
    });
    assert.deepEqual(digest.numbers(), ["01"]);
    assert.equal(digest.edition.textContent, "1 story / 3 recent articles");
    assert.ok(digest.storyHeight() <= digest.availableHeight());
});

test("a narrow footer reserves the extra lines needed by refresh error labels", async () => {
    const digest = await startDigest([50, 30], {
        footerHeight: label => label.length > 35 ? 30 : 0,
    });
    assert.deepEqual(digest.numbers(), ["01"]);
    const available = digest.availableHeight();
    const requestedEdition = await digest.refresh({ fail: true });
    assert.equal(digest.edition.textContent, "Refresh unavailable · Showing the last edition");
    assert.equal(requestedEdition.length, 1);
    assert.equal(digest.stories.children[0], requestedEdition[0]);
    assert.equal(digest.message.hidden, true);
    assert.equal(digest.availableHeight(), available);
    assert.ok(digest.storyHeight() <= digest.availableHeight());
});

test("unchanged refreshes preserve story and status nodes with footer reservation", async () => {
    const digest = await startDigest([50], {
        footerHeight: label => label.length > 35 ? 30 : 0,
    });
    const original = digest.stories.children[0];
    const status = digest.edition.children[0];
    const replacements = digest.stories.replacements;
    const textWrites = status.textWrites;
    await digest.refresh();
    assert.equal(digest.stories.children[0], original);
    assert.equal(digest.stories.replacements, replacements);
    assert.equal(digest.edition.children[0], status);
    assert.equal(status.textWrites, textWrites);
    assert.ok(digest.storyHeight() <= digest.availableHeight());
});

function initialDisplay({ saved = null, search = "" } = {}) {
    const root = { dataset: {} };
    runInNewContext(script, {
        document: { documentElement: root, addEventListener() {} },
        localStorage: { getItem: () => saved },
        location: { search },
        URLSearchParams,
    });
    return root.dataset;
}

test("fresh visits start in the dark Wall digest shown in the docs", () => {
    const settings = initialDisplay();
    assert.equal(settings.display, "wall");
    assert.equal(settings.contrast, "dark");
});

test("saved reading preference and explicit view still select reading mode", () => {
    assert.equal(initialDisplay({ saved: '{"mode":"reading","contrast":"ink"}' }).display, "reading");
    assert.equal(initialDisplay({ search: "?view=reading" }).display, "reading");
});
