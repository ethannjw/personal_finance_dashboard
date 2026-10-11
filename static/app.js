"use strict";

const defaultFilters = () => ({ bucket: "all", year: "all", month: "all", category: "all", status: "all", search: "" });
const filterControls = { bucket: "#bucket-filter", year: "#year-filter", month: "#month-filter", category: "#category-filter", status: "#status-filter", search: "#global-search" };

const state = {
  transactions: [], categories: [], filtered: [], gridRows: [],
  view: "overview", editing: null, table: null,
  filtersByView: { overview: defaultFilters(), transactions: defaultFilters() },
  overviewRows: [],
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => [...document.querySelectorAll(selector)];
const money = new Intl.NumberFormat("en-SG", { style: "currency", currency: "SGD", maximumFractionDigits: 2 });
const monthName = new Intl.DateTimeFormat("en-SG", { month: "short", year: "numeric", timeZone: "UTC" });
const shortMonthName = new Intl.DateTimeFormat("en-SG", { month: "short", year: "2-digit", timeZone: "UTC" });
const dateLabel = new Intl.DateTimeFormat("en-SG", { day: "numeric", month: "short", year: "numeric", timeZone: "UTC" });

function esc(value) {
  return String(value ?? "").replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
}

function dateFromIso(value) { return new Date(`${value}T00:00:00Z`); }
function formatDate(value) { return /^\d{4}-\d{2}-\d{2}$/.test(value) ? dateLabel.format(dateFromIso(value)) : value; }
function formatMonth(value) { return /^\d{4}-\d{2}$/.test(value) ? monthName.format(new Date(`${value}-01T00:00:00Z`)) : value; }
function formatShortMonth(value) { return /^\d{4}-\d{2}$/.test(value) ? shortMonthName.format(new Date(`${value}-01T00:00:00Z`)).replace(" ", " ’") : value; }
function sum(rows) { return rows.reduce((total, row) => total + Number(row.amount || 0), 0); }

async function api(path, options = {}) {
  const response = await fetch(path, { headers: { "Content-Type": "application/json" }, ...options });
  let body = {};
  try { body = await response.json(); } catch (_) { /* use status fallback */ }
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}

async function loadData(force = false) {
  $("#loading").classList.remove("hidden");
  $("#error-state").classList.add("hidden");
  $("#content").classList.add("hidden");
  $("#refresh-button").disabled = true;
  try {
    const data = await api(`/api/data${force ? "?refresh=1" : ""}`);
    state.transactions = data.transactions;
    state.categories = data.categories;
    $("#sheet-link").href = data.spreadsheetUrl;
    const synced = data.syncedAt ? new Date(data.syncedAt) : new Date();
    $("#sync-status").textContent = `Synced ${synced.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
    populateFilters();
    updateBulkField();
    applyFilters();
    $("#loading").classList.add("hidden");
    $("#content").classList.remove("hidden");
  } catch (error) {
    $("#loading").classList.add("hidden");
    $("#error-message").textContent = error.message;
    $("#error-state").classList.remove("hidden");
  } finally {
    $("#refresh-button").disabled = false;
  }
}

function setOptions(select, options, allLabel, preserve = true) {
  const previous = preserve ? select.value : "all";
  select.innerHTML = `<option value="all">${esc(allLabel)}</option>` + options.map(value => `<option value="${esc(value)}">${esc(value)}</option>`).join("");
  if (options.includes(previous)) select.value = previous;
}

function populateFilters() {
  setOptions($("#bucket-filter"), [...new Set(state.transactions.map(t => t.reportingBucket))].sort(), "All buckets");
  setOptions($("#year-filter"), [...new Set(state.transactions.map(t => t.month.slice(0, 4)))].filter(Boolean).sort().reverse(), "All years");
  setOptions($("#category-filter"), state.categories.map(c => c.name).sort(), "All categories");
  $("#bucket-tabs").innerHTML = `<button data-bucket="all">All</button>` + [...new Set(state.transactions.map(t => t.reportingBucket))].filter(Boolean).sort().map(bucket => `<button data-bucket="${esc(bucket)}">${esc(bucket)}</button>`).join("");
  $("#edit-category").innerHTML = state.categories.map(c => `<option value="${esc(c.name)}">${esc(c.name)}</option>`).join("");
  updateMonthOptions();
}

function updateMonthOptions() {
  const year = $("#year-filter").value;
  const months = [...new Set(state.transactions.map(t => t.month).filter(month => year === "all" || month.startsWith(`${year}-`)))].sort().reverse();
  const select = $("#month-filter");
  const previous = select.value;
  select.innerHTML = `<option value="all">All months</option>` + months.map(value => `<option value="${esc(value)}">${esc(formatMonth(value))}</option>`).join("");
  if (months.includes(previous)) select.value = previous;
}

function filters(view = state.view) {
  return state.filtersByView[view];
}

function saveFilters() {
  for (const [key, selector] of Object.entries(filterControls)) {
    filters()[key] = $(selector).value;
  }
}

function restoreFilters() {
  // Restore the year before rebuilding its dependent month options.
  for (const [key, selector] of Object.entries(filterControls)) {
    if (key !== "month") $(selector).value = filters()[key];
  }
  updateMonthOptions();
  $("#month-filter").value = filters().month;
  if (!$("#month-filter").value) $("#month-filter").value = "all";
  $(".filter-bar").setAttribute("aria-label", state.view === "overview" ? "Overview filters" : "Transaction filters");
}

function filterTransactions(f) {
  const search = f.search.trim().toLowerCase();
  return state.transactions.filter(t => {
    if (f.bucket !== "all" && t.reportingBucket !== f.bucket) return false;
    if (f.year !== "all" && !t.month.startsWith(`${f.year}-`)) return false;
    if (f.month !== "all" && t.month !== f.month) return false;
    if (f.category !== "all" && t.category !== f.category) return false;
    if (f.status === "included" && !t.included) return false;
    if (f.status === "excluded" && t.included) return false;
    if (search && ![t.description, t.account, t.bank, t.category, t.notes, t.amount].join(" ").toLowerCase().includes(search)) return false;
    return true;
  }).sort((a, b) => b.date.localeCompare(a.date) || b.rowNumber - a.rowNumber);
}

function applyFilters() {
  saveFilters();
  state.overviewRows = filterTransactions(filters("overview"));
  state.filtered = filterTransactions(filters("transactions"));
  syncBucketTabs();
  render();
}

function reportRows() { return state.overviewRows.filter(t => t.included); }

function render() {
  renderPeriod();
  renderReports();
  if (state.view === "transactions") renderGrid();
}

function renderReports() {
  renderMetrics();
  renderMonthlyChart();
  renderCategoryChart();
}

function renderPeriod() {
  const { year, month, bucket } = filters("overview");
  let title = "All time";
  if (month !== "all") title = formatMonth(month);
  else if (year !== "all") title = year;
  $("#period-title").textContent = title;
  $("#period-subtitle").textContent = bucket === "all" ? "Across all reporting buckets" : `${bucket} expenses only`;
}

function groupedTotals(rows, key) {
  return rows.reduce((groups, row) => {
    const label = row[key] || "Unspecified";
    groups[label] = (groups[label] || 0) + Number(row.amount || 0);
    return groups;
  }, {});
}

function renderMetrics() {
  const rows = reportRows();
  const net = sum(rows);
  const months = [...new Set(rows.map(t => t.month))];
  const categories = Object.entries(groupedTotals(rows, "category")).sort((a, b) => b[1] - a[1]);
  $("#net-expenses").textContent = money.format(net);
  $("#transaction-count").textContent = rows.length.toLocaleString("en-SG");
  $("#monthly-average").textContent = money.format(months.length ? net / months.length : 0);
  $("#average-caption").textContent = months.length ? `Across ${months.length} represented month${months.length === 1 ? "" : "s"}` : "No represented months";
  $("#top-category").textContent = categories[0]?.[0] || "—";
  $("#top-category-caption").textContent = categories[0] ? `${money.format(categories[0][1])} net` : "No tracked expenses";
}

function renderMonthlyChart() {
  const values = Object.entries(groupedTotals(reportRows(), "month")).sort(([a], [b]) => a.localeCompare(b));
  const target = $("#monthly-chart");
  if (!values.length) { target.innerHTML = `<div class="empty-chart">No tracked expenses for these filters</div>`; return; }
  const width = 700, height = 230, left = 52, right = 12, top = 12, bottom = 32;
  const positiveMax = Math.max(...values.map(([, value]) => value), 1);
  const negativeMin = Math.min(...values.map(([, value]) => value), 0);
  const range = positiveMax - negativeMin || 1;
  const x = i => left + (values.length === 1 ? (width - left - right) / 2 : i * (width - left - right) / (values.length - 1));
  const y = value => top + (positiveMax - value) * (height - top - bottom) / range;
  const points = values.map(([, value], i) => `${x(i)},${y(value)}`).join(" ");
  const area = `${left},${y(0)} ${points} ${x(values.length - 1)},${y(0)}`;
  const grid = [0, .25, .5, .75, 1].map(fraction => {
    const value = positiveMax - fraction * range;
    const py = y(value);
    return `<line class="chart-gridline" x1="${left}" y1="${py}" x2="${width-right}" y2="${py}"/><text class="chart-label" x="0" y="${py+3}">${esc(compactMoney(value))}</text>`;
  }).join("");
  const labelEvery = Math.max(1, Math.ceil(values.length / 7));
  const labels = values.map(([month], i) => i % labelEvery === 0 || i === values.length - 1 ? `<text class="chart-label" x="${x(i)}" y="${height-7}" text-anchor="middle">${esc(formatShortMonth(month))}</text>` : "").join("");
  const hoverPoints = values.map(([month, value], i) => {
    const start = i === 0 ? left - 12 : (x(i - 1) + x(i)) / 2;
    const end = i === values.length - 1 ? width : (x(i) + x(i + 1)) / 2;
    const tooltipX = Math.max(left, Math.min(x(i) - 80, width - 172));
    const label = `${formatMonth(month)}: ${money.format(value)}`;
    return `<g class="monthly-hover" tabindex="0" role="img" aria-label="${esc(label)}"><rect class="monthly-hit" x="${start}" y="${top}" width="${end - start}" height="${height - top - bottom}"/><circle class="monthly-highlight" cx="${x(i)}" cy="${y(value)}" r="5"/><g class="monthly-tooltip" transform="translate(${tooltipX},14)"><rect width="160" height="48" rx="6"/><text x="10" y="19">${esc(formatMonth(month))}</text><text class="monthly-tooltip-value" x="10" y="37">${esc(money.format(value))}</text></g></g>`;
  }).join("");
  target.innerHTML = `<svg viewBox="0 0 ${width} ${height}" role="img"><defs><linearGradient id="areaGradient" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#5a8e7e" stop-opacity=".28"/><stop offset="1" stop-color="#5a8e7e" stop-opacity="0"/></linearGradient></defs>${grid}<polygon class="chart-area" points="${area}"/><polyline class="chart-line" points="${points}"/>${values.map(([, value], i) => `<circle class="chart-dot" cx="${x(i)}" cy="${y(value)}" r="3"><title>${esc(formatMonth(values[i][0]))}: ${esc(money.format(value))}</title></circle>`).join("")}${labels}${hoverPoints}</svg>`;
}

function compactMoney(value) {
  const abs = Math.abs(value);
  const sign = value < 0 ? "−" : "";
  return abs >= 1000 ? `${sign}$${(abs / 1000).toFixed(abs >= 10000 ? 0 : 1)}k` : `${sign}$${abs.toFixed(0)}`;
}

function renderCategoryChart() {
  const values = Object.entries(groupedTotals(reportRows(), "category")).filter(([, value]) => value > 0).sort((a, b) => b[1] - a[1]).slice(0, 7);
  const target = $("#category-chart");
  if (!values.length) { target.innerHTML = `<div class="empty-chart">No category data</div>`; return; }
  const max = values[0][1] || 1;
  target.innerHTML = values.map(([category, value]) => `<div class="category-row" tabindex="0" aria-label="${esc(category)}: ${esc(money.format(value))}"><span title="${esc(category)}">${esc(category)}</span><div class="bar-track"><div class="bar-fill" style="width:${Math.max(1, value / max * 100)}%"></div></div><span class="category-value">${esc(compactMoney(value))}</span><div class="category-tooltip" role="tooltip"><strong>${esc(category)}</strong><span>${esc(money.format(value))}</span></div></div>`).join("");
}

function listFilterParams() {
  return { valuesLookup: true, clearable: true, autocomplete: true, listOnEmpty: true };
}

function amountHeaderFilter(headerValue, rowValue) {
  const query = String(headerValue || "").trim().replace(/[$,\s]/g, "");
  if (!query) return true;
  const amount = Number(rowValue);
  const range = query.match(/^(-?\d+(?:\.\d+)?)\.\.(-?\d+(?:\.\d+)?)$/);
  if (range) return amount >= Number(range[1]) && amount <= Number(range[2]);
  const comparison = query.match(/^(<=|>=|<|>)(-?\d+(?:\.\d+)?)$/);
  if (comparison) {
    const target = Number(comparison[2]);
    return ({ "<": amount < target, "<=": amount <= target, ">": amount > target, ">=": amount >= target })[comparison[1]];
  }
  const target = Number(query);
  return Number.isNaN(target) ? true : amount === target;
}

function trackedHeaderFilter(headerValue, rowValue) {
  if (headerValue === "") return true;
  return String(rowValue) === String(headerValue);
}

function renderGrid() {
  if (!window.Tabulator) {
    showToast("The transaction grid failed to load", true);
    return;
  }
  if (state.table) {
    state.table.replaceData(state.filtered);
    return;
  }
  const categoryValues = Object.fromEntries(state.categories.map(category => [category.name, category.name]));
  state.table = new Tabulator("#transaction-grid", {
    data: state.filtered,
    selectableRows: true,
    selectableRowsPersistence: false,
    index: "id",
    height: "650px",
    layout: "fitDataStretch",
    pagination: true,
    paginationMode: "local",
    paginationSize: 25,
    paginationSizeSelector: [25, 50, 100, true],
    initialSort: [{ column: "date", dir: "desc" }],
    movableColumns: true,
    resizableColumnFit: true,
    placeholder: "No transactions match these filters",
    persistence: { sort: true, headerFilter: true, columns: true, page: true },
    persistenceID: "ledger-transactions-v2",
    columns: [
      { title: "Select", formatter: "rowSelection", titleFormatter: "rowSelection", titleFormatterParams: { rowRange: "visible" }, hozAlign: "center", headerSort: false, width: 48, frozen: true, resizable: false },
      { title: "Date", field: "date", sorter: "string", width: 120, frozen: true, headerFilter: "input", headerFilterPlaceholder: "YYYY-MM…" },
      { title: "Description", field: "description", sorter: "string", minWidth: 260, widthGrow: 3, editor: "input", headerFilter: "input", headerFilterPlaceholder: "Contains…" },
      { title: "Bank", field: "bank", sorter: "string", width: 95, headerFilter: "list", headerFilterParams: listFilterParams() },
      { title: "Account", field: "account", sorter: "string", minWidth: 190, widthGrow: 2, headerFilter: "list", headerFilterParams: listFilterParams() },
      { title: "Bucket", field: "reportingBucket", sorter: "string", width: 135, headerFilter: "list", headerFilterParams: listFilterParams() },
      { title: "Category", field: "category", sorter: "string", minWidth: 165, editor: "list", editorParams: { values: categoryValues, autocomplete: true, listOnEmpty: true }, headerFilter: "list", headerFilterParams: { values: categoryValues, clearable: true, autocomplete: true, listOnEmpty: true } },
      { title: "Amount", field: "amount", sorter: "number", hozAlign: "right", width: 130, formatter: cell => `<span class="${cell.getValue() < 0 ? "credit" : ""}">${esc(money.format(cell.getValue()))}</span>`, headerFilter: "input", headerFilterPlaceholder: "e.g. 10..100", headerFilterFunc: amountHeaderFilter },
      { title: "Tracked", field: "included", sorter: "boolean", hozAlign: "center", width: 105, formatter: "tickCross", editor: "tickCross", headerFilter: "list", headerFilterParams: { values: { "": "All", true: "Tracked", false: "Excluded" }, clearable: true }, headerFilterFunc: trackedHeaderFilter },
      { title: "Notes", field: "notes", sorter: "string", minWidth: 180, widthGrow: 2, editor: "textarea", headerFilter: "input", headerFilterPlaceholder: "Contains…" },
      { title: "Source", field: "source", sorter: "string", minWidth: 155, headerFilter: "input", visible: false },
      { title: "File", field: "file", sorter: "string", minWidth: 220, headerFilter: "input", visible: false },
      { title: "Edit", width: 72, headerSort: false, formatter: () => "<button class=\"grid-edit-button\">Edit</button>", cellClick: (_event, cell) => openEditor(cell.getRow().getData().id) },
    ],
  });
  state.table.on("dataFiltered", (_filters, rows) => {
    state.gridRows = rows.map(row => row.getData());
    $("#result-count").textContent = `${state.gridRows.length.toLocaleString("en-SG")} result${state.gridRows.length === 1 ? "" : "s"}`;
  });
  state.table.on("rowSelectionChanged", updateBulkSelection);
  updateBulkSelection();
  state.table.on("rowDblClick", (_event, row) => openEditor(row.getData().id));
  state.table.on("cellEdited", saveGridCell);
  $("#result-count").textContent = `${state.filtered.length.toLocaleString("en-SG")} results`;
}

function updateBulkSelection() {
  const count = state.table?.getSelectedRows().length || 0;
  $("#selected-count").textContent = ` ${count} selected`;
  $("#bulk-apply").disabled = !count || state.bulkSaving;
}

function updateBulkField() {
  const field = $("#bulk-field").value;
  $("#bulk-category").innerHTML = state.categories.map(category => `<option value="${esc(category.name)}">${esc(category.name)}</option>`).join("");
  $("#bulk-category-label").classList.toggle("hidden", field !== "category");
  $("#bulk-text-label").classList.toggle("hidden", !["description", "notes"].includes(field));
  $("#bulk-status-label").classList.toggle("hidden", field !== "isTransfer");
  $("#bulk-text").maxLength = field === "notes" ? 2000 : 500;
}

async function saveBulk(event) {
  event.preventDefault();
  if (state.bulkSaving) return;
  const rows = state.table.getSelectedRows();
  if (!rows.length) return;
  const field = $("#bulk-field").value;
  const value = field === "category" ? $("#bulk-category").value : field === "isTransfer" ? $("#bulk-status").value === "true" : $("#bulk-text").value;
  state.bulkSaving = true;
  updateBulkSelection();
  const controls = [...$("#bulk-form").querySelectorAll("input,select,button")];
  controls.forEach(control => control.disabled = true);
  let saved = 0;
  let failure = "";
  let next = 0;
  async function worker() {
    while (!failure && next < rows.length) {
      const row = rows[next++];
      const id = row.getData().id;
      try {
        const result = await api(`/api/transactions/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ [field]: value }) });
        const index = state.transactions.findIndex(item => item.id === id);
        if (index >= 0) state.transactions[index] = { ...state.transactions[index], ...result.transaction };
        await row.update(result.transaction);
        row.deselect();
        saved++;
        $("#bulk-progress").textContent = `Saved ${saved} of ${rows.length}…`;
      } catch (error) { failure = failure || error.message; }
    }
  }
  $("#bulk-progress").textContent = `Saving ${rows.length} entries…`;
  if (field === "category") {
    try {
      const result = await api("/api/transactions/categories", { method: "PATCH", body: JSON.stringify({ ids: rows.map(row => row.getData().id), category: value }) });
      const updated = new Map(result.transactions.map(transaction => [transaction.id, transaction]));
      state.transactions = state.transactions.map(transaction => updated.has(transaction.id) ? { ...transaction, ...updated.get(transaction.id) } : transaction);
      saved = result.transactions.length;
      for (const row of rows) { await row.update(updated.get(row.getData().id)); row.deselect(); }
    } catch (error) { failure = `${error.message} Refresh to verify the sheet before retrying.`; }
  } else {
    await worker();
  }
  state.bulkSaving = false;
  controls.forEach(control => control.disabled = false);
  updateBulkSelection();
  state.gridRows = state.table.getRows("active").map(row => row.getData());
  applyFilters();
  $("#bulk-progress").textContent = failure ? `${saved} saved; ${rows.length - saved} remain selected. ${failure}` : `${saved} entries saved.`;
  showToast(failure ? "Bulk update stopped. Remaining entries are still selected." : `${saved} entries saved to Google Sheets`, !!failure);
}

async function saveGridCell(cell) {
  const field = cell.getField();
  const patches = {
    category: () => ({ category: cell.getValue() }),
    description: () => ({ description: cell.getValue() }),
    notes: () => ({ notes: cell.getValue() }),
    included: () => ({ isTransfer: !cell.getValue() }),
  };
  if (!patches[field]) return;
  const row = cell.getRow();
  const transaction = row.getData();
  try {
    const result = await api(`/api/transactions/${encodeURIComponent(transaction.id)}`, { method: "PATCH", body: JSON.stringify(patches[field]()) });
    await row.update(result.transaction);
    const index = state.transactions.findIndex(item => item.id === transaction.id);
    if (index >= 0) state.transactions[index] = { ...state.transactions[index], ...result.transaction };
    showToast("Saved to Google Sheets");
    applyFilters(false);
  } catch (error) {
    cell.restoreOldValue();
    showToast(error.message, true);
  }
}

function syncBucketTabs() {
  const bucket = filters("overview").bucket;
  $$('[data-bucket]').forEach(button => button.classList.toggle("active", button.dataset.bucket === bucket));
}

function openEditor(id) {
  const transaction = state.transactions.find(t => t.id === id);
  if (!transaction) return;
  state.editing = transaction;
  $("#edit-id").value = transaction.id;
  $("#edit-date").textContent = formatDate(transaction.date);
  $("#edit-amount").textContent = money.format(transaction.amount);
  $("#edit-account").textContent = `${transaction.reportingBucket} · ${transaction.account}`;
  $("#edit-category").value = transaction.category;
  $("#edit-description").value = transaction.description;
  $("#edit-notes").value = transaction.notes;
  $("#edit-transfer").checked = !transaction.included;
  toggleCategoryForTransfer();
  $("#drawer-backdrop").classList.remove("hidden");
  $("#edit-drawer").classList.add("open");
  $("#edit-drawer").setAttribute("aria-hidden", "false");
}

function closeEditor() {
  $("#edit-drawer").classList.remove("open");
  $("#edit-drawer").setAttribute("aria-hidden", "true");
  setTimeout(() => $("#drawer-backdrop").classList.add("hidden"), 240);
  state.editing = null;
}

function toggleCategoryForTransfer() {
  const transfer = $("#edit-transfer").checked;
  $("#edit-category").disabled = transfer;
  if (transfer) $("#edit-category").value = "Internal Transfer";
  else if ($("#edit-category").value === "Internal Transfer" && state.editing) {
    const included = new Set(state.categories.filter(c => c.included).map(c => c.name));
    $("#edit-category").value = included.has(state.editing.autoCategory) ? state.editing.autoCategory : "Other";
  }
}

async function saveEditor(event) {
  event.preventDefault();
  if (!state.editing) return;
  const button = $("#save-edit");
  button.disabled = true;
  button.textContent = "Saving…";
  const patch = {
    category: $("#edit-category").value,
    isTransfer: $("#edit-transfer").checked,
    description: $("#edit-description").value,
    notes: $("#edit-notes").value,
  };
  try {
    await api(`/api/transactions/${encodeURIComponent(state.editing.id)}`, { method: "PATCH", body: JSON.stringify(patch) });
    closeEditor();
    showToast("Saved to Google Sheets");
    await loadData(true);
  } catch (error) {
    showToast(error.message, true);
  } finally {
    button.disabled = false;
    button.textContent = "Save changes";
  }
}

function showToast(message, error = false) {
  const toast = $("#toast");
  toast.textContent = message;
  toast.style.background = error ? "#8c3e35" : "";
  toast.classList.add("show");
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => toast.classList.remove("show"), 3200);
}

function downloadCsv() {
  state.table?.download("csv", `finance-transactions-${new Date().toISOString().slice(0, 10)}.csv`, { bom: true }, "active");
}

function switchView(view) {
  saveFilters();
  state.view = view;
  restoreFilters();
  $$('[data-view]').forEach(button => button.classList.toggle("active", button.dataset.view === view));
  $("#overview-view").classList.toggle("hidden", view !== "overview");
  $("#page-title").textContent = view === "overview" ? "Your money, clearly." : "Every transaction.";
  $(".transactions-panel").classList.toggle("hidden", view !== "transactions");
  applyFilters();
  if (view === "transactions") state.table?.redraw(true);
}

function bindEvents() {
  ["#bucket-filter", "#month-filter", "#category-filter", "#status-filter"].forEach(selector => $(selector).addEventListener("change", () => applyFilters()));
  $("#year-filter").addEventListener("change", () => { updateMonthOptions(); applyFilters(); });
  let searchTimer;
  $("#global-search").addEventListener("input", () => { clearTimeout(searchTimer); searchTimer = setTimeout(() => applyFilters(), 180); });
  $("#clear-filters").addEventListener("click", () => {
    ["#bucket-filter", "#year-filter", "#month-filter", "#category-filter", "#status-filter"].forEach(selector => $(selector).value = "all");
    $("#global-search").value = "";
    updateMonthOptions(); applyFilters();
  });
  $("#bucket-tabs").addEventListener("click", event => {
    const button = event.target.closest("[data-bucket]");
    if (button) { $("#bucket-filter").value = button.dataset.bucket; applyFilters(); }
  });
  $$('[data-view]').forEach(button => button.addEventListener("click", () => switchView(button.dataset.view)));
  $("#refresh-button").addEventListener("click", () => loadData(true));
  $("#retry-button").addEventListener("click", () => loadData(true));
  $("#download-button").addEventListener("click", downloadCsv);
  $("#bulk-field").addEventListener("change", updateBulkField);
  $("#clear-selection").addEventListener("click", () => state.table?.deselectRow());
  $("#bulk-form").addEventListener("submit", saveBulk);
  $("#reset-grid-button").addEventListener("click", () => {
    state.table?.clearFilter(true);
    state.table?.clearSort();
    showToast("Table filters and sorting cleared");
  });
  $("#close-drawer").addEventListener("click", closeEditor);
  $("#cancel-edit").addEventListener("click", closeEditor);
  $("#drawer-backdrop").addEventListener("click", closeEditor);
  $("#edit-transfer").addEventListener("change", toggleCategoryForTransfer);
  $("#edit-form").addEventListener("submit", saveEditor);
  document.addEventListener("keydown", event => { if (event.key === "Escape") closeEditor(); });
}

bindEvents();
loadData();
