const { expect } = require('@playwright/test');

async function observeApplication(page, { width = 1440, height = 960, theme = 'light', motion = 'reduce' } = {}) {
  await page.setViewportSize({ width, height });
  await page.emulateMedia({ colorScheme: theme, reducedMotion: motion });
  const faults = { pageErrors: [], businessRequests: [], externalRequests: [], sockets: [] };
  page.on('pageerror', (error) => faults.pageErrors.push(error.message));
  page.on('websocket', (socket) => faults.sockets.push(new URL(socket.url()).pathname));
  const origin = `http://127.0.0.1:${process.env.VUE_TEST_PORT || 5242}`;
  await page.route('**/*', (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.protocol === 'data:' || url.protocol === 'blob:') return route.continue();
    if (url.origin !== origin) {
      faults.externalRequests.push(`${request.method()} ${url.origin}${url.pathname}`);
      return route.abort();
    }
    if (!['GET', 'HEAD'].includes(request.method()) || ['fetch', 'xhr'].includes(request.resourceType()) || url.pathname.startsWith('/api/')) {
      faults.businessRequests.push(`${request.method()} ${url.pathname}`);
      return route.abort();
    }
    return route.continue();
  });
  return faults;
}

async function noHorizontalOverflow(page) {
  const dimensions = await page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    document: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }));
  expect(dimensions.document).toBeLessThanOrEqual(dimensions.viewport + 1);
  expect(dimensions.body).toBeLessThanOrEqual(dimensions.viewport + 1);
}

function noUnexpectedActivity(faults) {
  expect(faults).toEqual({ pageErrors: [], businessRequests: [], externalRequests: [], sockets: [] });
}

module.exports = { observeApplication, noHorizontalOverflow, noUnexpectedActivity };
