import {
  test as base,
  expect,
  type Locator,
  type Page,
} from "@playwright/test";
import { createHash, randomUUID } from "node:crypto";
import {
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  realpath,
  rm,
  writeFile,
} from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";

const closure = [
  "src/components/ui/UiDialog.vue",
  "src/components/ui/UiDrawer.vue",
  "src/components/ui/UiPopover.vue",
  "src/components/ui/UiButton.vue",
  "src/components/ui/UiIcon.vue",
  "src/components/ui/UiSpinner.vue",
  "src/components/ui/types.ts",
  "src/composables/useLayer.ts",
  "src/styles/tokens.css",
  "src/styles/base.css",
  "src/styles/components.css",
];

const shell = `<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import UiDialog from './src/components/ui/UiDialog.vue'
import UiDrawer from './src/components/ui/UiDrawer.vue'
import UiPopover from './src/components/ui/UiPopover.vue'
const query = new URLSearchParams(location.search)
const Surface = query.get('kind') === 'drawer' ? UiDrawer : UiDialog
const mode = query.get('mode') || 'ordinary'
const nativeTarget = query.get('target')
const outside = query.get('outside') !== 'false'
const escape = query.get('escape') !== 'false'
const open = ref(false), nested = ref(false), clicks = ref(0)
const events = ref<string[]>([])
const mounted = ref(true), lower = ref(mode === 'initial'), confirmation = ref(mode === 'initial')
const popover = ref(false), disabled = ref(false), generation = ref(0), detached = ref(false)
const nativeTrigger = ref<HTMLElement|null>(null), invalidTarget = ref(false), captured = ref(false)
const pageMounted = ref(true), pageOpen = ref(false), pageLayerMounted = ref(true)
const pageTitle = ref<HTMLElement|null>(null), pageTrigger = ref<HTMLElement|null>(null)
const pageGeneration = ref(0), pageDetached = ref(false), pageFocuses = ref(0), alternate = ref(false)
const pageNested = ref(false), nestedDisabled = ref(false)
const layerMounted = ref(true)
const fallbackState = query.get('fallback') || 'valid'
const pageFallback = computed(() => fallbackState === 'none' ? null : pageTitle.value)
async function restorePage() {
  const oldTrigger = pageTrigger.value
  pageMounted.value = false
  await nextTick()
  pageDetached.value = oldTrigger?.isConnected === false
  pageOpen.value = true
  pageMounted.value = true
  pageGeneration.value++
}
function confirmNative() {
  captured.value = document.activeElement === nativeTrigger.value
  confirmation.value = true
}
async function remount() {
  const oldTrigger = document.querySelector('[data-testid="confirmation-launch"]')
  mounted.value = false
  await nextTick()
  detached.value = oldTrigger?.isConnected === false
  lower.value = true
  confirmation.value = true
  mounted.value = true
  generation.value++
}
</script>
<template>
  <main style="padding:24px">
    <button data-testid="launch" @click="mode === 'ordinary' ? open = true : lower = true">Open surface</button>
    <label>Later input <input data-testid="later-input" /></label>
    <output data-testid="events">{{ JSON.stringify(events) }}</output>
    <Surface v-if="mode === 'ordinary'" v-model:open="open" title="Focus surface" :close-on-outside="outside" :close-on-escape="escape"
      @update:open="value => events.push('update:' + value)" @close="reason => events.push('close:' + reason)">
      <label>Inside input <input data-testid="inside-input" data-autofocus /></label>
      <button data-testid="inside-button" @click="clicks++">Clicked {{ clicks }}</button>
      <a data-testid="inside-link" href="#inside">Inside link</a>
      <p data-testid="inside-text">Selectable content</p>
      <button data-testid="nested-launch" @click="nested = true">Open nested</button>
      <UiDialog v-model:open="nested" title="Nested surface"
        @close="reason => events.push('nested:' + reason)">
        <input data-testid="nested-input" data-autofocus />
      </UiDialog>
      <template #footer="{ close }"><button data-testid="footer-close" @click="close">Done</button></template>
    </Surface>
    <UiPopover v-if="mode === 'popover-page'" v-model:open="popover" label="Page popover" data-testid="page-anchor">
      <template #default="{ close }"><button data-testid="page-popover-close" @click="close">Close page popover</button></template>
    </UiPopover>
    <section v-if="mode === 'page-fallback' && pageMounted">
      <component :is="alternate ? 'h2' : fallbackState === 'disabled' ? 'button' : 'h1'"
        :key="alternate ? 'new-title' : 'original-title'" ref="pageTitle" tabindex="-1"
        data-testid="page-title" :hidden="fallbackState === 'hidden'"
        :disabled="fallbackState === 'disabled'" @focus="pageFocuses++">Current page {{ pageGeneration }} {{ alternate ? 'new' : 'original' }}</component>
      <button ref="pageTrigger" data-testid="page-open" @click="pageOpen = true">Open page confirmation</button>
      <input data-testid="page-later" aria-label="Current page input" />
      <output data-testid="page-generation">{{ pageGeneration }}:{{ pageDetached }}</output>
      <output data-testid="page-focuses">{{ pageFocuses }}</output>
      <UiDialog v-if="pageLayerMounted" v-model:open="pageOpen" title="Page confirmation" :fallback-focus="pageFallback">
        <input data-testid="page-confirm-input" data-autofocus />
        <button data-testid="page-restore" @click="restorePage">Unmount and restore page</button>
        <button data-testid="page-replace-target" @click="alternate = true">Replace current heading</button>
        <button data-testid="page-destroy-layer" @click="pageLayerMounted = false">Destroy confirmation directly</button>
        <button data-testid="page-nested" :disabled="nestedDisabled" @click="pageNested = true">Open nested confirmation</button>
        <template #footer="{ close }"><button data-testid="page-close" @click="close">Continue editing</button></template>
      </UiDialog>
      <UiDialog v-model:open="pageNested" title="Nested page confirmation" :fallback-focus="pageFallback">
        <input data-testid="page-nested-input" data-autofocus />
        <button data-testid="page-disable-nested" @click="nestedDisabled = true">Disable nested trigger</button>
        <button data-testid="page-remove-lower" @click="pageLayerMounted = false">Remove lower confirmation</button>
      </UiDialog>
    </section>
  </main>
  <template v-if="mode === 'reverse-order'">
    <UiDialog v-model:open="confirmation" title="Earlier confirmation">
      <input data-testid="order-confirmation-input" data-autofocus />
      <button data-testid="order-remove-lower" @click="popover = false; layerMounted = false">Remove lower</button>
      <button data-testid="order-reopen-lower" @click="layerMounted = true; lower = true">Reopen lower</button>
      <template #footer="{ close }"><button data-testid="order-confirmation-close" @click="close">Close confirmation</button></template>
    </UiDialog>
    <Surface v-if="layerMounted" v-model:open="lower" title="Later lower">
      <input data-testid="order-lower-input" data-autofocus />
      <button data-testid="order-confirmation-launch" @click="confirmation = true">Open earlier confirmation</button>
      <UiPopover v-model:open="popover" label="Layer actions" data-testid="order-popover-anchor">
        <template #default="{ close }">
          <button data-testid="order-popover-confirm" @click="confirmation = true">Confirm from popover</button>
          <button data-testid="order-popover-close" @click="close">Close popover</button>
        </template>
      </UiPopover>
      <template #footer="{ close }"><button data-testid="order-lower-close" @click="close">Close lower</button></template>
    </Surface>
  </template>
  <template v-if="mode !== 'ordinary' && mode !== 'popover-page' && mode !== 'page-fallback' && mode !== 'reverse-order' && mounted">
    <Surface v-model:open="lower" title="Remaining modal">
      <input data-testid="lower-input" data-autofocus />
      <div v-if="mode === 'native-target'" :contenteditable="nativeTarget === 'editable-child' && invalidTarget ? 'true' : undefined">
        <span ref="nativeTrigger" data-testid="native-trigger"
          :tabindex="nativeTarget === 'editable-child' && !invalidTarget ? 0 : undefined"
          :contenteditable="nativeTarget === 'editable-host' ? 'true' : undefined"
          style="display:inline-block;padding:8px;border:1px solid currentColor" @click="confirmNative">Open confirmation</span>
      </div>
      <button v-else data-testid="confirmation-launch" @click="confirmation = true">Open confirmation</button>
      <UiPopover v-model:open="popover" label="Modal popover" data-testid="modal-anchor" :disabled="disabled">
        <template #default="{ close }">
          <button data-testid="disable-anchor" @click="disabled = true">Disable old anchor</button>
          <button data-testid="modal-popover-close" @click="close">Close modal popover</button>
        </template>
      </UiPopover>
      <input data-testid="lower-later" />
    </Surface>
    <UiDialog v-model:open="confirmation" title="Confirmation">
      <input data-testid="confirmation-input" data-autofocus />
      <template v-if="mode === 'native-target'">
        <button data-testid="invalidate-native" @click="invalidTarget = true">Change trigger state</button>
        <output data-testid="native-capture">{{ captured }}</output>
      </template>
      <button data-testid="remount" @click="remount">Unmount and restore both</button>
      <output data-testid="generation">{{ generation }}:{{ detached }}</output>
      <template #footer="{ close }"><button data-testid="confirmation-close" @click="close">Close confirmation</button></template>
    </UiDialog>
  </template>
</template>`;

