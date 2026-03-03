#!/usr/bin/env node
import { chromium } from "playwright";

const BASE_URL = process.env.KUBETTY_BASE_URL || "https://kubetty-dev.support.tools";
const USERNAME = process.env.KUBETTY_E2E_USERNAME;
const PASSWORD = process.env.KUBETTY_E2E_PASSWORD;
const PROJECT_NAME = process.env.KUBETTY_E2E_PROJECT || "Test Project";

if (!USERNAME || !PASSWORD) {
  console.error("Missing credentials. Set KUBETTY_E2E_USERNAME and KUBETTY_E2E_PASSWORD.");
  process.exit(2);
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function assertNoReconnect(page, timeoutMs) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const reconnectText = await page.getByText(/Reconnecting/i).count();
    if (reconnectText > 0) {
      throw new Error("Detected Reconnecting state in terminal overlay");
    }
    await sleep(250);
  }
}

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext();
  const page = await context.newPage();

  try {
    await page.goto(BASE_URL, { waitUntil: "domcontentloaded" });
    await page.getByRole("textbox", { name: "Username" }).fill(USERNAME);
    await page.getByRole("textbox", { name: "Password" }).fill(PASSWORD);
    await page.getByRole("button", { name: "Sign in" }).click();

    await page.getByRole("button", { name: "Open a tab" }).waitFor({ timeout: 15000 });
    await page.getByRole("button", { name: "Open a tab" }).click();
    await page.getByRole("button", { name: new RegExp(PROJECT_NAME, "i") }).first().click();

    await page.getByRole("textbox", { name: "Terminal input" }).fill("echo FIRST_TAB\n");

    await page.getByRole("button", { name: "+" }).click();
    await page.getByRole("button", { name: /New Session|New Shell/i }).first().click();

    await page.getByRole("textbox", { name: "Terminal input" }).fill("echo SECOND_TAB\n");

    await assertNoReconnect(page, 5000);

    console.log("PASS: second session stayed connected and accepted input");
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(`FAIL: ${err.message}`);
  process.exit(1);
});

