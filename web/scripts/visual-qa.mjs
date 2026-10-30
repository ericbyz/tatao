import { mkdir } from 'node:fs/promises'
import { join, resolve } from 'node:path'
import { chromium } from 'playwright-core'

const outputDir = resolve(process.argv[2] ?? '.qa-output')
const browserPath = process.env.BROWSER_PATH ?? 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe'
const baseURL = process.env.APP_URL ?? 'http://localhost:4173'

await mkdir(outputDir, { recursive: true })

const browser = await chromium.launch({ executablePath: browserPath, headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 960 }, deviceScaleFactor: 1 })
const issues = []
page.on('console', (message) => {
  if (message.type() === 'error' || message.type() === 'warning') issues.push(`${message.type()}: ${message.text()}`)
})
page.on('pageerror', (error) => issues.push(`pageerror: ${error.message}`))
page.on('response', (response) => {
  if (response.status() >= 400) issues.push(`http ${response.status()}: ${response.url()}`)
})

try {
  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: '项目', exact: true }).waitFor()
  const identity = { url: page.url(), title: await page.title() }

  await page.screenshot({ path: join(outputDir, 'desktop-projects.png'), fullPage: false })
  await page.setViewportSize({ width: 1536, height: 1024 })
  await page.waitForTimeout(100)
  await page.screenshot({ path: join(outputDir, 'native-projects.png'), fullPage: false })
  await page.setViewportSize({ width: 1440, height: 960 })
  await page.waitForTimeout(100)

  await page.getByRole('button', { name: '新建项目' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByText('本地文件夹', { exact: true }).waitFor()
  await page.screenshot({ path: join(outputDir, 'desktop-create-project.png'), fullPage: false })
  await dialog.getByRole('button', { name: '取消' }).click()

  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(250)
  const mobileSidebarTransform = await page.locator('.sidebar').evaluate((element) => getComputedStyle(element).transform)
  await page.screenshot({ path: join(outputDir, 'mobile-projects.png'), fullPage: false })

  await page.setViewportSize({ width: 1440, height: 960 })
  await page.waitForTimeout(250)
  await page.getByRole('button', { name: '新建项目' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '下一步' }).click()
  await page.getByLabel('项目名称').fill('视觉验证项目')
  await page.getByPlaceholder('选择或输入项目目录').fill('已授权 / visual-qa')
  await page.getByRole('dialog').getByRole('button', { name: '创建项目' }).click()
  await page.getByRole('cell', { name: '视觉验证项目', exact: true }).waitFor()

  await page.getByRole('button', { name: '测试环境', exact: true }).click()
  await page.getByRole('button', { name: '新增环境' }).click()
  const environmentDialog = page.getByRole('dialog')
  await page.screenshot({ path: join(outputDir, 'environment-create.png'), fullPage: false })
  await environmentDialog.getByLabel('环境名称').fill('视觉验证 Linux')
  await environmentDialog.getByLabel('主机地址').fill('127.0.0.1')
  await environmentDialog.getByLabel('账号').fill('qa')
  await environmentDialog.getByRole('textbox', { name: '密码', exact: true }).fill('qa-placeholder-not-a-secret')
  await environmentDialog.getByRole('button', { name: '保存环境' }).click()
  const environmentRow = page.getByRole('row').filter({ hasText: '视觉验证 Linux' })
  await environmentRow.getByText('未分配', { exact: true }).waitFor()
  await environmentRow.getByRole('button', { name: '分配项目' }).click()
  const assignmentDialog = page.getByRole('dialog')
  await page.screenshot({ path: join(outputDir, 'environment-assign.png'), fullPage: false })
  await assignmentDialog.getByRole('checkbox', { name: /视觉验证项目/ }).check()
  await assignmentDialog.getByRole('button', { name: '保存分配' }).click()
  await environmentRow.getByText('视觉验证项目', { exact: true }).waitFor()

  await page.getByRole('button', { name: '测试用例', exact: true }).click()
  await page.getByText('尚未添加测试用例。这里只会显示你创建或从 TAPD 同步的数据。', { exact: true }).waitFor()
  await page.screenshot({ path: join(outputDir, 'empty-test-cases.png'), fullPage: false })

  console.log(JSON.stringify({ identity, issues, dialogVerified: true, integratedFlows: ['create project', 'create unassigned Linux environment', 'assign environment to project', 'verify empty test cases'], mobileSidebarTransform, outputDir }, null, 2))
  if (identity.title !== 'AI Test Tool' || issues.length > 0 || mobileSidebarTransform === 'none') process.exitCode = 1
} finally {
  await browser.close()
}