type Harness = { origin: string; nonce: string };
const test = base.extend<{ ownOrigin: void }, { harness: Harness }>({
  harness: [
    async ({}, use, workerInfo) => {
      const webRoot = process.env.AGENTEAM_DIALOG_WEB_ROOT!;
      const runDir = process.env.AGENTEAM_DIALOG_RUN_DIR!;
      const webRequire = createRequire(join(webRoot, "package.json"));
      const floatingPackage = webRequire.resolve(
        "@floating-ui/vue/package.json",
      );
      const floating = JSON.parse(await readFile(floatingPackage, "utf8")) as {
        module: string;
      };
      const { createServer } = await import(
        pathToFileURL(webRequire.resolve("vite")).href
      );
      const { default: vue } = await import(
        pathToFileURL(webRequire.resolve("@vitejs/plugin-vue")).href
      );
      await mkdir(runDir, { recursive: true });
      const root = await mkdtemp(join(runDir, "component-"));
      const nonce = randomUUID();
      const inputs: Record<string, string> = {};
      for (const path of closure) {
        await mkdir(dirname(join(root, path)), { recursive: true });
        await copyFile(join(webRoot, path), join(root, path));
        inputs[path] = createHash("sha256")
          .update(await readFile(join(root, path)))
          .digest("hex");
      }
      await writeFile(join(root, "Harness.vue"), shell);
      await writeFile(
        join(root, "main.ts"),
        `import { createApp } from 'vue'; import Harness from './Harness.vue';
import './src/styles/tokens.css'; import './src/styles/base.css'; import './src/styles/components.css';
document.body.style.overflow = 'auto'; createApp(Harness).mount('#app');`,
      );
      await writeFile(
        join(root, "index.html"),
        `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="harness-nonce" content="${nonce}"></head><body><div id="app"></div><script type="module" src="/main.ts"></script></body></html>`,
      );
      const server = await createServer({
        configFile: false,
        root,
        publicDir: false,
        cacheDir: join(root, ".vite"),
        clearScreen: false,
        logLevel: "error",
        plugins: [vue()],
        resolve: {
          alias: {
            vue: webRequire.resolve("vue/dist/vue.esm-bundler.js"),
            "@floating-ui/vue": join(dirname(floatingPackage), floating.module),
          },
          dedupe: ["vue"],
        },
        server: {
          host: "127.0.0.1",
          port: 0,
          strictPort: true,
          hmr: false,
          cors: false,
          fs: { allow: [root, await realpath(join(webRoot, "node_modules"))] },
        },
      });
      const lifecycle = join(runDir, `server-${workerInfo.workerIndex}.json`);
      let origin = "";
      try {
        await server.listen();
        const address = server.httpServer!.address();
        if (!address || typeof address === "string")
          throw new Error("LOOPBACK_ADDRESS_REQUIRED");
        origin = `http://127.0.0.1:${address.port}`;
        await writeFile(
          lifecycle,
          JSON.stringify(
            { pid: process.pid, origin, nonce, inputs, closed: false },
            null,
            2,
          ),
        );
        await use({ origin, nonce });
      } finally {
        await server.close();
        await writeFile(
          lifecycle,
          JSON.stringify(
            {
              pid: process.pid,
              origin,
              nonce,
              inputs,
              closed: !server.httpServer?.listening,
            },
            null,
            2,
          ),
        );
        await rm(root, { recursive: true, force: true });
      }
    },
    { scope: "worker" },
  ],
  ownOrigin: [
    async ({ context, page, harness }, use, testInfo) => {
      const blocked: string[] = [];
      await context.route("**/*", async (route) => {
        if (new URL(route.request().url()).origin === harness.origin)
          await route.continue();
        else {
          blocked.push(route.request().url());
          await route.abort("blockedbyclient");
        }
      });
      await page.addInitScript(() => {
        const events: object[] = [];
        Object.assign(window, { pointerEvidence: events });
        const label = (node: EventTarget | null) =>
          node instanceof Element
            ? node.getAttribute("data-testid") ||
              node.id ||
              node.className ||
              node.tagName
            : null;
        for (const type of [
          "pointerdown",
          "mousedown",
          "mouseup",
          "click",
          "focusin",
          "focusout",
        ]) {
          for (const capture of [true, false]) {
            document.addEventListener(
              type,
              (event) => {
                if (events.length < 1000)
                  events.push({
                    type,
                    capture,
                    target: label(event.target),
                    active: label(document.activeElement),
                    defaultPrevented: event.defaultPrevented,
                  });
              },
              capture,
            );
          }
        }
      });
      try {
        await use();
      } finally {
        if (!page.isClosed()) {
          await testInfo.attach("pointer-events", {
            body: Buffer.from(
              JSON.stringify(
                {
                  blocked,
                  events: await page.evaluate(
                    () =>
                      (window as unknown as { pointerEvidence: object[] })
                        .pointerEvidence,
                  ),
                },
                null,
                2,
              ),
            ),
            contentType: "application/json",
          });
        }
        expect(
          blocked,
          "the component harness must not request other origins",
        ).toEqual([]);
      }
    },
    { auto: true },
  ],
});

