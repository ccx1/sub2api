const state = {
  files: [],
  outputText: '{}',
  outputPayload: null,
}

const converter = window.CodexSub2apiConverter

const els = {
  fileInput: document.querySelector('#fileInput'),
  folderInput: document.querySelector('#folderInput'),
  pickFiles: document.querySelector('#pickFiles'),
  pickFolder: document.querySelector('#pickFolder'),
  dropZone: document.querySelector('#dropZone'),
  namePrefix: document.querySelector('#namePrefix'),
  notes: document.querySelector('#notes'),
  concurrency: document.querySelector('#concurrency'),
  priority: document.querySelector('#priority'),
  dedupe: document.querySelector('#dedupe'),
  outputMode: document.querySelector('#outputMode'),
  downloadBtn: document.querySelector('#downloadBtn'),
  copyBtn: document.querySelector('#copyBtn'),
  clearBtn: document.querySelector('#clearBtn'),
  statusLine: document.querySelector('#statusLine'),
  fileRows: document.querySelector('#fileRows'),
  jsonPreview: document.querySelector('#jsonPreview'),
  fileCount: document.querySelector('#fileCount'),
  accountCount: document.querySelector('#accountCount'),
  errorCount: document.querySelector('#errorCount'),
  validBadge: document.querySelector('#validBadge'),
  sizeBadge: document.querySelector('#sizeBadge'),
}

function readSettings() {
  return {
    namePrefix: els.namePrefix.value.trim(),
    notes: els.notes.value,
    concurrency: Math.max(0, Number(els.concurrency.value || 0)),
    priority: Math.max(0, Number(els.priority.value || 0)),
    dedupe: els.dedupe.value,
    outputMode: els.outputMode.value,
  }
}

async function processFiles(files) {
  const items = []
  const settings = readSettings()
  for (const file of files) {
    try {
      const data = JSON.parse(await file.text())
      if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error('顶层必须是 JSON 对象')
      const account = await converter.buildAccount(file.name, data, settings)
      items.push({ file, account, error: '' })
    } catch (error) {
      items.push({ file, account: null, error: error.message || String(error) })
    }
  }
  state.files = items
  regenerate()
}

function regenerate() {
  const settings = readSettings()
  const valid = state.files.filter(item => item.account).map(item => converter.applySettings(item.account, settings))
  const accounts = converter.dedupeAccounts(valid, settings.dedupe)
  state.outputPayload = settings.outputMode === 'accounts' ? accounts : converter.buildBundle(accounts)
  state.outputText = accounts.length ? JSON.stringify(state.outputPayload, null, 2) : '{}'
  render(accounts)
}

function render(accounts) {
  const errors = state.files.filter(item => item.error).length
  els.fileCount.textContent = String(state.files.length)
  els.accountCount.textContent = String(accounts.length)
  els.errorCount.textContent = String(errors)
  els.jsonPreview.textContent = state.outputText
  els.sizeBadge.textContent = `${Math.ceil(new Blob([state.outputText]).size / 1024)} KB`
  renderRows()
  renderState(accounts.length, errors)
}

function renderRows() {
  if (!state.files.length) {
    els.fileRows.innerHTML = '<tr><td colspan="4" class="empty-cell">暂无文件</td></tr>'
    return
  }
  els.fileRows.innerHTML = state.files.map(item => {
    const ok = !item.error
    const account = item.account || {}
    const email = account.credentials?.email || '-'
    const status = ok ? '<span class="status-ok">可导入</span>' : '<span class="status-error">错误</span>'
    return `<tr><td>${status}</td><td>${escapeHtml(account.name || item.error)}</td><td>${escapeHtml(email)}</td><td>${escapeHtml(item.file.name)}</td></tr>`
  }).join('')
}

function renderState(accountCount, errorCount) {
  const ready = accountCount > 0 && errorCount === 0
  els.downloadBtn.disabled = !ready
  els.copyBtn.disabled = !ready
  els.clearBtn.disabled = state.files.length === 0
  els.validBadge.className = `badge ${ready ? 'ok' : errorCount ? 'error' : 'muted'}`
  els.validBadge.textContent = ready ? '可下载' : errorCount ? '需处理' : '未生成'
  els.statusLine.textContent = ready ? '已生成导入 JSON' : errorCount ? '存在无法转换的文件' : '等待文件'
}

function downloadOutput() {
  const blob = new Blob([state.outputText], { type: 'application/json;charset=utf-8' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = `sub2api-import-${Date.now()}.json`
  link.click()
  URL.revokeObjectURL(link.href)
}

async function copyOutput() {
  await navigator.clipboard.writeText(state.outputText)
  els.statusLine.textContent = '已复制到剪贴板'
}

function clearAll() {
  state.files = []
  els.fileInput.value = ''
  els.folderInput.value = ''
  state.outputText = '{}'
  render([])
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, char => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[char]))
}

els.pickFiles.addEventListener('click', () => els.fileInput.click())
els.pickFolder.addEventListener('click', () => els.folderInput.click())
els.fileInput.addEventListener('change', event => processFiles([...event.target.files]))
els.folderInput.addEventListener('change', event => {
  const files = [...event.target.files].filter(file => file.name.toLowerCase().endsWith('.json'))
  processFiles(files)
})
els.downloadBtn.addEventListener('click', downloadOutput)
els.copyBtn.addEventListener('click', copyOutput)
els.clearBtn.addEventListener('click', clearAll)
for (const el of [els.namePrefix, els.notes, els.concurrency, els.priority, els.dedupe, els.outputMode]) {
  el.addEventListener('input', regenerate)
}
for (const eventName of ['dragenter', 'dragover']) {
  els.dropZone.addEventListener(eventName, event => {
    event.preventDefault()
    els.dropZone.classList.add('is-dragging')
  })
}
for (const eventName of ['dragleave', 'drop']) {
  els.dropZone.addEventListener(eventName, event => {
    event.preventDefault()
    els.dropZone.classList.remove('is-dragging')
  })
}
els.dropZone.addEventListener('drop', event => {
  const files = [...event.dataTransfer.files].filter(file => file.name.toLowerCase().endsWith('.json'))
  processFiles(files)
})
