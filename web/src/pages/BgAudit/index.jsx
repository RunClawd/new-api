import React, { useState, useCallback, useEffect } from 'react';
import { Card, Table, Tag, Button, Space, Input, Select, Typography, DatePicker } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';

const { Title, Text } = Typography;

const EVENT_TYPES = [
  'response_created',
  'response_finalized',
  'response_canceled',
  'session_closed',
];

export default function BgAuditPage() {
  const { t } = useTranslation();
  const [rows, setRows]       = useState([]);
  const [total, setTotal]     = useState(0);
  const [loading, setLoading] = useState(false);
  const [page, setPage]       = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [eventType, setEventType] = useState('');
  const [responseId, setResponseId] = useState('');
  const [requestId, setRequestId]   = useState('');
  const [dateRange, setDateRange]   = useState([]);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ p: page, page_size: pageSize });
      if (eventType)  params.set('event_type',  eventType);
      if (responseId) params.set('response_id', responseId);
      if (requestId)  params.set('request_id',  requestId);
      if (dateRange?.length === 2 && dateRange[0] && dateRange[1]) {
        params.set('start_timestamp', Math.floor(dateRange[0].valueOf() / 1000));
        params.set('end_timestamp',   Math.floor(dateRange[1].valueOf() / 1000));
      }
      const res = await API.get(`/api/bg/audit?${params}`);
      if (res.data?.success) {
        setRows(res.data.data.items ?? []);
        setTotal(res.data.data.total ?? 0);
      } else {
        showError(res.data?.message || t('获取失败'));
      }
    } catch {
      showError(t('获取审计日志失败'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, eventType, responseId, requestId, dateRange, t]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const columns = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    {
      title: t('事件类型'),
      dataIndex: 'event_type',
      width: 160,
      render: v => {
        const colors = {
          response_created: 'blue',
          response_finalized: 'green',
          response_canceled: 'orange',
          session_closed: 'purple',
        };
        return <Tag color={colors[v] ?? 'grey'}>{v}</Tag>;
      },
    },
    { title: t('Org ID'), dataIndex: 'org_id', width: 80 },
    { title: 'Response ID', dataIndex: 'response_id', width: 210, render: v => v ? <Text code>{v}</Text> : '-' },
    { title: 'Request ID',  dataIndex: 'request_id',  width: 210, render: v => v ? <Text code>{v}</Text> : '-' },
    {
      title: t('详情'),
      dataIndex: 'detail_json',
      ellipsis: true,
      render: (v) => {
        if (!v) return '-';
        try {
          const obj = JSON.parse(v);
          return (
            <details style={{ cursor: 'pointer' }}>
              <summary style={{ color: 'var(--semi-color-text-2)', fontSize: 12 }}>
                {Object.keys(obj).slice(0, 2).map(k => `${k}: ${obj[k]}`).join(', ')}
              </summary>
              <pre style={{ fontSize: 11, margin: '4px 0 0', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                {JSON.stringify(obj, null, 2)}
              </pre>
            </details>
          );
        } catch {
          return <Text size='small' type='tertiary'>{v}</Text>;
        }
      },
    },
    { title: t('时间'), dataIndex: 'created_at', width: 155, render: timestamp2string },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '60px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 审计日志')}</Title>
        <Text type='tertiary' size='small'>{t('系统操作全生命周期审计记录')}</Text>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: 0 }}>
        <div style={{ padding: '16px 20px 8px' }}>
          <Space wrap>
            <DatePicker
              type='dateTimeRange'
              value={dateRange}
              onChange={setDateRange}
              style={{ width: 340 }}
              placeholder={[t('开始时间'), t('结束时间')]}
              showClear
            />
            <Select
              value={eventType}
              onChange={setEventType}
              optionList={[{ value: '', label: t('全部事件') }, ...EVENT_TYPES.map(e => ({ value: e, label: e }))]}
              style={{ width: 200 }}
              placeholder={t('事件类型')}
              showClear
            />
            <Input value={responseId} onChange={setResponseId} placeholder='Response ID' style={{ width: 200 }} showClear />
            <Input value={requestId}  onChange={setRequestId}  placeholder='Request ID'  style={{ width: 200 }} showClear />
            <Button icon={<IconRefresh />} onClick={fetchData} loading={loading} type='tertiary'>{t('刷新')}</Button>
          </Space>
        </div>
        <Table
          columns={columns}
          dataSource={rows}
          loading={loading}
          rowKey='id'
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
    </div>
  );
}