async function visit(page: Page, harness: Harness, kind: string, options = "") {
  await page.goto(
    `${harness.origin}/?kind=${kind}&nonce=${harness.nonce}${options}`,
  );
  await expect(page.locator('meta[name="harness-nonce"]')).toHaveAttribute(
    "content",
    harness.nonce,
  );
  await expect(page.getByTestId("launch")).toBeVisible();
}

async function open(page: Page) {
  await page.getByTestId("launch").click();
  await expect(
    page.getByRole("dialog", { name: "Focus surface", exact: true }),
  ).toBeVisible();
  await expect(page.getByTestId("inside-input")).toBeFocused();
  await expect(page.locator("#app")).toHaveJSProperty("inert", true);
  await expect(page.locator("body")).toHaveCSS("overflow", "hidden");
}

async function clickOverlay(page: Page) {
  const box = await page.locator(".ui-overlay").last().boundingBox();
  if (!box) throw new Error("OVERLAY_BOX_REQUIRED");
  const point = { x: box.x + 2, y: box.y + 2 };
  expect(
    await page.evaluate(
      ({ x, y }) =>
        document.elementFromPoint(x, y) ===
        [...document.querySelectorAll(".ui-overlay")].at(-1),
      point,
    ),
    "coordinate must hit the actual top overlay",
  ).toBe(true);
  await page.mouse.click(point.x, point.y);
}

