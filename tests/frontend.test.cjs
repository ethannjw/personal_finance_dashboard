const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function app() {
  const elements = new Map();
  const document = {
    querySelector(selector) {
      if (!elements.has(selector)) elements.set(selector, {
        value: selector === '#global-search' ? '' : 'all',
        classList: { toggle() {} }, setAttribute() {},
      });
      return elements.get(selector);
    },
    querySelectorAll() { return []; },
  };
  const context = vm.createContext({ document, Intl, console });
  const source = fs.readFileSync(path.join(__dirname, '../static/app.js'), 'utf8');
  vm.runInContext(source.replace(/bindEvents\(\);\s*loadData\(\);\s*$/, ''), context);
  vm.runInContext(`
    renderReports = () => {};
    renderGrid = () => {};
    state.transactions = [
      { id: 'personal', reportingBucket: 'Personal', month: '2026-01', date: '2026-01-02', category: 'Food', included: true, amount: 10 },
      { id: 'home', reportingBucket: 'Home Shared', month: '2025-12', date: '2025-12-02', category: 'Rent', included: true, amount: 20 },
    ];
  `, context);
  return code => vm.runInContext(code, context);
}

test('page filters and search survive navigation independently', () => {
  const run = app();
  run(`$('#bucket-filter').value = 'Personal'; $('#global-search').value = 'Food'; applyFilters(); switchView('transactions');`);
  assert.equal(run(`$('#bucket-filter').value`), 'all');
  assert.equal(run(`$('#global-search').value`), '');
  assert.equal(run('state.filtered.length'), 2);
  run(`$('#bucket-filter').value = 'Home Shared'; $('#global-search').value = 'Rent'; applyFilters(); switchView('overview');`);
  assert.equal(run(`$('#bucket-filter').value`), 'Personal');
  assert.equal(run(`$('#global-search').value`), 'Food');
  assert.equal(run('reportRows()[0].id'), 'personal');
  assert.equal(run('state.filtered[0].id'), 'home');
  run(`switchView('transactions');`);
  assert.equal(run(`$('#global-search').value`), 'Rent');
});

test('dependent month and year filters restore per page', () => {
  const run = app();
  run(`$('#year-filter').value = '2026'; $('#month-filter').value = '2026-01'; applyFilters(); switchView('transactions');`);
  assert.equal(run(`$('#year-filter').value`), 'all');
  assert.equal(run(`$('#month-filter').value`), 'all');
  run(`$('#year-filter').value = '2025'; $('#month-filter').value = '2025-12'; applyFilters(); switchView('overview');`);
  assert.equal(run(`$('#year-filter').value`), '2026');
  assert.equal(run(`$('#month-filter').value`), '2026-01');
});

test('transaction column filters do not feed overview reports', () => {
  const run = app();
  run(`applyFilters(); state.gridRows = [];`);
  assert.equal(run('reportRows().length'), 2);
  assert.equal(run('sum(reportRows())'), 30);
});
