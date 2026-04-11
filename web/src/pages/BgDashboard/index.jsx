import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Card, Typography, Spin, Button, Tag, Space, Table } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { VChart } from '@visactor/react-vchart';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import BgUsageStats from '../../components/table/bg-usage/BgUsageStats';

const { Title, Text } = Typography;

const STATE_COLORS = { closed: 'green', open: 'red', half_open: 'orange', 'half-open': 'orange' };

export default function BgDashboardPage() {
  const { t } = useTranslation();

  const [stats, setStats]             = useState(null);
  const [adapters, setAdapters]       = useState([]);
  const [failures, setFailures]       = useState([]);
  const [activeCount, setActiveCount] = useState(0);
  const [webhookStats, setWebhookStats] = useState(null);
  const [trend, setTrend]             = useState([]);
  const [loading, setLoading]         = useState(true);
  const [error, setError]             = useState({});

  const fetchAll = useCallback(async () => {
    setLoading(true);
    const now = Math.floor(Date.now() / 1000);
    const start24h = now - 86400;

    const [statsRes, adaptersRes, failuresRes, sessionsRes, webhookRes, trendRes] =
      await Promise.allSettled([
        API.get('/api/bg/usage/stats'),
        API.get('/api/bg/adapters'),
        API.get('/api/bg/responses?status=failed&page_size=5'),
        API.get('/api/bg/sessions?status=active&page_size=1'),
        API.get('/api/bg/webhooks/stats'),
        API.get(`/api/bg/usage?start_date=${formatDate(start24h)}&end_date=${formatDate(now)}&granularity=hour&include_cost=true&limit=100&offset=0`),
      ]);

    const errs = {};
    if (statsRes.status === 'fulfilled' && statsRes.value.data?.success) setStats(statsRes.value.data.data);
    else errs.stats = true;

    if (adaptersRes.status === 'fulfilled' && adaptersRes.value.data?.success) setAdapters(adaptersRes.value.data.data ?? []);
    else errs.adapters = true;

    if (failuresRes.status === 'fulfilled' && failuresRes.value.data?.success) setFailures(failuresRes.value.data.data?.items ?? []);
    else errs.failures = true;

    if (sessionsRes.status === 'fulfilled' && sessionsRes.value.data?.success) setActiveCount(sessionsRes.value.data.data?.total ?? 0);
    else errs.sessions = true;

    if (webhookRes.status === 'fulfilled' && webhookRes.value.data?.success) setWebhookStats(webhookRes.value.data.data);
    else errs.webhooks = true;

    if (trendRes.status === 'fulfilled' && trendRes.value.data?.success) setTrend(trendRes.value.data.data?.items ?? []);
    else errs.trend = true;

    setError(errs);
    setLoading(false);
  }, []);

  useEffect(() => { fetchAll(); }, [fetchAll]);

  const successRate = stats
    ? stats.total_requests === 0 ? 0 : ((stats.succeeded_count / stats.total_requests) * 100).toFixed(1)
    : null;

  const webhookRate = webhookStats
    ? webhookStats.total === 0 ? '—' : `${((webhookStats.delivered / webhookStats.total) * 100).toFixed(1)}%`
    : '—';

  const chartSpec = {
    type: 'line',
    data: [{ id: 'trend', values: trend }],
    xField: 'time_bucket',
    yField: 'total_requests',
    seriesField: 'model',
    point: { style: { size: 4 } },
    legends: { visible: true, orient: 'bottom' },
    axes: [
      { orient: 'left', title: { visible: true, text: t('请求数') } },
      { orient: 'bottom' },
    ],
  };

  const failureCols = [
    { title: 'Response ID', dataIndex: 'response_id', width: 200, render: v => <Text code>{v}</Text> },
    { title: t('模型'), dataIndex: 'model', width: 150 },
    { title: t('时间'), dataIndex: 'created_at', render: timestamp2string },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '60px auto 0' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20 }}>
        <div>
          <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 总览')}</Title>
          <Text type='tertiary' size='small'>{t('系统健康状态一屏总览')}</Text>
        </div>
        <Button icon={<IconRefresh />} onClick={fetchAll} loading={loading} type='tertiary'>{t('刷新')}</Button>
      </div>

      <Spin spinning={loading && !stats}>
        <BgUsageStats stats={stats} loading={loading} />
      </Spin>

      {/* KPI row: active sessions + webhook */}
      <div style={{ display: 'flex', gap: 16, marginBottom: 20, flexWrap: 'wrap' }}>
        <Card shadows='hover' style={{ flex: 1, minWidth: 200, borderRadius: 12 }}>
          <Text type='tertiary' size='small'>{t('活跃 Session 数')}</Text>
          {error.sessions
            ? <Button size='small' onClick={fetchAll}>{t('重试')}</Button>
            : <Title heading={2} style={{ margin: '8px 0 0' }}>{activeCount}</Title>
          }
        </Card>
        <Card shadows='hover' style={{ flex: 1, minWidth: 200, borderRadius: 12 }}>
          <Text type='tertiary' size='small'>{t('Webhook 投递成功率')}</Text>
          {error.webhooks
            ? <Button size='small' onClick={fetchAll}>{t('重试')}</Button>
            : <>
                <Title heading={2} style={{ margin: '8px 0 0' }}>{webhookRate}</Title>
                {webhookStats && <Text type='tertiary' size='small'>{webhookStats.delivered}/{webhookStats.total} delivered</Text>}
              </>
          }
        </Card>
        <Card shadows='hover' style={{ flex: 1, minWidth: 200, borderRadius: 12 }}>
          <Text type='tertiary' size='small'>{t('请求总成功率')}</Text>
          {error.stats
            ? <Button size='small' onClick={fetchAll}>{t('重试')}</Button>
            : <Title heading={2} style={{ margin: '8px 0 0' }}>{successRate !== null ? `${successRate}%` : '—'}</Title>
          }
        </Card>
      </div>

      {/* Adapter health strip */}
      {adapters.length > 0 && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 20 }}>
          {adapters.map(a => (
            <Tag key={a.name} color={STATE_COLORS[a.circuit_state] ?? 'grey'} size='large' style={{ borderRadius: 8, padding: '4px 12px' }}>
              {a.name}{a.circuit_state !== 'closed' ? ` (${a.circuit_state})` : ''}
            </Tag>
          ))}
        </div>
      )}

      {/* Trend chart */}
      {trend.length > 0 && (
        <Card shadows='hover' style={{ borderRadius: 12, marginBottom: 20 }} bodyStyle={{ padding: 16 }}>
          <Text strong>{t('最近 24h 请求趋势')}</Text>
          <div style={{ height: 280, marginTop: 12 }}>
            <VChart spec={chartSpec} />
          </div>
        </Card>
      )}

      {/* Recent failures */}
      <Card title={t('最近失败请求 Top 5')} shadows='hover' style={{ borderRadius: 12 }}>
        {error.failures
          ? <div style={{ textAlign: 'center', padding: 24 }}><Button onClick={fetchAll}>{t('重试')}</Button></div>
          : <Table columns={failureCols} dataSource={failures} rowKey='response_id' pagination={false} size='middle' />
        }
      </Card>
    </div>
  );
}

function formatDate(ts) {
  const d = new Date(ts * 1000);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