async function restored(page: Page) {
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByTestId("launch")).toBeFocused();
  await expect(page.locator("#app")).toHaveJSProperty("inert", false);
  await expect(page.locator("body")).toHaveCSS("overflow", "auto");
}

for (const kind of ["dialog", "drawer"]) {
  for (const width of [390, 1440]) {
    for (const reducedMotion of ["no-preference", "reduce"] as const) {
      test(`${kind} ${width} ${reducedMotion}: outside keeps trigger focus`, async ({
        page,
        harness,
      }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ reducedMotion });
        await visit(page, harness, kind);
        await open(page);
        await page.getByTestId("inside-input").click();
        await page.getByTestId("inside-input").fill("Editable inside");
        await expect(page.getByTestId("inside-input")).toHaveValue(
          "Editable inside",
        );
        await clickOverlay(page);
        await restored(page);
        await expect(page.getByTestId("events")).toHaveText(
          '["update:false","close:outside"]',
        );
        await page.getByTestId("later-input").click();
        await page.getByTestId("later-input").fill("User chose this input");
        // Observe beyond the accepted leave transition, so a late focus repair cannot pass.
        await page.waitForTimeout(250);
        await expect(page.getByTestId("later-input")).toBeFocused();
        for (const action of ["escape", "button", "footer"]) {
          await open(page);
          if (action === "escape") await page.keyboard.press("Escape");
          else if (action === "button")
            await page
              .getByRole("button", { name: "关闭", exact: true })
              .click();
          else await page.getByTestId("footer-close").click();
          await restored(page);
        }
        await expect(page.getByTestId("events")).toHaveText(
          '["update:false","close:outside","update:false","close:escape","update:false","close:action","update:false","close:action"]',
        );
      });
    }
  }

  test(`${kind}: disabled outside and Escape preserve editing and action`, async ({
    page,
    harness,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 });
    await visit(page, harness, kind, "&outside=false&escape=false");
    await open(page);
    await clickOverlay(page);
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(1);
    await expect(page.getByTestId("events")).toHaveText("[]");
    await page.getByTestId("inside-input").click();
    await page.getByTestId("inside-input").fill("Still editable");
    await expect(page.getByTestId("inside-input")).toBeFocused();
    await expect(page.getByTestId("inside-input")).toHaveValue(
      "Still editable",
    );
    await page.getByTestId("inside-button").click();
    await expect(page.getByTestId("inside-button")).toHaveText("Clicked 1");
    await page.getByTestId("inside-link").click();
    await expect(page).toHaveURL(/#inside$/);
    await expect(page.getByTestId("inside-link")).toBeFocused();
    await expect(page.getByTestId("events")).toHaveText("[]");
    await page.getByTestId("footer-close").click();
    await restored(page);
    await expect(page.getByTestId("events")).toHaveText(
      '["update:false","close:action"]',
    );
  });
}

