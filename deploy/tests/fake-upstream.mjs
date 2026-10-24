import { createServer } from 'node:http';
import { randomUUID } from 'node:crypto';
import { WebSocketServer } from '../../frontend/node_modules/ws/wrapper.mjs';

const traces = [];
const hits = new Map();
const usage = { input_tokens: 100, output_tokens: 20, total_tokens: 120,
  input_tokens_details: { cached_tokens: 10 } };
const chatUsage = { prompt_tokens: 100, completion_tokens: 20, total_tokens: 120,
  prompt_tokens_details: { cached_tokens: 10 } };
const geminiUsage = { promptTokenCount: 100, candidatesTokenCount: 20, totalTokenCount: 120,
  cachedContentTokenCount: 10 };
const geminiModels = ['gemini-2.5-flash', 'gemini-2.5-flash-lite', 'gemini-3-flash',
  'gemini-3-pro-low', 'gemini-3-pro-high'];
const geminiResult = (model, text, completed) => ({
  candidates: [{ index: 0, content: { role: 'model', parts: [{ text }] },
    ...(completed ? { finishReason: 'STOP' } : {}) }],
  modelVersion: model, ...(completed ? { usageMetadata: structuredClone(geminiUsage) } : {}),
});
const terminal = (model, marker, id) => ({ id, object: 'response', status: 'completed', model,
  output: [{ id: `msg_${id}`, type: 'message', role: 'assistant', status: 'completed',
    content: [{ type: 'output_text', text: marker, annotations: [] }] }], usage: structuredClone(usage) });
