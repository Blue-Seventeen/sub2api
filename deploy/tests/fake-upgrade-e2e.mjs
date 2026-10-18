import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import assert from 'node:assert/strict';
import WebSocket from '../../frontend/node_modules/ws/wrapper.mjs';

const manifestPath = process.argv[2];
if (!manifestPath) throw new Error('Usage: node deploy/tests/fake-upgrade-e2e.mjs <manifest.json>');
const manifest = JSON.parse(readFileSync(manifestPath, 'utf8').replace(/^\uFEFF/, ''));
if (!/^sub2api-v0215-[a-f0-9]{10}-postgres$/.test(manifest.postgres) || Number(manifest.port) === 8080) {
  throw new Error('Only an isolated upgrade fixture is allowed');
}
const base = `http://127.0.0.1:${manifest.port}`;
const fake = `${manifest.postgres.replace(/-postgres$/, '-fake')}:18321`;
const env = Object.fromEntries(JSON.parse(readFileSync(join(manifest.snapshot, 'app-env.private.json'), 'utf8').replace(/^\uFEFF/, ''))
  .map(v => [v.slice(0, v.indexOf('=')), v.slice(v.indexOf('=') + 1)]));
const sql = query => execFileSync('docker', ['exec', manifest.postgres, 'psql', '-U', 'sub2api', '-d', 'sub2api',
  '-v', 'ON_ERROR_STOP=1', '-Atc', query], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
const query = select => JSON.parse(sql(`SELECT COALESCE(json_agg(t),'[]'::json) FROM (${select}) t`));
async function api(path, token, body, method = body ? 'POST' : 'GET') {
  const response = await fetch(`${base}/api/v1/${path}`, { method, headers: {
    'content-type': 'application/json', ...(token ? { authorization: `Bearer ${token}` } : {}),
  }, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(20000) });
  const result = await response.json();
  if (!response.ok) throw new Error(`${method} ${path}: HTTP ${response.status} (${result.code || 'api_error'}): ${result.message || ''}`);
  return result.data;
}
const admin = (await api('auth/login', '', { email: env.ADMIN_EMAIL, password: env.ADMIN_PASSWORD })).access_token;
const suffix = randomUUID().slice(0, 8);
const userEmail = `v0215e2e-${suffix}@example.test`;
const password = randomUUID();
const user = await api('admin/users', admin, { email: userEmail, password, balance: 100,
  concurrency: 8, unified_rate_enabled: true, unified_rate_multiplier: 1.5 });
const token = (await api('auth/login', '', { email: userEmail, password })).access_token;
const models = ['gpt-5.5', 'gpt-5.4', 'claude-sonnet-4-6'];
const price = { models, billing_mode: 'token', input_price: 0.000001, output_price: 0.000002,
  cache_read_price: 0.0000001, cache_write_price: 0.00000125 };
const resources = { user: user.id, groups: [], accounts: [], keys: [] };
const results = [];
const usageRows = [];
const startUsage = Number(sql(`SELECT COALESCE(max(id),0) FROM usage_logs`));
const startBalance = Number(sql(`SELECT balance FROM users WHERE id=${user.id}`));
async function fixture(name, platform, protocol = 'responses', mode = 'normal', fallback = false) {
  const group = await api('admin/groups', admin, { name: `v0215-${suffix}-${name}`, platform,
    is_exclusive: true, rate_multiplier: 2, allow_messages_dispatch: true,
    peak_rate_enabled: true, peak_rate_windows: [{ start: '00:00', end: '12:00', multiplier: 3 },
      { start: '12:00', end: '23:59', multiplier: 3 }], model_pricing: [{ ...price, platform }],
    model_allowlist: { enabled: true, models }, models_list_config: { enabled: true, models } });
  resources.groups.push(group.id);
  await api(`admin/users/${user.id}`, admin, { allowed_groups: resources.groups }, 'PUT');
  const account = await api('admin/accounts', admin, { name: `v0215-${suffix}-${name}`, platform,
    type: 'apikey', credentials: { api_key: 'sk-fixture-upstream', base_url: `http://${fake}/${mode}${platform === 'anthropic' ? '' : '/v1'}`,
      api_protocol: protocol, model_mapping: { 'gpt-5.5': 'gpt-5.4', 'claude-sonnet-4-6': 'claude-sonnet-4-6' } },
    extra: { openai_apikey_responses_websockets_v2_enabled: name === 'websocket',
      openai_apikey_responses_websockets_v2_mode: name === 'websocket' ? 'passthrough' : 'off',
      openai_responses_mode: protocol === 'responses' ? 'force_responses' : 'auto' },
    priority: 1, concurrency: 8, group_ids: [group.id] });
  resources.accounts.push(account.id);
  if (fallback) {
    const other = await api('admin/accounts', admin, { name: `v0215-${suffix}-${name}-fallback`, platform,
      type: 'apikey', credentials: { api_key: 'sk-fixture-fallback', base_url: `http://${fake}/normal/v1`, api_protocol: protocol,
        model_mapping: { 'gpt-5.5': 'gpt-5.4' } }, priority: 2, concurrency: 8, group_ids: [group.id] });
    resources.accounts.push(other.id);
  }
  const key = await api('keys', token, { name: `v0215-${name}`, group_id: group.id });
  resources.keys.push(key.id);
  return { group, account, key };
}
function sse(wire) {
  const events = [];
  for (const line of wire.split(/\r?\n/)) {
    if (!line.startsWith('data:')) continue;
    const data = line.slice(5).trim();
    events.push(data === '[DONE]' ? { type: '[DONE]' } : JSON.parse(data));
  }
  return events;
}
async function waitUsage(groupID) {
  for (let n = 0; n < 60; n++) {
    const rows = query(`SELECT id,request_id,model,requested_model,upstream_model,upstream_response_model,
      input_tokens,output_tokens,cache_read_tokens,total_cost,actual_cost,real_actual_cost,first_token_ms,
      rate_multiplier,unified_rate_multiplier,inbound_endpoint,upstream_endpoint,billing_type,
      model_mapping_chain,account_id FROM usage_logs WHERE id>${startUsage} AND user_id=${user.id} AND group_id=${groupID} ORDER BY id`);
    if (rows.length) return rows;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  return [];
}
async function httpCase(name, platform, protocol, route, mode = 'normal', expected = 'success', stream = true) {
  const f = await fixture(name, platform, protocol, mode, ['retry', 'empty'].includes(mode));
  const model = platform === 'anthropic' ? 'claude-sonnet-4-6' : 'gpt-5.5';
  const body = route === 'responses' ? { model, input: 'Reply FIXTURE_OK', stream, max_output_tokens: 32 } : {
    model, messages: [{ role: 'user', content: 'Reply FIXTURE_OK' }], stream, max_tokens: 32,
    ...(route === 'chat/completions' ? { stream_options: { include_usage: true } } : {}),
  };
  const at = Date.now();
  let status, events, correlation, failure;
  try {
    const response = await fetch(`${base}/v1/${route}`, { method: 'POST', headers: {
      authorization: `Bearer ${f.key.key}`, 'content-type': 'application/json', 'anthropic-version': '2023-06-01',
    }, body: JSON.stringify(body), signal: AbortSignal.timeout(22000) });
    status = response.status;
    correlation = response.headers.get('x-client-request-id');
    const wire = await response.text();
    events = stream && response.ok ? sse(wire) : [JSON.parse(wire)];
    const hasError = events.some(e => e.error || ['response.failed', 'response.incomplete', 'error'].includes(e.type));
    if (expected === 'success') {
      assert.equal(status, 200);
      assert.equal(hasError, false);
      assert.ok(JSON.stringify(events).includes('FIXTURE_OK'));
      if (stream) assert.ok(events.some(e => ['response.completed', 'message_stop', '[DONE]'].includes(e.type)));
      if (mode === 'hold') assert.ok(Date.now() - at < 5000, 'complete terminal must not wait for upstream EOF');
    } else if (expected === 'partial') {
      assert.equal(status, 200);
      assert.equal(hasError, true);
      assert.ok(!JSON.stringify(events).includes('sk-fixture-private'), 'client error must redact upstream secrets');
    } else assert.ok(status >= 400 || hasError, 'unterminated upstream must not silently succeed');
    const rows = await waitUsage(f.group.id);
    usageRows.push(...rows);
    // Official native Responses Forward returns no billable result on terminal
    // failure, even when response.failed contains usage. Preserve that contract.
    if (expected !== 'success') assert.equal(rows.length, 0, 'native failed request must not debit balance');
    if (expected === 'success') {
      assert.equal(rows.length, 1, 'one billed usage record per request');
      const row = rows[0];
      assert.ok(row.output_tokens > 0);
      assert.ok(row.total_cost > 0 && row.real_actual_cost > 0);
      if (stream) assert.ok(row.first_token_ms !== null);
      assert.equal(row.requested_model, model);
      assert.ok(row.inbound_endpoint && row.upstream_endpoint);
      assert.equal(Number(row.rate_multiplier), 9);
      assert.equal(Number(row.unified_rate_multiplier), 1.5);
      assert.ok(Math.abs(row.actual_cost / row.real_actual_cost - 1.5) < 1e-5);
      if (platform === 'openai') assert.equal(row.upstream_model, 'gpt-5.4');
      const visible = await api('usage?page_size=100', token);
      const mine = visible.items.find(r => r.id === row.id);
      assert.ok(mine, 'usage must be readable by the owning user');
      assert.ok(!mine.account_id && !mine.real_actual_cost && !mine.upstream_model,
        'user usage must not reveal admin-only attribution or real costs');
    }
    results.push({ name, status, passed: true, duration_ms: Date.now() - at, usage_rows: rows.length, correlation });
  } catch (error) {
    failure = error.message;
    results.push({ name, status, passed: false, duration_ms: Date.now() - at, reason: failure });
  }
  console.log(`${name}: ${failure ? 'FAIL' : 'PASS'} status=${status || 0}`);
}
async function websocketCase() {
  const f = await fixture('websocket', 'openai');
  const client = new WebSocket(`${base.replace('http:', 'ws:')}/v1/responses`, {
    headers: { authorization: `Bearer ${f.key.key}`, 'OpenAI-Beta': 'responses_websockets=2026-02-06' },
  });
  const terminals = [];
  try {
    await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('WebSocket two-turn timeout')), 20000);
      client.on('error', reject);
      client.on('unexpected-response', (_, response) => reject(new Error(`WebSocket HTTP ${response.statusCode}`)));
      client.on('open', () => client.send(JSON.stringify({ type: 'response.create', model: 'gpt-5.5', input: 'Reply FIXTURE_WS_OK' })));
      client.on('message', raw => {
        const event = JSON.parse(raw.toString());
        if (event.type === 'error' || event.type === 'response.failed') reject(new Error('WebSocket upstream error'));
        if (event.type !== 'response.completed') return;
        terminals.push(event.response.id);
        if (terminals.length === 1) client.send(JSON.stringify({ type: 'response.create', model: 'gpt-5.5',
          input: 'Reply FIXTURE_WS_OK again', previous_response_id: event.response.id }));
        else { clearTimeout(timer); resolve(); }
      });
    });
    client.close();
    await new Promise(resolve => setTimeout(resolve, 1000));
    const rows = await waitUsage(f.group.id);
    usageRows.push(...rows);
    assert.equal(new Set(terminals).size, 2);
    assert.equal(rows.length, 2, 'each WebSocket turn billed once');
    assert.ok(rows.every(r => r.output_tokens === 20 && r.actual_cost > 0 && r.first_token_ms !== null));
    results.push({ name: 'websocket-two-turns', passed: true, usage_rows: rows.length });
  } catch (error) {
    results.push({ name: 'websocket-two-turns', passed: false, reason: error.message });
  } finally { client.terminate(); }
}
try {
  await httpCase('responses-native', 'openai', 'responses', 'responses');
  await httpCase('responses-json', 'openai', 'responses', 'responses', 'normal', 'success', false);
  await httpCase('chat-native', 'openai', 'chat_completions', 'chat/completions');
  await httpCase('responses-to-chat', 'openai', 'chat_completions', 'responses');
  await httpCase('chat-to-responses', 'openai', 'responses', 'chat/completions');
  await httpCase('anthropic-native', 'anthropic', 'anthropic', 'messages');
  await httpCase('messages-to-responses', 'openai', 'responses', 'messages');
  await httpCase('terminal-before-eof', 'openai', 'responses', 'responses', 'hold');
  await httpCase('progressive-usage', 'openai', 'responses', 'responses', 'progressive');
  await httpCase('upstream-model-replace', 'openai', 'responses', 'responses', 'replace');
  await httpCase('partial-usage-failure', 'openai', 'responses', 'responses', 'partial', 'partial');
  await httpCase('eof-no-terminal', 'openai', 'responses', 'responses', 'eof', 'failure');
  await httpCase('failure-retry', 'openai', 'responses', 'responses', 'retry');
  await httpCase('empty-terminal-failover', 'openai', 'responses', 'responses', 'empty');
  await websocketCase();
  const endBalance = Number(sql(`SELECT balance FROM users WHERE id=${user.id}`));
  const billed = Number(sql(`SELECT COALESCE(sum(real_actual_cost),0) FROM usage_logs WHERE user_id=${user.id} AND id>${startUsage}`));
  results.push({ name: 'real-balance-deduction', passed: Math.abs(startBalance - endBalance - billed) < 1e-6,
    balance_delta: startBalance - endBalance, real_billed: billed });
} finally {
  const report = { checked_at: new Date().toISOString(), base_url: base, resources, results, usage: usageRows,
    trace: await (await fetch(`${process.env.SUB2API_FAKE_UPSTREAM_BASE || 'http://127.0.0.1:18321'}/trace`)).json() };
  const path = join(dirname(manifestPath), 'fake-upgrade-e2e.json');
  writeFileSync(path, JSON.stringify(report, null, 2));
  console.log(`FAKE_E2E_REPORT=${path}`);
}
if (results.some(r => !r.passed)) process.exitCode = 1;