test("two modal layers close only the top and restore the lower trigger", async ({
  page,
  harness,
}) => {
  await page.setViewportSize({ width: 390, height: 900 });
  await visit(page, harness, "dialog");
  await open(page);
  await page.getByTestId("nested-launch").click();
  await expect(page.getByTestId("nested-input")).toBeFocused();
  await expect(page.locator('[role="dialog"]')).toHaveCount(2);
  await expect(page.locator('[role="dialog"]').first()).toHaveJSProperty(
    "inert",
    true,
  );
  await clickOverlay(page);
  await expect(page.locator('[role="dialog"]')).toHaveCount(1);
  await expect(page.getByTestId("nested-launch")).toBeFocused();
  await expect(page.locator("#app")).toHaveJSProperty("inert", true);
  await expect(page.getByTestId("events")).toHaveText('["nested:outside"]');
  await page.keyboard.press("Escape");
  await restored(page);
  await expect(page.getByTestId("events")).toHaveText(
    '["nested:outside","update:false","close:escape"]',
  );
});

async function insideRemainingModal(page: Page) {
  const panel = page.getByRole("dialog", {
    name: "Remaining modal",
    exact: true,
  });
  await expect(panel).toBeVisible();
  await expect(panel).toHaveJSProperty("inert", false);
  await expect
    .poll(() => panel.evaluate((node) => node.contains(document.activeElement)))
    .toBe(true);
  await expect(page.locator("#app")).toHaveJSProperty("inert", true);
  await expect(page.locator("body")).toHaveCSS("overflow", "hidden");
}

async function pointerTarget(target: Locator) {
  await expect
    .poll(
      () =>
        target.evaluate((node) => {
          const box = node.getBoundingClientRect();
          const hit = document.elementFromPoint(
            box.left + box.width / 2,
            box.top + box.height / 2,
          );
          return !!hit && (hit === node || node.contains(hit));
        }),
      "the visible control must receive the native pointer",
    )
    .toBe(true);
}

for (const kind of ["dialog", "drawer"]) {
  for (const width of [390, 1440]) {
    for (const reducedMotion of ["no-preference", "reduce"] as const) {
      test(`${kind} ${width} ${reducedMotion}: activation order overrides Teleport order`, async ({
        page,
        harness,
      }) => {
        await page.setViewportSize({ width, height: 900 });
        await page.emulateMedia({ reducedMotion });
        await visit(page, harness, kind, "&mode=reverse-order");
        await page.getByTestId("launch").click();
        await expect(page.getByTestId("order-lower-input")).toBeFocused();
        const lower = page
          .locator(".ui-dialog")
          .filter({ has: page.getByTestId("order-lower-input") });
        const confirmation = page
          .locator(".ui-dialog")
          .filter({ has: page.getByTestId("order-confirmation-input") });
        const launch = page.getByTestId("order-confirmation-launch");
        const close = page.getByTestId("order-confirmation-close");
        await launch.click();
        await expect(
          page.getByTestId("order-confirmation-input"),
        ).toBeFocused();
        expect(
          await page.evaluate(() => {
            const panels = [...document.querySelectorAll(".ui-dialog")];
            return (
              panels[0]?.contains(
                document.querySelector(
                  '[data-testid="order-confirmation-input"]',
                ),
              ) &&
              panels[1]?.contains(
                document.querySelector('[data-testid="order-lower-input"]'),
              )
            );
          }),
        ).toBe(true);
        await expect(lower).toHaveJSProperty("inert", true);
        await expect(confirmation).toHaveJSProperty("inert", false);
        await pointerTarget(close);
        await close.click();
        await expect(confirmation).toHaveCount(0);
        await expect(launch).toBeFocused();
        await expect(lower).toHaveJSProperty("inert", false);
        // Reopening the earlier component must still put its whole backdrop on top.
        await launch.click();
        await expect(confirmation).toBeVisible();
        const overlay = confirmation.locator("..");
        const box = await overlay.boundingBox();
        if (!box) throw new Error("ORDER_OVERLAY_BOX_REQUIRED");
        const point = { x: box.x + 2, y: box.y + 2 };
        await expect
          .poll(() =>
            overlay.evaluate(
              (node, point) =>
                document.elementFromPoint(point.x, point.y) === node,
              point,
            ),
          )
          .toBe(true);
        await page.mouse.click(point.x, point.y);
        await expect(confirmation).toHaveCount(0);
        await expect(launch).toBeFocused();

        await page.getByTestId("order-popover-anchor").click();
        const popover = page.getByRole("dialog", {
          name: "Layer actions",
          exact: true,
        });
        const fromPopover = page.getByTestId("order-popover-confirm");
        await pointerTarget(fromPopover);
        await fromPopover.click();
        await expect(confirmation).toBeVisible();
        await expect(page.locator(".ui-popover")).toHaveJSProperty(
          "inert",
          true,
        );
        await pointerTarget(close);
        await close.click();
        await expect(confirmation).toHaveCount(0);
        await expect(fromPopover).toBeFocused();
        await expect(popover).toHaveJSProperty("inert", false);
        await pointerTarget(fromPopover);
        await fromPopover.click();
        await expect(
          page.getByTestId("order-confirmation-input"),
        ).toBeFocused();
        await page.getByTestId("order-remove-lower").click();
        await expect(lower).toHaveCount(0);
        await expect(page.locator(".ui-popover")).toHaveCount(0);
        await expect(confirmation).toHaveJSProperty("inert", false);
        await expect(page.getByTestId("order-remove-lower")).toBeFocused();
        const reopen = page.getByTestId("order-reopen-lower");
        await pointerTarget(reopen);
        await reopen.click();
        await expect(page.getByTestId("order-lower-input")).toBeFocused();
        await expect(confirmation).toHaveJSProperty("inert", true);
        await expect(lower).toHaveJSProperty("inert", false);
        for (const key of ["Tab", "Shift+Tab"]) {
          await page.keyboard.press(key);
          expect(
            await lower.evaluate((node) =>
              node.contains(document.activeElement),
            ),
          ).toBe(true);
        }
        const lowerClose = page.getByTestId("order-lower-close");
        await pointerTarget(lowerClose);
        await lowerClose.click();
        await expect(lower).toHaveCount(0);
        await expect(confirmation).toHaveJSProperty("inert", false);
        await expect(reopen).toBeFocused();
        await pointerTarget(close);
        await page.keyboard.press("Escape");
        await expect(page.locator(".ui-overlay")).toHaveCount(0);
        await expect(page.locator("#app")).toHaveJSProperty("inert", false);
        await expect(page.locator("body")).toHaveCSS("overflow", "auto");
      });
    }
  }
}

