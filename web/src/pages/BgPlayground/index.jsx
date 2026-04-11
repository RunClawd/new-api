import React, { useState, useCallback, useEffect, useRef } from 'react';
import { Card, Typography, Button, Select, Input, TextArea, Space, Tag, Toast } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text } = Typography;

const EXEC_MODES = ['sync', 'async', 'stream'];

// Max playground history stored in sessionStorage
const HISTORY_KEY = 'bg_playground_history';
const MAX_HISTORY = 20;

function loadHistory() {
  try { return JSON.parse(sessionStorage.getItem(HISTORY_KEY) ?? '[]'); } catch { return []; }
}
function saveHistory(hist) {
  sessionStorage.setItem(HISTORY_KEY, JSON.stringify(hist.slice(0, MAX_HISTORY)));
}

// SSE chunk parser: splits on \n\n boundaries
function* parseSSEChunks(text) {
  const blocks = text.split('\n\n');
  for (const block of blocks) {
    if (!block.trim()) continue;
    let eventType = '', data = '';
    for (const line of block.split('\n')) {
      if (line.startsWith('event: ')) eventType = line.slice(7).trim();
      else if (line.startsWith('data: ')) data = line.slice(6).trim();
    }
    if (data) yield { eventType, data };
  }
}

export default function BgPlaygroundPage() {
  const { t } = useTranslation();

  const [capabilities, setCapabilities] = useState([]);
  const [model, setModel]               = useState('');
  const [execMode, setExecMode]         = useState('sync');
  const [inputJSON, setInputJSON]       = useState('{"messages":[{"role":"user","content":"Hello!"}]}');
  const [apiToken, setApiToken]         = useState(() => sessionStorage.getItem('playground_token') ?? '');
  const [output, setOutput]             = useState('');
  const [streamText, setStreamText]     = useState('');
  const [sending, setSending]           = useState(false);
  const [history, setHistory]           = useState(loadHistory);
  const [showHistory, setShowHistory]   = useState(false);
  const abortRef = useRef(null);

  // Load capability models
  const fetchCapabilities = useCallback(async () => {
    try {
      const res = await API.get('/api/bg/capabilities?p=1&page_size=1000');
      if (res.data?.success) {
        const items = res.data.data?.items ?? [];
        // Unique capability names
        const names = [...new Set(items.map(i => i.capability_name))].sort();
        setCapabilities(names);
        if (!model && names.length) setModel(names[0]);
      }
    } catch { /* ignore */ }
  }, [model]);

  useEffect(() => { fetchCapabilities(); }, []);

  const persistToken = (val) => {
    setApiToken(val);
    sessionStorage.setItem('playground_token', val);
  };

  const formatJSON = () => {
    try {
      setInputJSON(JSON.stringify(JSON.parse(inputJSON), null, 2));
    } catch { showError(t('JSON 格式错误')); }
  };

  const addHistory = useCallback((entry) => {
    setHistory(prev => {
      const next = [entry, ...prev].slice(0, MAX_HISTORY);
      saveHistory(next);
      return next;
    });
  }, []);

  const handleSend = useCallback(async () => {
    if (!apiToken) { showError(t('请填写 API Token（格式 sk-xxx）')); return; }
    if (!model) { showError(t('请选择 Capability Model')); return; }
    let parsedInput;
    try { parsedInput = JSON.parse(inputJSON); }
    catch { showError(t('Input JSON 格式错误')); return; }

    setSending(true);
    setOutput('');
    setStreamText('');

    const payload = { model, input: parsedInput, execution_mode: execMode };

    if (execMode === 'stream') {
      // fetch + ReadableStream for SSE (EventSource doesn't support custom headers)
      try {
        const ctrl = new AbortController();
        abortRef.current = ctrl;

        const resp = await fetch('/v1/bg/responses', {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${apiToken}`,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(payload),
          signal: ctrl.signal,
        });

        if (!resp.ok) {
          const text = await resp.text();
          setOutput(`HTTP ${resp.status}: ${text}`);
          setSending(false);
          return;
        }

        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          for (const { eventType, data } of parseSSEChunks(buffer)) {
            buffer = '';
            if (data === '[DONE]') { setSending(false); return; }
            try {
              const obj = JSON.parse(data);
              if (eventType === 'response.output_text.delta' && obj.delta) {
                setStreamText(prev => prev + obj.delta);
              }
            } catch { /* non-JSON chunk, ignore */ }
          }
        }
        setSending(false);
      } catch (e) {
        if (e.name !== 'AbortError') {
          setOutput(t('连接已断开，请重试'));
          Toast.warning({ content: t('连接已断开，请刷新或重试'), duration: 4 });
        }
        setSending(false);
      }
    } else {
      // sync / async: plain POST
      try {
        const res = await fetch('/v1/bg/responses', {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${apiToken}`,
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(payload),
        });
        const json = await res.json();
        const pretty = JSON.stringify(json, null, 2);
        setOutput(pretty);
        addHistory({ model, execMode, input: inputJSON, output: pretty, ts: Date.now() });
      } catch (e) {
        setOutput(`Error: ${e.message}`);
      } finally {
        setSending(false);
      }
    }
  }, [apiToken, model, execMode, inputJSON, addHistory, t]);

  const handleAbort = () => {
    abortRef.current?.abort();
    setSending(false);
    Toast.info({ content: t('已中止请求') });
  };

  const loadHistoryEntry = (entry) => {
    setModel(entry.model);
    setExecMode(entry.execMode);
    setInputJSON(entry.input);
    setOutput(entry.output ?? '');
    setShowHistory(false);
  };

  return (
    <div style={{ padding: '24px', maxWidth: 1300, margin: '60px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 调试场')}</Title>
        <Text type='tertiary' size='small'>{t('发送请求并实时查看响应，支持 sync / async / stream 模式')}</Text>
      </div>

      {/* Token input */}
      <Card shadows='hover' style={{ borderRadius: 12, marginBottom: 16 }} bodyStyle={{ padding: '12px 20px' }}>
        <Space align='center'>
          <Text style={{ whiteSpace: 'nowrap' }}>{t('API Token (sk-xxx)')}</Text>
          <Input
            value={apiToken}
            onChange={persistToken}
            mode='password'
            placeholder={t('粘贴令牌管理页面创建的 Token')}
            style={{ width: 340 }}
            showClear
          />
          <Text type='tertiary' size='small'>{t('临时保存于 sessionStorage，不持久化')}</Text>
        </Space>
      </Card>

      <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        {/* Left: configuration + editor */}
        <div style={{ flex: 1, minWidth: 320, display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: '12px 16px' }}>
            <Space wrap>
              <div>
                <Text size='small'>{t('Capability Model')}</Text>
                <Select
                  value={model}
                  onChange={setModel}
                  optionList={capabilities.map(c => ({ value: c, label: c }))}
                  style={{ width: 240, marginTop: 4 }}
                  placeholder={t('选择模型')}
                  filter
                  showClear
                />
              </div>
              <div>
                <Text size='small'>{t('执行模式')}</Text>
                <Select
                  value={execMode}
                  onChange={setExecMode}
                  optionList={EXEC_MODES.map(m => ({ value: m, label: m }))}
                  style={{ width: 120, marginTop: 4 }}
                />
              </div>
            </Space>
          </Card>

          <Card
            title={<Text strong>{t('Input JSON')}</Text>}
            shadows='hover'
            style={{ borderRadius: 12 }}
            headerExtraContent={<Button size='small' onClick={formatJSON}>{t('格式化')}</Button>}
            bodyStyle={{ padding: 0 }}
          >
            <TextArea
              value={inputJSON}
              onChange={setInputJSON}
              autosize={{ minRows: 12, maxRows: 22 }}
              style={{ fontFamily: 'monospace', fontSize: 13, borderRadius: 0, border: 'none' }}
            />
          </Card>

          <Space>
            {sending
              ? <Button type='danger' onClick={handleAbort}>{t('中止')}</Button>
              : <Button type='primary' onClick={handleSend}>{t('发送请求')}</Button>
            }
            <Button onClick={() => setShowHistory(h => !h)} type='tertiary'>
              {showHistory ? t('隐藏历史') : t('历史记录')} ({history.length})
            </Button>
          </Space>

          {showHistory && (
            <Card title={t('最近请求')} shadows='hover' style={{ borderRadius: 12, maxHeight: 300, overflowY: 'auto' }} bodyStyle={{ padding: 8 }}>
              {history.length === 0
                ? <Text type='tertiary' size='small'>{t('暂无历史')}</Text>
                : history.map((h, i) => (
                  <div key={i} onClick={() => loadHistoryEntry(h)} style={{ cursor: 'pointer', padding: '6px 8px', borderRadius: 6, marginBottom: 4 }}
                    onMouseEnter={e => e.currentTarget.style.background = 'var(--semi-color-fill-0)'}
                    onMouseLeave={e => e.currentTarget.style.background = 'transparent'}
                  >
                    <Tag size='small'>{h.execMode}</Tag>&nbsp;
                    <Text size='small' strong>{h.model}</Text>&nbsp;
                    <Text type='tertiary' size='small'>{new Date(h.ts).toLocaleTimeString()}</Text>
                  </div>
                ))
              }
            </Card>
          )}
        </div>

        {/* Right: response */}
        <div style={{ flex: 1, minWidth: 320 }}>
          <Card
            title={
              <span>
                {t('响应')}
                {sending && execMode === 'stream' && (
                  <Tag size='small' color='blue' style={{ marginLeft: 8 }}>{t('流式接收中...')}</Tag>
                )}
              </span>
            }
            shadows='hover'
            style={{ borderRadius: 12 }}
            bodyStyle={{ padding: 0 }}
          >
            {execMode === 'stream' && streamText ? (
              <div style={{ padding: 16, minHeight: 300, fontFamily: 'serif', fontSize: 15, lineHeight: 1.8, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                {streamText}
                {sending && <span style={{ opacity: 0.5, animation: 'pulse 1s infinite' }}>▌</span>}
              </div>
            ) : (
              <pre style={{
                padding: 16,
                margin: 0,
                minHeight: 300,
                fontSize: 12,
                fontFamily: 'monospace',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                overflowY: 'auto',
                maxHeight: 600,
                color: output ? 'var(--semi-color-text-0)' : 'var(--semi-color-text-2)',
              }}>
                {output || (sending ? t('等待响应...') : t('点击"发送请求"查看响应'))}
              </pre>
            )}
          </Card>
        </div>
      </div>
    </div>
  );
}
