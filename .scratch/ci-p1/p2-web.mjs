// Live browser and HTTP rows. Cookie/grant values are never printed or saved.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import { createHash, randomBytes } from 'node:crypto'
import { pathToFileURL } from 'node:url'

const { chromium, request } = await import(pathToFileURL(`${process.env.TMP}/browser/node_modules/playwright/index.mjs`))
const base = process.env.URL
const prefix = `${base}/apps/urgit/api/ci`
const cookies = fs.readFileSync(process.env.JAR, 'utf8').split('\n')
  .filter((line) => line && (!line.startsWith('#') || line.startsWith('#HttpOnly_')))
  .map((line) => {
    const [domain, , path, secure, expires, name, value] = line.replace(/^#HttpOnly_/, '').split('\t')
    return { domain, path, secure: secure === 'TRUE', expires: Number(expires) || -1, name, value, httpOnly: line.startsWith('#HttpOnly_'), sameSite: 'Lax' }
  })
const owner = await request.newContext({ storageState: { cookies, origins: [] } })
const anon = await request.newContext()
let browser
async function read(path) {
  const response = await owner.get(prefix + path, { timeout: 60000 })
  assert.equal(response.status(), 200, `GET ${path}`)
  return response.json()
}
async function action(body, status = 200) {
  const response = await owner.post(prefix + '/action', { data: body, timeout: 60000 })
  assert.equal(response.status(), status, `action ${body.action}`)
  return response.json()
}
async function openPage(repo, query) {
  browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, storageState: { cookies, origins: [] } })
  const page = await context.newPage()
  page.setDefaultTimeout(60000)
  const errors = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.goto(`${base}/apps/urgit/#/${encodeURIComponent(repo)}?${query}`, { waitUntil: 'domcontentloaded' })
  return { page, errors }
}
const settings = () => JSON.parse(fs.readFileSync(`${process.env.TMP}/p2-web-settings.json`, 'utf8'))
const [row, ...args] = process.argv.slice(2)
try {
  if (row === 'q14') {
    const cid = args[0]
    const candidate = await read(`/candidate/${cid}`)
    const jobs = candidate.attempts.filter((a) => a.kind === 'job')
    assert.equal(candidate.plan.length, 8)
    assert.equal(jobs.length, 8)
    assert.ok(jobs.every((a) => a.status === 'passed' && a.log))
    const listing = await read(`/repository/${candidate.repository}/candidates`)
    assert.ok(listing.candidates.some((c) => c.id === cid))
    const { page, errors } = await openPage(candidate.repository, 'tab=ci')
    const card = page.locator('.ci-candidate').filter({ hasText: candidate.head.slice(0, 8) })
    await card.waitFor()
    assert.equal(await card.locator('.ci-pip').count(), 8)
    await card.click()
    await page.locator('.ci-jobs tbody tr').first().waitFor()
    assert.equal(await page.locator('.ci-jobs tbody tr').count(), 8)
    assert.equal(await page.locator('.ci-jobs').getByRole('button', { name: 'Log', exact: true }).count(), 8)
    assert.equal(await page.locator('.ci-jobs .ci-status-passed').count(), 8)
    assert.equal(await page.locator('.ci-verdict').innerText(), 'Landed')
    await page.screenshot({ path: `${process.env.TMP}/q14-jobs.png`, fullPage: true })
    await page.locator('.ci-jobs').getByRole('button', { name: 'Log', exact: true }).first().click()
    await page.locator('.ci-log-line').first().waitFor()
    assert.ok((await page.locator('.ci-log').innerText()).includes('['))
    const logCount = await page.locator('.ci-log-line').count()
    const logURL = page.url()
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.locator('.ci-log-line').first().waitFor()
    assert.equal(page.url(), logURL)
    await page.screenshot({ path: `${process.env.TMP}/q14-log.png`, fullPage: true })
    await page.getByRole('button', { name: '← Jobs', exact: true }).click()
    await page.locator('.ci-jobs').waitFor()
    await page.goBack()
    await page.locator('.ci-log').waitFor()
    assert.deepEqual(errors, [])
    console.log('Q14 PASS:', cid, '8 job rows, 8 passed statuses, 8 log links; rendered', logCount, 'lines; log deep link/reload/back work')
  } else if (row === 'q15') {
    const { repo } = settings()
    const policyPath = `/repository/${repo}/policy`
    await action({ action: 'set-ci-protected', repo, ref: 'refs/heads/master', protected: true })
    await action({ action: 'set-untrusted-policy', repo, policy: 'approval' })
    const { page, errors } = await openPage(repo, 'tab=settings')
    const toggle = page.getByRole('checkbox', { name: 'CI required: master', exact: true })
    await toggle.waitFor()
    assert.equal(await toggle.isChecked(), true)
    await toggle.click()
    await page.waitForFunction(() => !document.querySelector('[aria-label="CI required: master"]').disabled)
    assert.equal((await read(policyPath)).protectedRefs.includes('refs/heads/master'), false)
    await page.reload({ waitUntil: 'domcontentloaded' })
    await toggle.waitFor()
    assert.equal(await toggle.isChecked(), false)
    await toggle.click()
    await page.waitForFunction(() => !document.querySelector('[aria-label="CI required: master"]').disabled)
    assert.equal((await read(policyPath)).protectedRefs.includes('refs/heads/master'), true)
    await page.getByRole('radio', { name: /Run restricted checks/ }).click()
    await page.waitForFunction(() => !document.querySelector('[aria-label="CI required: master"]').disabled)
    assert.equal((await read(policyPath)).untrusted, 'restricted')
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.getByRole('radio', { name: /Run restricted checks/ }).waitFor()
    assert.equal(await page.getByRole('radio', { name: /Run restricted checks/ }).isChecked(), true)

    const secret = 'web-' + randomBytes(24).toString('hex')
    const input = page.getByLabel('Credential value', { exact: true })
    await page.getByLabel('Credential name', { exact: true }).fill('WEB_TEST_SECRET')
    await input.fill(secret)
    assert.equal((await page.content()).includes(secret), false, 'password is absent from serialized DOM before submit')
    await page.getByLabel('Credential scope', { exact: true }).selectOption('env')
    await page.getByLabel('Credential environments', { exact: true }).fill('production, staging')
    await page.getByRole('button', { name: 'Add credential', exact: true }).click()
    assert.equal(await input.inputValue() === '', true, 'password cleared on submit')
    await page.getByRole('button', { name: 'Delete credential WEB_TEST_SECRET', exact: true }).waitFor()
    const policy = await read(policyPath)
    const credential = policy.credentials.find((c) => c.name === 'WEB_TEST_SECRET')
    assert.equal(credential.scope, 'env')
    assert.deepEqual(credential.envs.sort(), ['production', 'staging'])
    assert.equal(JSON.stringify(policy).includes(secret), false)
    assert.equal((await page.content()).includes(secret), false)
    await page.reload({ waitUntil: 'domcontentloaded' })
    await page.getByRole('button', { name: 'Delete credential WEB_TEST_SECRET', exact: true }).click()
    await page.getByRole('button', { name: 'Delete credential WEB_TEST_SECRET', exact: true }).waitFor({ state: 'detached' })
    assert.equal((await read(policyPath)).credentials.some((c) => c.name === 'WEB_TEST_SECRET'), false)
    assert.equal((await page.content()).includes(secret), false)

    let posts = 0
    page.on('request', (req) => { if (req.url() === prefix + '/action' && req.method() === 'POST') posts++ })
    await input.evaluate((element) => {
      const clipboardData = new DataTransfer()
      clipboardData.setData('text/plain', 'invalid\ncredential')
      element.dispatchEvent(new ClipboardEvent('paste', { clipboardData, bubbles: true, cancelable: true }))
    })
    await page.getByRole('alert').filter({ hasText: 'single line' }).waitFor()
    assert.equal(posts, 0)
    assert.equal(await input.inputValue() === '', true)
    await page.getByRole('radio', { name: 'Require approval before running', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('[aria-label="CI required: master"]').disabled)
    assert.equal((await read(policyPath)).untrusted, 'approval')
    assert.deepEqual(errors, [])
    await page.screenshot({ path: `${process.env.TMP}/q15-settings.png`, fullPage: true })
    console.log('Q15 PASS:', repo, 'CI toggle and policy survive reload; scoped credential added/deleted; password absent from page source; multiline paste refused before POST')
  } else if (row === 'q16') {
    const [phase, cid] = args
    const candidate = await read(`/candidate/${cid}`)
    const attempt = candidate.attempts.find((a) => a.log)
    assert.ok(attempt)
    const paths = [`/repository/${candidate.repository}/candidates`, `/candidate/${cid}`, `/repository/${candidate.repository}/policy`, '/key', `/attempt/${attempt.attempt}/log`]
    for (const path of paths) {
      const bare = await anon.get(prefix + path, { maxRedirects: 0, timeout: 60000 })
      assert.equal(bare.status(), phase === 'red' ? (path.endsWith('/log') ? 302 : 200) : 401, `anonymous ${path}`)
      const allowed = await owner.get(prefix + path, { maxRedirects: 0, timeout: 60000 })
      assert.equal(allowed.status(), path.endsWith('/log') ? 302 : 200, `session ${path}`)
      console.log(path, 'anonymous', bare.status(), 'session', allowed.status())
    }
    const daemonRead = await anon.get(prefix + `/attempt/${attempt.attempt}`)
    assert.equal(daemonRead.status(), 401)
    assert.equal((await owner.get(prefix + `/attempt/${attempt.attempt}`)).status(), 200)
    const body = { action: 'set-untrusted-policy', repo: settings().repo, policy: 'approval' }
    assert.equal((await anon.post(prefix + '/action', { data: body })).status(), phase === 'red' ? 200 : 401)
    assert.equal((await owner.post(prefix + '/action', { data: body })).status(), 200)
    console.log(phase === 'red' ? 'Q16 RED: anonymous CI reads and action accepted' : 'Q16 GREEN: every web read and action requires a session; daemon attempt reader retains its own authorization')
  } else if (row === 'actions') {
    const fixture = JSON.parse(fs.readFileSync(`${process.env.TMP}/p2-web-actions.json`, 'utf8'))
    const original = await read(`/candidate/${fixture.cid}`)
    assert.equal(original.trust, 'untrusted')
    assert.equal(original.attempts.length, 0)
    const { page, errors } = await openPage(fixture.repo, `tab=ci&candidate=${fixture.cid}`)
    await page.getByRole('button', { name: 'Rerun checks', exact: true }).click()
    await page.waitForURL((url) => url.hash.includes('candidate=') && !url.hash.includes(fixture.cid))
    let cid = new URLSearchParams(page.url().split('?')[1]).get('candidate')
    const rerun = await read(`/candidate/${cid}`)
    assert.equal(rerun.trust, 'untrusted')
    assert.equal(rerun.head, original.head)
    assert.equal(rerun.base, original.base)
    assert.equal(rerun.attempts.length, 0)
    assert.equal((await read(`/candidate/${fixture.cid}`)).status, 'skipped')
    const ids = [fixture.cid, cid]
    for (let i = 0; i < 52; i++) {
      cid = (await action({ action: 'rerun-candidate', id: cid }, 202)).candidate
      ids.push(cid)
    }
    const path = `/repository/${fixture.repo}/candidates`
    const first = await read(path)
    assert.equal(first.candidates.length, 50)
    assert.equal(first.candidates[0].id, cid)
    assert.equal(first.next, first.candidates.at(-1).id)
    const second = await read(`${path}?before=${first.next}`)
    assert.equal(second.candidates.length, 4)
    assert.equal(second.next, null)
    const all = [...first.candidates, ...second.candidates]
    assert.equal(new Set(all.map((c) => c.id)).size, 54)
    assert.deepEqual(all.map((c) => c.id).sort(), ids.sort())
    assert.ok(all.every((c, i) => i === 0 || c.created <= all[i - 1].created))
    assert.equal((await owner.get(prefix + path + '?before=invalid')).status(), 400)
    assert.equal((await owner.get(prefix + path + '?before=0v0')).status(), 404)
    await page.getByRole('button', { name: '← Candidates', exact: true }).click()
    await page.waitForFunction(() => document.querySelectorAll('.ci-candidate').length === 50)
    await page.getByRole('button', { name: 'Older candidates →', exact: true }).click()
    await page.waitForFunction(() => document.querySelectorAll('.ci-candidate').length === 4)
    await page.getByRole('button', { name: 'Newest', exact: true }).click()
    await page.waitForFunction(() => document.querySelectorAll('.ci-candidate').length === 50)
    await page.locator('.ci-candidate').first().click()
    await page.getByRole('button', { name: 'Approve revision', exact: true }).click()
    await page.waitForURL((url) => url.hash.includes('candidate=') && !url.hash.includes(cid))
    const approvedId = new URLSearchParams(page.url().split('?')[1]).get('candidate')
    const approved = await read(`/candidate/${approvedId}`)
    assert.equal(approved.trust, 'trusted')
    assert.equal(approved.head, original.head)
    assert.equal(approved.base, original.base)
    assert.equal((await read(`/candidate/${cid}`)).verdictReason, 'superseded by approval')
    await page.locator('.ci-verdict').filter({ hasText: /^Landed$/ }).waitFor({ timeout: 240000 })
    assert.deepEqual(errors, [])
    console.log('Web actions PASS: untrusted rerun preserves revision/trust and waits; 54 candidates paginate 50/4 without duplicates; browser approval creates trusted revision and lands', approvedId)
  } else if (row === 'q18') {
    const candidate = await read(`/candidate/${args[0]}`)
    assert.equal(candidate.status, 'passed')
    assert.equal(candidate.landed, true)
    const jobs = candidate.attempts.filter((a) => a.kind === 'job')
    assert.equal(jobs.length, 8)
    for (const attempt of jobs) {
      assert.equal(attempt.status, 'passed')
      assert.ok(attempt.log.key.includes('/trusted/'))
      const redirect = await owner.get(prefix + `/attempt/${attempt.attempt}/log`, { maxRedirects: 0 })
      assert.equal(redirect.status(), 302)
      const url = redirect.headers().location
      assert.ok(new URL(url).searchParams.has('X-Amz-Signature'))
      const response = await anon.get(url)
      assert.equal(response.status(), 200)
      const bytes = await response.body()
      assert.equal(bytes.length, attempt.log.size)
      assert.equal(createHash('sha256').update(bytes).digest('hex'), attempt.log.sha256)
      console.log(attempt.workflow + '/' + attempt.job, attempt.attempt, bytes.length, attempt.log.sha256)
    }
    const repoResponse = await owner.get(`${base}/apps/urgit/api/repository/${candidate.repository}`)
    const repo = await repoResponse.json()
    assert.equal(repo.refs.find((ref) => ref.name === candidate.ref).oid, candidate.candidate)
    console.log('Q18 PASS:', candidate.id, '8/8 passed, 8 trusted logs read from private store with matching sizes/hashes, landed', candidate.candidate)
  } else {
    throw new Error('unknown web row')
  }
} finally {
  await browser?.close()
  await owner.dispose()
  await anon.dispose()
}