for (const kind of ["dialog", "drawer"]) {
  for (const width of [390, 1440]) {
    for (const reducedMotion of ["no-preference", "reduce"] as const) {
      for (const mode of ["initial", "remount"]) {
        test(`${kind} ${width} ${reducedMotion}: ${mode} modal restoration`, async ({
          page,
          harness,
        }) => {
          await page.setViewportSize({ width, height: 900 });
          await page.emulateMedia({ reducedMotion });
          for (const action of ["escape", "action", "outside"]) {
            await visit(page, harness, kind, `&mode=${mode}`);
            if (mode === "remount") {
              await page.getByTestId("launch").click();
              await page.getByTestId("confirmation-launch").click();
              await expect(
                page.getByTestId("confirmation-input"),
              ).toBeFocused();
              await page.getByTestId("remount").click();
              await expect(page.getByTestId("generation")).toHaveText("1:true");
            }
            await expect(page.getByTestId("confirmation-input")).toBeFocused();
            await expect(page.locator(".ui-dialog")).toHaveCount(2);
            // The controlled host registers/renders the confirmation last. A
            // wrong visual order must not be mistaken for a restoration defect.
            await expect(page.locator(".ui-dialog").last()).toContainText(
              "Confirmation",
            );
            await expect(page.locator(".ui-dialog").last()).toHaveJSProperty(
              "inert",
              false,
            );
            if (action === "escape") await page.keyboard.press("Escape");
            else if (action === "action")
              await page.getByTestId("confirmation-close").click();
            else await clickOverlay(page);
            await expect(page.locator(".ui-dialog")).toHaveCount(1);
            await insideRemainingModal(page);
            for (const key of ["Tab", "Shift+Tab"]) {
              await page.keyboard.press(key);
              await insideRemainingModal(page);
            }
            await page.getByTestId("lower-later").click();
            await page.waitForTimeout(250);
            await expect(page.getByTestId("lower-later")).toBeFocused();
            await page.keyboard.press("Escape");
            await expect(page.locator(".ui-dialog")).toHaveCount(0);
            await expect(page.locator("#app")).toHaveJSProperty("inert", false);
            await expect(page.locator("body")).toHaveCSS("overflow", "auto");
          }
        });
      }
    }
  }
}