const event = (response, type, data) => response.write(`event: ${type}\ndata: ${JSON.stringify({ type, ...data })}\n\n`);
const json = (response, status, data) => {
  response.writeHead(status, { 'content-type': 'application/json' });
  response.end(JSON.stringify(data));
};
const server = createServer(async (request, response) => {
  if (request.url === '/health') return json(response, 200, { ok: true });
  if (request.url === '/trace') return json(response, 200, traces);
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  let body;
  try { body = JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}'); }
  catch { return json(response, 400, { error: { message: 'bad json' } }); }
  const url = new URL(request.url, 'http://fixture');
  const path = url.pathname;
  const mode = path.split('/')[1];
  const id = `resp_${randomUUID().replaceAll('-', '')}`;
  const model = mode === 'replace' ? 'gpt-5.4' : (body.model || 'gpt-5.5');
  const marker = 'FIXTURE_OK';
  const isProbe = body.tools?.some(tool => tool.name === 'probe_ping');
  const count = (hits.get(mode) || 0) + (isProbe ? 0 : 1);
  if (!isProbe) hits.set(mode, count);
  traces.push({ transport: 'http', mode, path: request.url, model: body.model,
    stream: body.stream, tool_choice: body.tool_choice,
    roles: Array.isArray(body.messages || body.input) ? (body.messages || body.input).map(v => v.role) : [] });
  if (mode === 'retry' && count === 1 && !isProbe) return json(response, 503, { error: { type: 'server_error', message: 'fixture retry' } });
  if (path === '/normal/v1beta/models') {
    if (request.method !== 'GET') return json(response, 405, { error: { message: 'Gemini model catalog requires GET' } });
    return json(response, 200, { models: geminiModels.map(name => ({ name: `models/${name}`,
      displayName: name, supportedGenerationMethods: ['generateContent', 'streamGenerateContent'] })) });
  }
  const geminiAction = /^\/normal\/v1beta\/models\/([^/:]+):(generateContent|streamGenerateContent)$/.exec(path);
  if (geminiAction) {
    if (request.method !== 'POST') return json(response, 405, { error: { message: 'Gemini generation requires POST' } });
    const [, geminiModel, action] = geminiAction;
    if (action === 'generateContent') return json(response, 200, geminiResult(geminiModel, marker, true));
    if (url.searchParams.get('alt') !== 'sse') return json(response, 400, { error: { message: 'Gemini streaming fixture requires alt=sse' } });
    response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' });
    response.flushHeaders();
    response.write(`data: ${JSON.stringify(geminiResult(geminiModel, 'FIXTURE_', false))}\n\n`);
    response.write(`data: ${JSON.stringify(geminiResult(geminiModel, 'OK', true))}\n\n`);
    return response.end();
  }
  if (path.endsWith('/models')) return json(response, 200, { data: [
    { id: 'gpt-5.5', supported_endpoints: ['/chat/completions', '/responses'] },
    { id: 'claude-sonnet-4-6', supported_endpoints: ['/messages'] },
  ] });
  if (path.endsWith('/responses')) {
    if (isProbe) return json(response, 200, { ...terminal(model, marker, id), output: [
      { type: 'function_call', id: `fc_${id}`, call_id: `call_${id}`, name: 'probe_ping', arguments: '{"ok":true}', status: 'completed' },
    ] });
    if (!body.stream) return json(response, 200, terminal(model, marker, id));
    response.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' });
    response.flushHeaders();
    if (mode === 'empty') {
      event(response, 'response.completed', { response: { id, model, output: [], status: 'completed' } });
      return response.end();
    }
    event(response, 'response.created', { response: { id, model, status: 'in_progress' } });
    event(response, 'response.output_text.delta', { item_id: `msg_${id}`, output_index: 0,
      content_index: 0, delta: marker });
    if (mode === 'partial') {
      event(response, 'response.failed', { response: { id, model, status: 'failed', usage,
        error: { type: 'server_error', code: 'server_error', message: 'fixture upstream secret sk-fixture-private' } } });
      return response.end();
    }
    if (mode === 'eof') return response.end();
    if (mode === 'progressive') {
      event(response, 'response.in_progress', { response: { id, usage: { input_tokens: 100 } } });
      event(response, 'response.in_progress', { response: { id, usage } });
      const result = terminal(model, marker, id);
      delete result.usage;
      event(response, 'response.completed', { response: result });
    } else event(response, 'response.completed', { response: terminal(model, marker, id) });
    if (mode === 'hold') {
      const timer = setTimeout(() => response.end(), 15000);
      response.on('close', () => clearTimeout(timer));
      return;
    }
    return response.end();
  }
  if (path.endsWith('/chat/completions')) {
    const result = { id, object: 'chat.completion', created: 1, model,
      choices: [{ index: 0, message: { role: 'assistant', content: marker }, finish_reason: 'stop' }], usage: chatUsage };
    if (!body.stream) return json(response, 200, result);
    response.writeHead(200, { 'content-type': 'text/event-stream' });
    response.flushHeaders();
    const send = data => response.write(`data: ${JSON.stringify(data)}\n\n`);
    send({ id, object: 'chat.completion.chunk', model,
      choices: [{ index: 0, delta: { role: 'assistant', content: marker }, finish_reason: null }] });
    send({ id, object: 'chat.completion.chunk', model,
      choices: [{ index: 0, delta: {}, finish_reason: 'stop' }], usage: chatUsage });
    return response.end('data: [DONE]\n\n');
  }
  if (path.endsWith('/messages')) {
    const message = { id, type: 'message', role: 'assistant', model,
      content: [{ type: 'text', text: marker }], stop_reason: 'end_turn',
      usage: { input_tokens: 90, output_tokens: 20, cache_read_input_tokens: 10, cache_creation_input_tokens: 0 } };
    if (!body.stream) return json(response, 200, message);
    response.writeHead(200, { 'content-type': 'text/event-stream' });
    event(response, 'message_start', { message: { ...message, content: [], stop_reason: null,
      usage: { ...message.usage, output_tokens: 0 } } });
    event(response, 'content_block_start', { index: 0, content_block: { type: 'text', text: '' } });
    event(response, 'content_block_delta', { index: 0, delta: { type: 'text_delta', text: marker } });
    event(response, 'content_block_stop', { index: 0 });
    event(response, 'message_delta', { delta: { stop_reason: 'end_turn', stop_sequence: null }, usage: { output_tokens: 20 } });
    event(response, 'message_stop', {});
    return response.end();
  }
  return json(response, 404, { error: { message: 'unsupported fixture endpoint' } });
});

const websocket = new WebSocketServer({ noServer: true });
server.on('upgrade', (request, socket, head) => {
  if (!request.url.endsWith('/responses')) return socket.destroy();
  websocket.handleUpgrade(request, socket, head, connection => {
    connection.on('message', raw => {
      const body = JSON.parse(raw.toString());
      const id = `resp_${randomUUID().replaceAll('-', '')}`;
      const model = body.model || 'gpt-5.5';
      traces.push({ transport: 'ws', path: request.url, model, previous_response_id: body.previous_response_id });
      connection.send(JSON.stringify({ type: 'response.created', response: { id, model, status: 'in_progress' } }));
      connection.send(JSON.stringify({ type: 'response.output_text.delta', item_id: `msg_${id}`,
        output_index: 0, content_index: 0, delta: 'FIXTURE_WS_OK' }));
      const result = terminal(model, 'FIXTURE_WS_OK', id);
      result.usage.input_tokens_details.image_tokens = 8;
      connection.send(JSON.stringify({ type: 'response.completed', response: result }));
    });
  });
});
server.listen(18321, '0.0.0.0', () => console.log('FIXTURE_UPSTREAM_READY=18321'));
