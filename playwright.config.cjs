"use strict";

const { defineConfig } = require("@playwright/test");
const { join } = require("node:path");

if (!process.env.E2E_BASE_URL || !process.env.E2E_EMPTY_URL || !process.env.E2E_ARTIFACT_DIR) {
    throw new Error("Run browser tests through make verify-e2e or npm run test:e2e.");
}

module.exports = defineConfig({
    testDir: "./frontend/e2e",
    testMatch: "*.spec.cjs",
    workers: 1,
    retries: 0,
    forbidOnly: true,
    timeout: 30_000,
    globalTimeout: 180_000,
    outputDir: join(process.env.E2E_ARTIFACT_DIR, "test-results"),
    reporter: [
        ["list"],
        ["html", { outputFolder: join(process.env.E2E_ARTIFACT_DIR, "report"), open: "never" }],
    ],
    use: {
        baseURL: process.env.E2E_BASE_URL,
        browserName: "chromium",
        viewport: { width: 1280, height: 900 },
        trace: "retain-on-failure",
        screenshot: "only-on-failure",
    },
});