test("popover without a modal retains its original anchor on Escape and action", async ({
  page,
  harness,
}) => {
  await visit(page, harness, "dialog", "&mode=popover-page");
  for (const action of ["escape", "action"]) {
    await page.getByTestId("page-anchor").click();
    await expect(page.getByTestId("page-popover-close")).toBeFocused();
    if (action === "escape") await page.keyboard.press("Escape");
    else await page.getByTestId("page-popover-close").click();
    await expect(page.locator(".ui-popover")).toHaveCount(0);
    await expect(page.getByTestId("page-anchor")).toBeFocused();
    await expect(page.locator("#app")).toHaveJSProperty("inert", false);
  }
});

test("top nonmodal popover restores its valid modal anchor and keeps Tab inside", async ({
  page,
  harness,
}) => {
  await visit(page, harness, "dialog", "&mode=popover-modal");
  await page.getByTestId("launch").click();
  for (const action of ["escape", "action", "tab"]) {
    await page.getByTestId("modal-anchor").click();
    await expect(page.getByTestId("disable-anchor")).toBeFocused();
    if (action === "escape") await page.keyboard.press("Escape");
    else if (action === "action")
      await page.getByTestId("modal-popover-close").click();
    else await page.keyboard.press("Tab");
    await expect(page.locator(".ui-popover")).toHaveCount(0);
    if (action !== "tab")
      await expect(page.getByTestId("modal-anchor")).toBeFocused();
    await insideRemainingModal(page);
  }
  await page.keyboard.press("Escape");
  await restored(page);
});

test("top nonmodal popover with a disabled anchor falls back within its remaining modal", async ({
  page,
  harness,
}) => {
  await visit(page, harness, "drawer", "&mode=popover-modal");
  await page.getByTestId("launch").click();
  await page.getByTestId("modal-anchor").click();
  await page.getByTestId("disable-anchor").click();
  await expect(page.getByTestId("modal-anchor")).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(page.locator(".ui-popover")).toHaveCount(0);
  await insideRemainingModal(page);
  await expect(page.getByTestId("modal-anchor")).not.toBeFocused();
  await page.keyboard.press("Escape");
  await restored(page);
});

for (const kind of ["dialog", "drawer"]) {
  for (const target of ["editable-child", "editable-host"]) {
    test(`${kind}: native ${target} focus restoration`, async ({
      page,
      harness,
    }) => {
      await visit(page, harness, kind, `&mode=native-target&target=${target}`);
      await page.getByTestId("launch").click();
      await expect(page.getByTestId("lower-input")).toBeFocused();
      await page.getByTestId("native-trigger").click();
      await expect(page.getByTestId("native-capture")).toHaveText("true");
      await expect(page.getByTestId("confirmation-input")).toBeFocused();
      await page.getByTestId("invalidate-native").click();
      const trigger = page.getByTestId("native-trigger");
      await expect(trigger).toHaveJSProperty("isContentEditable", true);
      await expect(trigger).toHaveJSProperty("tabIndex", -1);
      expect(await trigger.getAttribute("tabindex")).toBeNull();
      await page.keyboard.press("Escape");
      await expect(page.locator(".ui-dialog")).toHaveCount(1);
      await insideRemainingModal(page);
      if (target === "editable-host") await expect(trigger).toBeFocused();
      else await expect(page.locator(".dialog-header button")).toBeFocused();
      await page.keyboard.press("Escape");
      await restored(page);
    });
  }
}

async function restoredSinglePage(page: Page, harness: Harness, options = "") {
  await visit(page, harness, "dialog", `&mode=page-fallback${options}`);
  await page.getByTestId("page-open").click();
  await expect(page.getByTestId("page-confirm-input")).toBeFocused();
  const oldTrigger = await page.getByTestId("page-open").elementHandle();
  await page.getByTestId("page-restore").click();
  await expect(page.getByTestId("page-generation")).toHaveText("1:true");
  expect(await oldTrigger!.evaluate((node) => node.isConnected)).toBe(false);
  await expect(page.getByTestId("page-confirm-input")).toBeFocused();
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await expect(page.getByTestId("page-focuses")).toHaveText("0");
  return oldTrigger!;
}

async function pageLayerClosed(page: Page) {
  await expect(page.locator(".ui-overlay")).toHaveCount(0);
  await expect(page.locator("#app")).toHaveJSProperty("inert", false);
  await expect(page.locator("body")).toHaveCSS("overflow", "auto");
}

