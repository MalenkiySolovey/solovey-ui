/**
 * Owner-local production browser qualification for Telegram.
 * The neutral panel runner discovers this contribution through component.json.
 */
export async function run({ page, expect }) {
  await page.goto('telegram')
  await expect(page.getByText('Telegram is disabled by default.', { exact: false })).toBeVisible()
}
