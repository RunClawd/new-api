import React, { useState, useCallback, useEffect } from 'react';
import { Card, Table, Tag, Button, Space, Input, Select, Typography, DatePicker, Modal, Popconfirm } from '@douyinfe/semi-ui';
import { IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess, timestamp2string } from '../../helpers';

const { Title, Text } = Typography;

const STATUS_COLORS = {
  delivered: 'green',
  pending: 'blue',
  delivering: 'blue',
  retrying: 'orange',
  dead: 'red',
};

const STATUS_OPTIONS = ['pending', 'delivering', 'delivered', 'retrying', 'dead'];

export default function BgWebhooksPage() {
  const { t } = useTranslation();
  const [rows, setRows]           = useState([]);
  const [total, setTotal]         = useState(0);
  const [loading, setLoading]     = useState(false);
  const [stats, setStats]         = useState(null);
  const [page, setPage]           = useState(1);
  const [pageSize, setPageSize]   = useState(20);
  const [deliveryStatus, setDeliveryStatus] = useState('');
  const [responseId, setResponseId]         = useState('');
  const [dateRange, setDateRange]           = useState([]);
  const [retrying, setRetrying]             = useState({});
  const [payloadModal, setPayloadModal]     = useState(null);

  const fetchStats = useCallback(async () => {
    try {
      const res = await API.get('/api/bg/webhooks/stats');
      if (res.data?.success) setStats(res.data.data);
    } catch { /* silently ignore */ }
  }, []);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ p: page, page_size: pageSize });
      if (deliveryStatus) params.set('delivery_status', deliveryStatus);
      if (responseId)     params.set('response_id', responseId);
      if (dateRange?.length === 2 && dateRange[0] && dateRange[1]) {
        params.set('start_timestamp', Math.floor(dateRange[0].valueOf() / 1000));
        params.set('end_timestamp',   Math.floor(dateRange[1].valueOf() / 1000));
      }
      const res = await API.get(`/api/bg/webhooks?${params}`);
      if (res.data?.success) {
        setRows(res.data.data.items ?? []);
        setTotal(res.data.data.total ?? 0);
      } else showError(res.data?.message || t('获取失败'));
    } catch { showError(t('获取 Webhook 列表失败')); }
    finally { setLoading(false); }
  }, [page, pageSize, deliveryStatus, responseId, dateRange, t]);

  useEffect(() => { fetchStats(); fetchData(); }, [fetchData]);

  const handleRetry = useCallback(async (eventId) => {
    setRetrying(prev => ({ ...prev, [eventId]: true }));
    try {
      const res = await API.post(`/api/bg/webhooks/${eventId}/retry`);
      if (res.data?.success) {
        showSuccess(t('已重新加入投递队列'));
        await fetchData();
        await fetchStats();
      } else showError(res.data?.message);
    } catch { showError(t('操作失败')); }
    finally { setRetrying(prev => ({ ...prev, [eventId]: false })); }
  }, [fetchData, fetchStats, t]);

  const columns = [
    { title: 'Event ID', dataIndex: 'event_id', width: 220, render: v => <Text code ellipsis>{v}</Text> },
    { title: 'Response ID', dataIndex: 'response_id', width: 210, render: v => v ? <Text code>{v}</Text> : '-' },
    { title: t('事件类型'), dataIndex: 'event_type', width: 160 },
    {
      title: t('投递状态'),
      dataIndex: 'delivery_status',
      width: 110,
      render: v => <Tag color={STATUS_COLORS[v] ?? 'grey'}>{v}</Tag>,
    },
    { title: t('重试次数'), dataIndex: 'retry_count', width: 80, align: 'center' },
    { title: t('下次重试'), dataIndex: 'next_retry_at', width: 155, render: v => v ? timestamp2string(v) : '-' },
    {
      title: 'Payload',
      dataIndex: 'payload_json',
      width: 90,
      align: 'center',
      render: (v, row) => (
        <Button size='small' onClick={() => setPayloadModal({ id: row.event_id, payload: v })}>{t('查看')}</Button>
      ),
    },
    { title: t('创建时间'), dataIndex: 'created_at', width: 155, render: timestamp2string },
    {
      title: t('操作'),
      dataIndex: 'event_id',
      width: 90,
      align: 'center',
      fixed: 'right',
      render: (id, row) => (
        <Popconfirm
          title={t('确认重新投递？')}
          content={t('该事件会被重置为 pending 状态，由 cron 自动投递')}
          onConfirm={() => handleRetry(id)}
        >
          <Button
            size='small'
            type='danger'
            disabled={row.delivery_status !== 'dead'}
            loading={retrying[id]}
          >
            {t('重试')}
          </Button>
        </Popconfirm>
      ),
    },
  ];

  const successRate = stats && stats.total > 0
    ? `${((stats.delivered / stats.total) * 100).toFixed(1)}%`
    : '—';

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '60px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate Webhooks')}</Title>
        <Text type='tertiary' size='small'>{t('Webhook 事件投递记录与重试管理')}</Text>
      </div>

      {/* Stats bar */}
      {stats && (
        <div style={{ display: 'flex', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
          {[
            { label: t('全部'), val: stats.total, color: 'var(--semi-color-text-0)' },
            { label: t('已投递'),  val: stats.delivered, color: 'green' },
            { label: t('待投递'),  val: stats.pending,   color: 'var(--semi-color-primary)' },
            { label: t('重试中'),  val: stats.retrying,  color: 'orange' },
            { label: t('死亡'),    val: stats.dead,      color: 'red' },
            { label: t('成功率'),  val: successRate,     color: 'green' },
          ].map(({ label, val, color }) => (
            <Card key={label} shadows='hover' style={{ flex: 1, minWidth: 80, borderRadius: 10, textAlign: 'center', padding: '8px 12px' }} bodyStyle={{ padding: 0 }}>
              <Text type='tertiary' size='small'>{label}</Text>
              <div style={{ fontSize: 22, fontWeight: 700, color, marginTop: 4 }}>{val}</div>
            </Card>
          ))}
        </div>
      )}

      <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: 0 }}>
        <div style={{ padding: '16px 20px 8px' }}>
          <Space wrap>
            <DatePicker
              type='dateTimeRange'
              value={dateRange}
              onChange={setDateRange}
              style={{ width: 340 }}
              showClear
            />
            <Select
              value={deliveryStatus}
              onChange={setDeliveryStatus}
              optionList={[{ value: '', label: t('全部状态') }, ...STATUS_OPTIONS.map(s => ({ value: s, label: s }))]}
              style={{ width: 160 }}
              showClear
            />
            <Input value={responseId} onChange={setResponseId} placeholder='Response ID' style={{ width: 200 }} showClear />
            <Button icon={<IconRefresh />} onClick={() => { fetchStats(); fetchData(); }} loading={loading} type='tertiary'>{t('刷新')}</Button>
          </Space>
        </div>
        <Table
          columns={columns}
          dataSource={rows}
          loading={loading}
          rowKey='event_id'
          pagination={{
            currentPage: page,
            pageSize,
            total,
            onPageChange: setPage,
            onPageSizeChange: (v) => { setPageSize(v); setPage(1); },
            pageSizeOpts: [10, 20, 50],
            showSizeChanger: true,
            showTotal: true,
          }}
          scroll={{ x: 'max-content' }}
          size='middle'
        />
      </Card>

      {/* Payload modal */}
      <Modal
        title={`Payload — ${payloadModal?.id ?? ''}`}
        visible={!!payloadModal}
        onCancel={() => setPayloadModal(null)}
        footer={null}
        width={640}
      >
        <pre style={{ fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-all', maxHeight: 400, overflow: 'auto' }}>
          {payloadModal?.payload
            ? (() => { try { return JSON.stringify(JSON.parse(payloadModal.payload), null, 2); } catch { return payloadModal.payload; } })()
            : '-'}
        </pre>
      </Modal>
    </div>
  );
}
