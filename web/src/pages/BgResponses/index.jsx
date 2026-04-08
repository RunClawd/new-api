import React, { useState, useCallback } from 'react';
import { Card, Table, Tag, Button, Space, Input, Select, Typography, Tabs, TabPane, DatePicker, Modal } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconEyeOpened, IconStop } from '@douyinfe/semi-icons';
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
  return `${Math.floor(sec / 60)}m${Math.floor(sec % 60)}s`;
}

function ResponseDetailModal({ record, onClose, onRefresh }) {
  const { t } = useTranslation();
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);
  const [canceling, setCanceling] = useState(false);

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

  const handleCancel = () => {
    Modal.confirm({
      title: t('确认取消该请求？'),
      content: t('处于排队或运行中的异步请求可被取消。'),
      onOk: () => {
        setCanceling(true);
        API.post(`/api/bg/responses/${record.response_id}/cancel`)
          .then((res) => {
            if (res.data?.success) {
              // updated locally
              setDetail(prev => ({...prev, status: 'canceled'}));
              if (onRefresh) onRefresh();
            } else {
              showError(res.data?.message || t('取消失败'));
            }
          })
          .catch(() => showError(t('网络错误')))
          .finally(() => setCanceling(false));
      }
    });
  };

  if (!record) return null;

  const canCancel = detail && ['running', 'accepted', 'queued'].includes(detail.status);

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
          display: 'flex', flexDirection: 'column',
          overflow: 'hidden', padding: 0, boxShadow: 'var(--semi-shadow-elevated)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '20px 24px', borderBottom: '1px solid var(--semi-color-border)' }}>
          <Title heading={5} style={{ margin: 0 }}>{t('请求详情')} - {record.response_id}</Title>
          <Space>
            {canCancel && (
              <Button type='danger' loading={canceling} icon={<IconStop />} onClick={handleCancel}>{t('取消请求')}</Button>
            )}
            <Button type='tertiary' onClick={onClose}>{t('关闭')}</Button>
          </Space>
        </div>
        <div style={{ padding: 24, overflow: 'auto', flex: 1 }}>
          {loading ? (
            <div style={{ textAlign: 'center', padding: 40 }}>{t('加载中…')}</div>
          ) : detail ? (
            <Tabs defaultActiveKey="info">
              <TabPane tab={t('基本信息')} itemKey="info">
                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px', marginBottom: 20, marginTop: 16 }}>
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
                      <div style={{ marginTop: 4 }}>{v}</div>
                    </div>
                  ))}
                </div>
                {(detail.output_json || detail.input_json) && (
                  <div style={{ marginTop: 24 }}>
                    <Text strong>{t('输入 / 输出')}</Text>
                    <details style={{ marginTop: 8 }} open>
                      <summary style={{ cursor: 'pointer', color: 'var(--semi-color-primary)', outline: 'none' }}>{t('展开 JSON')}</summary>
                      <pre style={{
                        background: 'var(--semi-color-fill-0)', padding: 12, borderRadius: 8,
                        fontSize: 12, overflow: 'auto', maxHeight: 350, marginTop: 12,
                      }}>
                        {JSON.stringify({ input: safeParse(detail.input_json), output: safeParse(detail.output_json) }, null, 2)}
                      </pre>
                    </details>
                  </div>
                )}
              </TabPane>
              
              <TabPane tab={t('执行尝试')} itemKey="attempts">
                <Table
                  size='small'
                  dataSource={detail.attempts || []}
                  rowKey='attempt_id'
                  style={{ marginTop: 16 }}
                  columns={[
                    { title: t('尝试ID'), dataIndex: 'attempt_id', width: 220 },
                    { title: t('适配器'), dataIndex: 'adapter_name' },
                    { title: t('状态'), dataIndex: 'status', render: (v) => <Tag color={STATUS_COLORS[v]}>{STATUS_LABELS[v] ?? v}</Tag> },
                    { title: t('轮询次数'), dataIndex: 'poll_count', width: 90 },
                    { title: t('错误信息'), dataIndex: 'error', render: (v) => v ? <Text type="danger" size="small">{v}</Text> : '-' },
                  ]}
                  pagination={false}
                  empty={t('暂无尝试记录')}
                />
              </TabPane>

              <TabPane tab={t('用量记录')} itemKey="usage">
                <Table
                  size='small'
                  dataSource={detail.usage_records || []}
                  rowKey='id'
                  style={{ marginTop: 16 }}
                  columns={[
                    { title: t('型号'), dataIndex: 'model' },
                    { title: t('提供商'), dataIndex: 'provider' },
                    { title: t('计量单位'), dataIndex: 'billable_unit' },
                    { title: t('用量'), dataIndex: 'billable_units', render: (v) => <Text code>{v}</Text> },
                  ]}
                  pagination={false}
                  empty={t('暂无用量记录')}
                />
              </TabPane>

              <TabPane tab={t('计费记录')} itemKey="billing">
                <Table
                  size='small'
                  dataSource={detail.billing_records || []}
                  rowKey='id'
                  style={{ marginTop: 16 }}
                  columns={[
                    { title: t('型号'), dataIndex: 'model' },
                    { title: t('币种'), dataIndex: 'currency' },
                    { title: t('数量'), dataIndex: 'quantity' },
                    { title: t('单价'), dataIndex: 'unit_price', render: (v) => <Text>{Number(v).toFixed(6)}</Text> },
                    { title: t('总费用'), dataIndex: 'amount', render: (v) => <Text strong type="danger">{Number(v).toFixed(6)}</Text> },
                  ]}
                  pagination={false}
                  empty={t('暂无计费记录')}
                />
              </TabPane>
            </Tabs>
          ) : null}
        </div>
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
  const [q, setQ] = useState('');
  const [dateRange, setDateRange] = useState([]);
  const [selectedRecord, setSelectedRecord] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ p: page, page_size: pageSize });
      if (status) params.set('status', status);
      if (model) params.set('model', model);
      if (q) params.set('q', q);
      if (dateRange && dateRange.length === 2 && dateRange[0] && dateRange[1]) {
        params.set('start_timestamp', Math.floor(dateRange[0].valueOf() / 1000));
        params.set('end_timestamp', Math.floor(dateRange[1].valueOf() / 1000));
      }
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
  }, [page, pageSize, status, model, q, dateRange]);

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
    <div style={{ padding: '24px', maxWidth: 1400, margin: '60px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 请求日志')}</Title>
        <Text type='tertiary' size='small'>{t('查看所有 BaseGate 异步/同步请求的执行状态')}</Text>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: 0 }}>
        <div style={{ padding: '16px 20px 8px' }}>
          <Space wrap>
            <Input
              value={q}
              onChange={setQ}
              placeholder={t('按 ID 搜索')}
              style={{ width: 220 }}
              prefix={<IconSearch size='small' />}
              showClear
            />
            <DatePicker
              type='dateTimeRange'
              value={dateRange}
              onChange={setDateRange}
              style={{ width: 340 }}
              placeholder={[t('开始时间'), t('结束时间')]}
              showClear
            />
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
              style={{ width: 180 }}
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
            onPageSizeChange: (v) => {
              setPageSize(v);
              setPage(1);
            },
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
          onRefresh={fetchData}
        />
      )}
    </div>
  );
}