for (const width of [390, 1440]) {
  for (const reducedMotion of ["no-preference", "reduce"] as const) {
    test(`page fallback ${width} ${reducedMotion}: restored single confirmation returns to current title`, async ({
      page,
      harness,
    }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ reducedMotion });
      for (const action of ["escape", "action", "outside"]) {
        const oldTrigger = await restoredSinglePage(page, harness);
        if (action === "escape") await page.keyboard.press("Escape");
        else if (action === "action")
          await page.getByTestId("page-close").click();
        else await clickOverlay(page);
        await pageLayerClosed(page);
        await expect(page.getByTestId("page-title")).toBeFocused();
        expect(await oldTrigger.evaluate((node) => node.isConnected)).toBe(
          false,
        );
        await expect(page.getByTestId("page-focuses")).toHaveText("1");
        await page.keyboard.press("Tab");
        await expect(page.getByTestId("page-open")).toBeFocused();
        await page.getByTestId("page-later").click();
        await page.waitForTimeout(250);
        await expect(page.getByTestId("page-later")).toBeFocused();
        await oldTrigger.dispose();
      }
    });
  }
}

test("page fallback preserves the exact live trigger before the explicit target", async ({
  page,
  harness,
}) => {
  await visit(page, harness, "dialog", "&mode=page-fallback");
  await page.getByTestId("page-open").click();
  await expect(page.getByTestId("page-confirm-input")).toBeFocused();
  await page.keyboard.press("Escape");
  await pageLayerClosed(page);
  await expect(page.getByTestId("page-open")).toBeFocused();
  await expect(page.getByTestId("page-focuses")).toHaveText("0");
});

test("page fallback reads the replacement local ref at close", async ({
  page,
  harness,
}) => {
  const trigger = await restoredSinglePage(page, harness);
  const oldTitle = await page.getByTestId("page-title").elementHandle();
  await page.getByTestId("page-replace-target").click();
  await expect(page.getByTestId("page-title")).toHaveText("Current page 1 new");
  expect(await oldTitle!.evaluate((node) => node.isConnected)).toBe(false);
  await page.getByTestId("page-close").click();
  await pageLayerClosed(page);
  await expect(page.getByTestId("page-title")).toBeFocused();
  await expect(page.getByTestId("page-focuses")).toHaveText("1");
  await oldTitle!.dispose();
  await trigger.dispose();
});

for (const state of ["hidden", "disabled", "none"]) {
  test(`page fallback refuses a ${state} target without selecting another page control`, async ({
    page,
    harness,
  }) => {
    const trigger = await restoredSinglePage(
      page,
      harness,
      `&fallback=${state}`,
    );
    await page.keyboard.press("Escape");
    await pageLayerClosed(page);
    await expect(page.getByTestId("page-focuses")).toHaveText("0");
    await expect(page.getByTestId("page-open")).not.toBeFocused();
    await expect(page.getByTestId("page-title")).not.toBeFocused();
    await trigger.dispose();
  });
}

test("page fallback is not attempted by direct component destruction", async ({
  page,
  harness,
}) => {
  const trigger = await restoredSinglePage(page, harness);
  await page.getByTestId("page-destroy-layer").click();
  await pageLayerClosed(page);
  await expect(page.getByTestId("page-focuses")).toHaveText("0");
  await expect(page.getByTestId("page-title")).not.toBeFocused();
  await page.getByTestId("page-later").click();
  await page.waitForTimeout(250);
  await expect(page.getByTestId("page-later")).toBeFocused();
  await trigger.dispose();
});

test("page fallback cannot escape a remaining modal or steal focus on non-top removal", async ({
  page,
  harness,
}) => {
  await visit(page, harness, "dialog", "&mode=page-fallback");
  await page.getByTestId("page-open").click();
  await page.getByTestId("page-nested").click();
  await expect(page.getByTestId("page-nested-input")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("page-nested")).toBeFocused();
  await expect(page.getByTestId("page-focuses")).toHaveText("0");
  await page.getByTestId("page-nested").click();
  await page.getByTestId("page-disable-nested").click();
  await page.keyboard.press("Escape");
  const lower = page.getByRole("dialog", {
    name: "Page confirmation",
    exact: true,
  });
  await expect(lower).toBeVisible();
  expect(
    await lower.evaluate((node) => node.contains(document.activeElement)),
  ).toBe(true);
  await expect(page.getByTestId("page-focuses")).toHaveText("0");
  await page.keyboard.press("Tab");
  expect(
    await lower.evaluate((node) => node.contains(document.activeElement)),
  ).toBe(true);
  await page.keyboard.press("Escape");
  await pageLayerClosed(page);

  await visit(page, harness, "dialog", "&mode=page-fallback");
  await page.getByTestId("page-open").click();
  await page.getByTestId("page-nested").click();
  await page.getByTestId("page-remove-lower").click();
  const top = page.getByRole("dialog", {
    name: "Nested page confirmation",
    exact: true,
  });
  await expect(page.locator(".ui-dialog")).toHaveCount(1);
  expect(
    await top.evaluate((node) => node.contains(document.activeElement)),
  ).toBe(true);
  await expect(page.getByTestId("page-focuses")).toHaveText("0");
});
