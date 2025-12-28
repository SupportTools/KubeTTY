# Browser Automation in KubeTTY

KubeTTY project pods include pre-installed browser automation tools for AI browser agents, end-to-end testing, and web scraping.

## Available Frameworks

### Playwright

Microsoft's browser automation library supporting multiple browsers.

**Installed browsers:**
- Chromium
- Firefox
- WebKit (Safari engine)

**Quick test:**
```bash
node -e "
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const page = await browser.newPage();
  await page.goto('https://example.com');
  console.log('Title:', await page.title());
  await browser.close();
})();
"
```

### Puppeteer

Google's Node.js library for controlling Chrome/Chromium.

**Quick test:**
```bash
node -e "
const puppeteer = require('puppeteer');
(async () => {
  const browser = await puppeteer.launch({ args: ['--no-sandbox'] });
  const page = await browser.newPage();
  await page.goto('https://example.com');
  console.log('Title:', await page.title());
  await browser.close();
})();
"
```

## Required Launch Arguments

When running browsers in KubeTTY pods, always use these arguments:

```javascript
// Playwright
const browser = await chromium.launch({
  args: ['--no-sandbox', '--disable-setuid-sandbox']
});

// Puppeteer
const browser = await puppeteer.launch({
  args: ['--no-sandbox', '--disable-setuid-sandbox']
});
```

**Why these flags are required:**
- `--no-sandbox`: Disables Chrome's sandbox (required in containers)
- `--disable-setuid-sandbox`: Companion flag for sandbox disable

## Taking Screenshots

Both frameworks support headless screenshot capture without needing a display:

### Playwright
```javascript
const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ args: ['--no-sandbox'] });
  const page = await browser.newPage();
  await page.goto('https://example.com');

  // Save to file
  await page.screenshot({ path: 'screenshot.png' });

  // Or get as base64
  const base64 = await page.screenshot({ encoding: 'base64' });

  await browser.close();
})();
```

### Puppeteer
```javascript
const puppeteer = require('puppeteer');

(async () => {
  const browser = await puppeteer.launch({ args: ['--no-sandbox'] });
  const page = await browser.newPage();
  await page.goto('https://example.com');

  // Save to file
  await page.screenshot({ path: 'screenshot.png' });

  // Or get as base64
  const base64 = await page.screenshot({ encoding: 'base64' });

  await browser.close();
})();
```

## Claude Code MCP Integration

The browser tools enable Claude Code's MCP (Model Context Protocol) browser server to work within KubeTTY pods. This allows Claude to:

- Navigate to URLs
- Take screenshots for visual analysis
- Interact with page elements (click, type, select)
- Extract page content and DOM structure

Example MCP configuration in `.claude/settings.json`:
```json
{
  "mcpServers": {
    "puppeteer": {
      "command": "npx",
      "args": ["-y", "@anthropics/mcp-server-puppeteer"]
    }
  }
}
```

## Resource Configuration

### Shared Memory

Chromium requires adequate shared memory. KubeTTY pods mount a 2GB `/dev/shm` volume by default.

Adjust via Helm values:
```yaml
browser:
  shmSize: "4Gi"  # Increase for heavy browser workloads
```

### Memory Limits

Browser processes are memory-intensive:

| Workload | Recommended Memory |
|----------|-------------------|
| Single page | 512MB - 1GB |
| Multiple tabs | 1GB - 2GB |
| Heavy automation | 4GB - 8GB |

Adjust pod memory limits in values.yaml:
```yaml
resources:
  limits:
    memory: "12Gi"
```

## Troubleshooting

### Browser fails to launch

**Error:** `Failed to launch browser` or sandbox-related errors

**Solution:** Ensure `--no-sandbox` flag is used:
```javascript
await chromium.launch({ args: ['--no-sandbox'] });
```

### Out of memory

**Error:** Browser crashes or `SIGKILL`

**Solutions:**
1. Increase shared memory: `browser.shmSize: "4Gi"`
2. Increase pod memory limits
3. Close browser instances when done
4. Use single browser instance with multiple contexts

### Fonts rendering incorrectly

KubeTTY includes `fonts-liberation` and `fonts-noto-color-emoji` for proper text and emoji rendering. If you need additional fonts, install them to `/home/mmattox/.fonts/` and run `fc-cache -fv`.

### Slow performance

For better performance:
1. Use headless mode (default)
2. Disable unnecessary features:
   ```javascript
   await browser.launch({
     args: [
       '--no-sandbox',
       '--disable-dev-shm-usage',
       '--disable-gpu',
       '--disable-extensions'
     ]
   });
   ```
3. Close pages/browsers when not in use

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PLAYWRIGHT_BROWSERS_PATH` | `/root/.cache/ms-playwright` | Browser installation path |
| `PUPPETEER_SKIP_CHROMIUM_DOWNLOAD` | `true` | Skip Puppeteer's own Chromium download |

## Security Considerations

The `--no-sandbox` mode disables Chrome's security sandbox. This is acceptable for:
- Trusted workloads within the cluster
- Development/testing environments
- AI agent interactions with known sites

**Not recommended for:**
- Untrusted content at scale
- Public-facing automation services
- Processing user-submitted URLs without validation
