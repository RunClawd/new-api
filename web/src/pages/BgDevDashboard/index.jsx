import React, { useState, useEffect, useCallback } from 'react';
import {
  Card,
  Table,
  Typography,
  Toast,
  Tag,
  Descriptions,
  Button,
  Modal,
  Space,
} from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { VChart } from '@visactor/react-vchart';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text } = Typography;

const STATUS_COLORS = {
  succeeded: 'green',
  failed: 'red',
  queued: 'blue',
  running: 'orange',
  canceled: 'grey',
};

export default function BgDevDashboard() {
  const { t } = useTranslation();

  const [stats, setStats] = useState(null);
  const [responses, setResponses] = useState([]);
  const [trend, setTrend] = useState([]);
  const [loading, setLoading] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  const [detailData, setDetailData] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const now = Math.floor(Date.now() / 1000);
      const start24h = now - 86400;
      const formatDate = (ts) => {
        const d = new Date(ts * 1000);
        return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
      };

      const [usageRes, respRes, trendRes] = await Promise.allSettled([
        API.get('/api/bg/dev/usage'),
        API.get('/api/bg/dev/responses?size=50'),
        API.get(`/api/bg/dev/usage?start_date=${formatDate(start24h)}&end_date=${formatDate(now)}&granularity=hour&limit=100&offset=0`),
      ]);

      if (usageRes.status === 'fulfilled' && usageRes.value.data?.success) {
        setStats(usageRes.value.data.data);
      }
      if (respRes.status === 'fulfilled' && respRes.value.data?.success) {
        setResponses(respRes.value.data.data?.items || []);
      }
      if (trendRes.status === 'fulfilled' && trendRes.value.data?.success) {
        setTrend(trendRes.value.data.data?.items || []);
      }
    } catch {
      showError(t('加载数据失败'));
    }
    setLoading(false);
  }, [t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const showDetail = (row) => {
    setDetailData(row);
    setDetailVisible(true);
  };

  const successRate = stats
    ? stats.total_requests === 0 ? '—' : `${((stats.succeeded_count / stats.total_requests) * 100).toFixed(1)}%`
    : '—';

  const chartSpec = trend.length > 0 ? {
    type: 'line',
    data: [{ id: 'trend', values: trend }],
    xField: 'time_bucket',
    yField: 'total_requests',
    seriesField: 'model',
    point: { style: { size: 3 } },
    legends: { visible: true, orient: 'bottom' },
    axes: [
      { orient: 'left', title: { visible: true, text: t('请求数') } },
      { orient: 'bottom' },
    ],
  } : null;

  const columns = [
    {
      title: t('Response ID'),
      dataIndex: 'response_id',
      width: 220,
      render: (v) => (
        <Text
          link={{ onClick: () => showDetail(responses.find((r) => r.response_id === v)) }}
          code
          style={{ fontSize: 12, cursor: 'pointer' }}
        >
          {v}
        </Text>
      ),
    },
    { title: t('模型'), dataIndex: 'model', width: 200 },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 100,
      render: (v) => <Tag color={STATUS_COLORS[v] || 'grey'}>{v}</Tag>,
    },
    {
      title: t('Token'),
      width: 100,
      render: (_, r) => r.total_tokens || r.usage?.total_tokens || '-',
    },
    {
      title: t('创建时间'),
      dataIndex: 'created_at',
      width: 180,
      render: (v) => (v ? new Date(v * 1000).toLocaleString() : '-'),
    },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '60px auto 0' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20 }}>
        <div>
          <Title heading={4} style={{ margin: 0 }}>{t('开发者总览')}</Title>
          <Text type='tertiary' size='small'>{t('您的 BaseGate 使用概览')}</Text>
        </div>
        <Button icon={<IconRefresh />} onClick={fetchData} loading={loading} type='tertiary'>{t('刷新')}</Button>
      </div>

      {/* Stats KPI row */}
      {stats && (
        <div style={{ display: 'flex', gap: 16, marginBottom: 20, flexWrap: 'wrap' }}>
          {[
            { label: t('总请求数'), value: stats.total_requests || 0 },
            { label: t('成功'), value: stats.succeeded_count || 0, color: 'green' },
            { label: t('失败'), value: stats.failed_count || 0, color: 'red' },
            { label: t('运行中'), value: stats.running_count || 0, color: 'orange' },
            { label: t('成功率'), value: successRate },
            { label: t('Token 消耗'), value: (stats.total_tokens || 0).toLocaleString() },
          ].map((item) => (
            <Card key={item.label} shadows='hover' style={{ flex: 1, minWidth: 150, borderRadius: 12 }}>
              <Text type='tertiary' size='small'>{item.label}</Text>
              <Title heading={3} style={{ margin: '4px 0 0', color: item.color ? `var(--semi-color-${item.color})` : undefined }}>{item.value}</Title>
            </Card>
          ))}
        </div>
      )}

      {/* Trend chart */}
      {chartSpec && (
        <Card shadows='hover' style={{ borderRadius: 12, marginBottom: 20 }} bodyStyle={{ padding: 16 }}>
          <Text strong>{t('最近 24h 请求趋势')}</Text>
          <div style={{ height: 260, marginTop: 12 }}>
            <VChart spec={chartSpec} />
          </div>
        </Card>
      )}

      {/* Recent responses */}
      <Card title={t('最近请求')} shadows='hover' style={{ borderRadius: 12 }}>
        <Table
          columns={columns}
          dataSource={responses}
          loading={loading}
          pagination={false}
          rowKey='response_id'
          size='small'
          onRow={(record) => ({
            style: { cursor: 'pointer' },
            onClick: () => showDetail(record),
          })}
        />
      </Card>

      {/* Response detail modal */}
      <Modal
        title={t('请求详情')}
        visible={detailVisible}
        onCancel={() => setDetailVisible(false)}
        footer={<Button onClick={() => setDetailVisible(false)}>{t('关闭')}</Button>}
        width={640}
      >
        {detailData && (
          <Descriptions row>
            {[
              { key: t('Response ID'), value: detailData.response_id },
              { key: t('模型'), value: detailData.model },
              { key: t('状态'), value: detailData.status },
              { key: t('执行模式'), value: detailData.execution_mode || '-' },
              { key: t('创建时间'), value: detailData.created_at ? new Date(detailData.created_at * 1000).toLocaleString() : '-' },
              { key: t('完成时间'), value: detailData.finalized_at ? new Date(detailData.finalized_at * 1000).toLocaleString() : '-' },
              { key: t('Token (总)'), value: detailData.total_tokens || detailData.usage?.total_tokens || '-' },
              { key: t('Prompt Tokens'), value: detailData.prompt_tokens || detailData.usage?.prompt_tokens || '-' },
              { key: t('Completion Tokens'), value: detailData.completion_tokens || detailData.usage?.completion_tokens || '-' },
            ].map((d) => (
              <Descriptions.Item key={d.key} itemKey={d.key}>{d.value}</Descriptions.Item>
            ))}
          </Descriptions>
        )}
        {detailData?.error_json && (
          <Card style={{ marginTop: 12 }} title={t('错误详情')}>
            <pre style={{ margin: 0, fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
              {typeof detailData.error_json === 'string' ? detailData.error_json : JSON.stringify(detailData.error_json, null, 2)}
            </pre>
          </Card>
        )}
      </Modal>
    </div>
  );
}
