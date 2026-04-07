import React, { useState, useCallback } from 'react';
import { Card, Table, Tag, Button, Space, Input, Select, Typography } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconEyeOpened } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';

const { Title, Text } = Typography;

const STATUS_COLORS = {
  succeeded: 'green',
  failed: 'red',
  running: 'blue',
  accepted: 'blue',
  queued: 'orange',
  canceled: 'grey',
  expired: 'grey',
};

const STATUS_LABELS = {
  succeeded: '成功',
  failed: '失败',
  running: '运行中',
  accepted: '已接受',
  queued: '排队中',
  canceled: '已取消',
  expired: '已过期',
};

const STATUS_OPTIONS = Object.entries(STATUS_LABELS).map(([v, l]) => ({
  value: v,
  label: l,
}));

function formatDuration(createdAt, finalizedAt) {
  if (!finalizedAt || finalizedAt === 0) return '-';
  const sec = finalizedAt - createdAt;
  if (sec < 0) return '-';
  if (sec < 60) return `${sec}s`;
  return `${Math.floor(sec / 60)}m${sec % 60}s`;
}

function ResponseDetailModal({ record, onClose }) {
  const { t } = useTranslation();
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);

  const safeParse = useCallback((str) => {
    if (!str) return null;
    try {
      return JSON.parse(str);
    } catch {
      return str;
    }
  }, []);

  React.useEffect(() => {
    if (!record) return;
    setLoading(true);
    API.get(`/api/bg/responses/${record.response_id}`)
      .then((res) => {
        if (res.data?.success) setDetail(res.data.data);
        else showError(res.data?.message || t('获取详情失败'));
      })
      .catch(() => showError(t('获取详情失败')))
      .finally(() => setLoading(false));
  }, [record?.response_id]);

  if (!record) return null;

  return (
    <div
      style={{
        position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.45)',
        zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center',
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: 'var(--semi-color-bg-0)', borderRadius: 12,
          width: '90vw', maxWidth: 860, maxHeight: '85vh',
          overflow: 'auto', padding: 28, boxShadow: 'var(--semi-shadow-elevated)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 20 }}>
          <Title heading={5} style={{ margin: 0 }}>{t('请求详情')}</Title>
          <Button type='tertiary' onClick={onClose}>{t('关闭')}</Button>
        </div>
        {loading ? (
          <div style={{ textAlign: 'center', padding: 40 }}>{t('加载中…')}</div>
        ) : detail ? (
          <div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 16px', marginBottom: 20 }}>
              {[
                [t('ID'), detail.response_id],
                [t('状态'), <Tag color={STATUS_COLORS[detail.status]}>{STATUS_LABELS[detail.status] ?? detail.status}</Tag>],
                [t('模型'), detail.model],
                [t('提供商'), detail.provider],
                [t('计费模式'), detail.billing_mode],
                [t('创建时间'), timestamp2string(detail.created_at)],
                [t('耗时'), formatDuration(detail.created_at, detail.finalized_at)],
              ].map(([k, v]) => (
                <div key={k}>
                  <Text type='tertiary' size='small'>{k}</Text>
                  <div style={{ marginTop: 2 }}>{v}</div>
                </div>
              ))}
            </div>
            {detail.attempts?.length > 0 && (
              <div style={{ marginBottom: 16 }}>
                <Text strong>{t('尝试记录')} ({detail.attempts.length})</Text>
                <Table
                  size='small'
                  dataSource={detail.attempts}
                  rowKey='attempt_id'
                  style={{ marginTop: 8 }}
                  columns={[
                    { title: t('尝试ID'), dataIndex: 'attempt_id', width: 120 },
                    { title: t('适配器'), dataIndex: 'adapter_name' },
                    { title: t('状态'), dataIndex: 'status', render: (v) => <Tag color={STATUS_COLORS[v]}>{STATUS_LABELS[v] ?? v}</Tag> },
                    { title: t('轮询次数'), dataIndex: 'poll_count', width: 90 },
                  ]}
                  pagination={false}
                />
              </div>
            )}
            {(detail.output_json || detail.input_json) && (
              <div>
                <Text strong>{t('输入 / 输出')}</Text>
                <details style={{ marginTop: 8 }}>
                  <summary style={{ cursor: 'pointer', color: 'var(--semi-color-primary)' }}>{t('展开 JSON')}</summary>
                  <pre style={{
                    background: 'var(--semi-color-fill-0)', padding: 12, borderRadius: 8,
                    fontSize: 12, overflow: 'auto', maxHeight: 300, marginTop: 8,
                  }}>
                    {JSON.stringify({ input: safeParse(detail.input_json), output: safeParse(detail.output_json) }, null, 2)}
                  </pre>
                </details>
              </div>
            )}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export default function BgResponsesPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [status, setStatus] = useState('');
  const [model, setModel] = useState('');
  const [selectedRecord, setSelectedRecord] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ p: page, page_size: pageSize });
      if (status) params.set('status', status);
      if (model) params.set('model', model);
      const res = await API.get(`/api/bg/responses?${params}`);
      if (res.data?.success) {
        const d = res.data.data;
        setRows(d.items ?? []);
        setTotal(d.total ?? 0);
      } else {
        showError(res.data?.message || t('获取失败'));
      }
    } catch (e) {
      showError(t('获取请求列表失败'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, status, model]);

  React.useEffect(() => { fetchData(); }, [fetchData]);

  const columns = [
    {
      title: 'Response ID',
      dataIndex: 'response_id',
      width: 220,
      render: (v) => (
        <Text code style={{ fontSize: 11 }}>{v}</Text>
      ),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 90,
      render: (v) => <Tag color={STATUS_COLORS[v]}>{STATUS_LABELS[v] ?? v}</Tag>,
    },
    { title: t('模型'), dataIndex: 'model', width: 180 },
    { title: t('计费模式'), dataIndex: 'billing_mode', width: 90 },
    {
      title: t('创建时间'),
      dataIndex: 'created_at',
      width: 160,
      render: timestamp2string,
    },
    {
      title: t('耗时'),
      dataIndex: 'finalized_at',
      width: 80,
      render: (v, r) => formatDuration(r.created_at, v),
    },
    {
      title: t('操作'),
      width: 80,
      render: (_, r) => (
        <Button
          size='small'
          type='tertiary'
          icon={<IconEyeOpened />}
          onClick={() => setSelectedRecord(r)}
        />
      ),
    },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '0 auto' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 请求日志')}</Title>
        <Text type='tertiary' size='small'>{t('查看所有 BaseGate 异步/同步请求的执行状态')}</Text>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: 0 }}>
        <div style={{ padding: '16px 20px 8px' }}>
          <Space wrap>
            <Select
              value={status}
              onChange={setStatus}
              optionList={[{ value: '', label: t('全部状态') }, ...STATUS_OPTIONS]}
              style={{ width: 130 }}
              placeholder={t('状态')}
            />
            <Input
              value={model}
              onChange={setModel}
              placeholder={t('按模型筛选')}
              style={{ width: 220 }}
              prefix={<IconSearch size='small' />}
              showClear
            />
            <Button icon={<IconRefresh />} onClick={fetchData} loading={loading} type='tertiary'>
              {t('刷新')}
            </Button>
          </Space>
        </div>
        <Table
          columns={columns}
          dataSource={rows}
          loading={loading}
          rowKey='response_id'
          pagination={{
            currentPage: page,
            pageSize,
            total,
            onPageChange: setPage,
            onPageSizeChange: setPageSize,
            pageSizeOpts: [10, 20, 50],
            showSizeChanger: true,
            showTotal: true,
          }}
          scroll={{ x: 'max-content' }}
          size='middle'
        />
      </Card>

      {selectedRecord && (
        <ResponseDetailModal
          record={selectedRecord}
          onClose={() => setSelectedRecord(null)}
        />
      )}
    </div>
  );
}
