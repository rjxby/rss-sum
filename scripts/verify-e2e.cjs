"use strict";

const { spawn } = require("node:child_process");
const { closeSync, mkdirSync, mkdtempSync, openSync, rmSync } = require("node:fs");
const { createServer } = require("node:net");
const { tmpdir } = require("node:os");
const { join, resolve } = require("node:path");
const { setTimeout: delay } = require("node:timers/promises");

const root = resolve(__dirname, "..");
const children = new Set();
let interrupted = false;

function stop(child) {
    if (child.stopping || child.closed) return;
    child.stopping = true;
    const kill = signal => {
        try {
            if (process.platform === "win32") child.kill(signal);
            else process.kill(-child.pid, signal);
        } catch (error) {
            if (error.code !== "ESRCH") throw error;
        }
    };
    kill("SIGTERM");
    const deadline = setTimeout(() => kill("SIGKILL"), 5000);
    deadline.unref();
    child.done.then(() => clearTimeout(deadline));
}

for (const signal of ["SIGINT", "SIGTERM"]) {
    process.on(signal, () => {
        interrupted = true;
        for (const child of children) stop(child);
    });
}

function launch(command, args, options = {}) {
    if (interrupted) throw new Error("E2E verification interrupted");
    const child = spawn(command, args, { cwd: root, stdio: "inherit", detached: process.platform !== "win32", ...options });
    children.add(child);
    child.done = new Promise(resolveDone => {
        child.once("error", error => { child.failure = error; });
        child.once("close", (code, signal) => {
            child.closed = true;
            children.delete(child);
            resolveDone({ code, signal, error: child.failure });
        });
    });
    return child;
}

async function run(command, args, options) {
    const result = await launch(command, args, options).done;
    if (result.error) throw result.error;
    if (result.code !== 0) throw new Error(`${command} failed with ${result.signal || result.code}`);
}

async function freePort() {
    const listener = createServer();
    await new Promise((resolveListen, reject) => {
        listener.once("error", reject);
        listener.listen(0, "127.0.0.1", resolveListen);
    });
    const port = listener.address().port;
    await new Promise((resolveClose, reject) => listener.close(error => error ? reject(error) : resolveClose()));
    return port;
}

async function startApp(binary, temporary, artifacts, name) {
    const port = await freePort();
    const url = `http://127.0.0.1:${port}`;
    const descriptor = openSync(join(artifacts, `${name}-server.log`), "w");
    let child;
    try {
        // Running outside the checkout prevents loading the developer's .env file.
        child = launch(binary, [], {
            cwd: temporary,
            stdio: ["ignore", descriptor, descriptor],
            env: {
                ...process.env,
                RUN_MIGRATION: "false",
                HTTP_SERVER_ENABLED: "true",
                RSS_WORKER_ENABLED: "false",
                HTTP_ADDR: `127.0.0.1:${port}`,
                DATABASE_PATH: join(temporary, `${name}.sqlite`),
            },
        });
    } finally {
        closeSync(descriptor);
    }
    const deadline = Date.now() + 15_000;
    while (Date.now() < deadline) {
        if (interrupted || child.closed) throw new Error(`${name} server stopped before readiness; see ${name}-server.log`);
        try {
            const response = await fetch(`${url}/`, { signal: AbortSignal.timeout(1000) });
            if (response.ok && (await response.text()).includes("e2e-verification")) return url;
        } catch { /* Retry connection failures until readiness or the deadline. */ }
        await delay(100);
    }
    throw new Error(`${name} server did not become ready; see ${name}-server.log`);
}

async function main() {
    let cli;
    try {
        cli = require.resolve("@playwright/test/cli");
    } catch {
        throw new Error("Install E2E dependencies with npm ci, then npx playwright install chromium. See docs/e2e.md.");
    }
    const artifactsRoot = join(root, "artifacts", "e2e");
    mkdirSync(artifactsRoot, { recursive: true });
    const artifacts = mkdtempSync(join(artifactsRoot, "run-"));
    const temporary = mkdtempSync(join(tmpdir(), "rss-sum-e2e-"));
    console.log(`E2E artifacts: ${artifacts}`);
    try {
        const binary = join(temporary, process.platform === "win32" ? "rss-sum.exe" : "rss-sum");
        await run("go", ["build", "-ldflags=-X main.revision=e2e-verification", "-o", binary, "."]);
        const descriptor = openSync(join(artifacts, "seed.log"), "w");
        try {
            await run("go", ["run", "./scripts/e2e-seed", join(temporary, "populated.sqlite"), join(temporary, "empty.sqlite")], {
                stdio: ["ignore", descriptor, descriptor],
            });
        } finally {
            closeSync(descriptor);
        }
        const baseURL = await startApp(binary, temporary, artifacts, "populated");
        const emptyURL = await startApp(binary, temporary, artifacts, "empty");
        await run(process.execPath, [cli, "test", "--config", join(root, "playwright.config.cjs"), ...process.argv.slice(2)], {
            env: { ...process.env, E2E_BASE_URL: baseURL, E2E_EMPTY_URL: emptyURL, E2E_ARTIFACT_DIR: artifacts },
        });
    } finally {
        const active = [...children];
        for (const child of active) stop(child);
        await Promise.all(active.map(child => child.done));
        rmSync(temporary, { recursive: true, force: true });
        console.log(`E2E report and logs: ${artifacts}`);
    }
}

main().catch(error => {
    console.error(error.message);
    process.exitCode = interrupted ? 130 : 1;
});
