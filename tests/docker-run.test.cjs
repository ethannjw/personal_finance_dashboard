const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const launcher = path.resolve(__dirname, '../scripts/docker-run.sh');

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ledger-docker-run-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, 'bin'));
  fs.mkdirSync(path.join(root, 'auth'));
  fs.writeFileSync(path.join(root, 'auth', 'dummy.json'), '{}');
  fs.writeFileSync(path.join(root, '.env'), 'GOOGLE_TOKEN_FILE="~/auth/dummy.json"\n');
  fs.writeFileSync(path.join(root, 'bin', 'docker'),
    '#!/bin/sh\nprintf "%s\\n" "$GOOGLE_TOKEN_FILE" "$LEDGER_ENV_FILE" "$FINANCE_SHEET_ID" "$@"\n',
    { mode: 0o755 });
  const env = { ...process.env, HOME: root, PATH: `${root}/bin:${process.env.PATH}` };
  for (const key of ['GOOGLE_TOKEN_FILE', 'LEDGER_ENV_FILE', 'FINANCE_SHEET_ID']) delete env[key];
  return {
    root,
    run: (overrides = {}) => spawnSync('sh', [launcher], {
      cwd: root, env: { ...env, ...overrides }, encoding: 'utf8',
    }),
  };
}

test('sheet-only override uses the token from .env', (t) => {
  const { root, run } = fixture(t);
  const result = run({ FINANCE_SHEET_ID: 'fictional-demo-sheet' });
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(result.stdout.trim().split('\n'), [
    `${root}/auth/dummy.json`, `${root}/.env`, 'fictional-demo-sheet',
    'compose', '--env-file', '/dev/null', 'up', '--build',
  ]);
});

test('environment token overrides .env', (t) => {
  const { root, run } = fixture(t);
  fs.writeFileSync(path.join(root, 'override.json'), '{}');
  const result = run({ GOOGLE_TOKEN_FILE: '~/override.json' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout.split('\n')[0], `${root}/override.json`);
});

test('relative token paths use the selected config directory without executing content', (t) => {
  const { root, run } = fixture(t);
  fs.mkdirSync(path.join(root, 'config'));
  const tokenName = '$(touch executed) token.json';
  fs.writeFileSync(path.join(root, 'config', tokenName), '{}');
  fs.writeFileSync(path.join(root, 'config', 'demo.env'), `GOOGLE_TOKEN_FILE='${tokenName}'\n`);
  const result = run({ LEDGER_ENV_FILE: 'config/demo.env' });
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout.split('\n')[0], `${root}/config/${tokenName}`);
  assert.equal(fs.existsSync(path.join(root, 'executed')), false);
});

test('missing token fails before starting Docker', (t) => {
  const { run } = fixture(t);
  const result = run({ GOOGLE_TOKEN_FILE: '~/missing.json' });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Google token file does not exist/);
  assert.equal(result.stdout, '');
});
