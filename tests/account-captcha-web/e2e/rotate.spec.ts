import { test, expect, type Page } from '@playwright/test'
import { solvePublicRotation } from './public-solver'
import { circularDistance, reachableDrag } from './drag-geometry'
declare const process: { env: { ACCOUNT_CAPTCHA_PASSWORD?: string } }
// An independent solver uses only the rendered public images. The server's
// private angle is never returned or fetched through a test-only answer API.
async function solve(page: Page) {
  const images = await page.evaluate(async () => {
    const read = async (selector: string) => {
      const img = document.querySelector<HTMLImageElement>(selector)!
      await img.decode()
      const canvas = document.createElement('canvas')
      canvas.width = img.naturalWidth; canvas.height = img.naturalHeight
      const ctx = canvas.getContext('2d')!; ctx.drawImage(img, 0, 0)
      return { rgba: Array.from(ctx.getImageData(0, 0, canvas.width, canvas.height).data), width: canvas.width, height: canvas.height }
    }
    return { master: await read('.gc-rotate-picture img'), thumb: await read('.gc-rotate-thumb-block img') }
  })
  expect(images.master.width).toBe(220); expect(images.master.height).toBe(220)
  expect(images.thumb.width).toBe(160); expect(images.thumb.height).toBe(160)
  return (await page.evaluate(solvePublicRotation, images)).angle
}
async function prepare(page: Page) {
 await page.goto('/'); await expect(page.getByRole('status', { name: 'Authentication status' })).toHaveText('Ready')
 await page.getByLabel('Password', { exact: true }).fill('incorrect fixture password 9234!')
 await page.getByRole('button', { name: 'Log in', exact: true }).click()
 await expect(page.getByRole('status', { name: 'Authentication status' })).toContainText('UNAUTHENTICATED')
 await page.getByLabel('Password', { exact: true }).fill(process.env.ACCOUNT_CAPTCHA_PASSWORD!)
 await page.getByRole('button', { name: 'Create challenge' }).click()
 await expect(page.locator('.gc-rotate-picture img')).toBeVisible()
 return solve(page)
}
async function assertLogin(page: Page) {
 await expect(page.getByRole('status', { name: 'Authentication status' })).toHaveText('Challenge verified')
 await expect(page.getByRole('button', { name: 'Log in', exact: true })).toBeFocused()
 await page.getByRole('button', { name: 'Log in', exact: true }).click()
 await expect(page.getByRole('status', { name: 'Authentication status' })).toHaveText('Logged in')
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
}
test('[desktop] official rotate drag → exact verification → login consumption, light', async ({ page }) => {
 await page.setViewportSize({ width: 1050, height: 900 }); await page.emulateMedia({ colorScheme: 'light' })
 const angle = await prepare(page)
 const bar = await page.locator('.gc-drag-slide-bar').boundingBox(), handle = await page.locator('.gc-drag-block').boundingBox()
 expect(bar).not.toBeNull(); expect(handle).not.toBeNull()
 const x = Math.round(handle!.x + handle!.width/2), y = Math.round(handle!.y + handle!.height/2)
 const geometry = await page.locator('.gc-drag-slide-bar').evaluate(el => {
   const block = el.querySelector<HTMLElement>('.gc-drag-block')!
   return { bar: (el as HTMLElement).offsetWidth, handle: block.offsetWidth, left: block.offsetLeft }
 })
 expect(geometry.left).toBe(0)
 const target = reachableDrag(angle, geometry.bar - geometry.handle)
 expect(circularDistance(angle, target.angle)).toBeLessThanOrEqual(1)
 const submitted = page.waitForRequest(r => r.method() === 'POST' && new URL(r.url()).pathname === '/fixture/verify')
 await page.mouse.move(x, y); await page.mouse.down()
 await page.mouse.move(x+target.pixel, y, { steps: 18 }); await page.mouse.up()
 const actual = (await submitted).postDataJSON().angle
 expect(actual).toBe(target.angle)
 console.log(JSON.stringify({ case: 'desktop', geometry, publicSolver: angle, targetPixel: target.pixel, actualPost: actual }))
 await assertLogin(page)
})
test('[keyboard] same official rotation, narrow dark and reduced motion', async ({ page }) => {
 await page.setViewportSize({ width: 360, height: 800 }); await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
 const angle = await prepare(page)
 const slider = page.getByRole('slider', { name: 'Rotation angle' }); await slider.focus(); await slider.press('Home')
 for (let i=0;i<angle;i++) await slider.press('ArrowRight')
 await expect(slider).toHaveValue(String(angle))
 await expect(page.locator('.gc-rotate-thumb-block')).toHaveCSS('transform', /matrix/)
 const submitted = page.waitForRequest(r => r.method() === 'POST' && new URL(r.url()).pathname === '/fixture/verify')
 await page.getByRole('button', { name: 'Verify angle' }).focus(); await page.keyboard.press('Enter')
 const actual = (await submitted).postDataJSON().angle
 expect(actual).toBe(angle)
 console.log(JSON.stringify({ case: 'keyboard', publicSolver: angle, actualPost: actual }))
 await assertLogin(page)
})
