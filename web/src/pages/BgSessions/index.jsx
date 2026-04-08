import React, { useState, useCallback } from 'react';
import { Card, Table, Tag, Button, Space, Input, Select, Typography, Tabs, TabPane, DatePicker, Modal } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh, IconEyeOpened, IconStop } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';

const { Title, Text } = Typography;

const STATUS_COLORS = {
  active: 'green',
  idle: 'orange',
  closed: 'grey',
  failed: 'red',
  creating: 'blue',
  expired: 'light-blue',
};

const STATUS_LABELS = {
  active: '活跃',
  idle: '闲置',
  closed: '已关闭',
  failed: '失败',
  creating: '创建中',
  expired: '已过期',
};

const STATUS_OPTIONS = Object.entries(STATUS_LABELS).map(([v, l]) => ({
  value: v,
  label: l,
}));

function SessionDetailModal({ record, onClose, onRefresh }) {
  const { t } = useTranslation();
  const [detail, setDetail] = useState(null);
  const [loading, setLoading] = useState(false);
  const [closing, setClosing] = useState(false);

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
    API.get(`/api/bg/sessions/${record.session_id}`)
      .then((res) => {
        if (res.data?.success) setDetail(res.data.data);
        else showError(res.data?.message || t('获取详情失败'));
      })
      .catch(() => showError(t('获取详情失败')))
      .finally(() => setLoading(false));
  }, [record?.session_id]);

  const handleClose = () => {
    Modal.confirm({
      title: t('确认关闭该会话？'),
      content: t('关闭会话将终止上游连接并触发计费。'),
      onOk: () => {
        setClosing(true);
        API.post(`/api/bg/sessions/${record.session_id}/close`)
          .then((res) => {
            if (res.data?.success) {
              setDetail(prev => ({...prev, status: 'closed'}));
              if (onRefresh) onRefresh();
            } else {
              showError(res.data?.message || t('关闭失败'));
            }
          })
          .catch(() => showError(t('网络错误')))
          .finally(() => setClosing(false));
      }
    });
  };

  if (!record) return null;

  const canClose = detail && ['active', 'idle', 'creating'].includes(detail.status);

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
          <Title heading={5} style={{ margin: 0 }}>{t('会话详情')} - {record.session_id}</Title>
          <Space>
            {canClose && (
              <Button type='danger' loading={closing} icon={<IconStop />} onClick={handleClose}>{t('关闭会话')}</Button>
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
                    [t('ID'), detail.session_id],
                    [t('状态'), <Tag color={STATUS_COLORS[detail.status]}>{STATUS_LABELS[detail.status] ?? detail.status}</Tag>],
                    [t('模型'), detail.model],
                    [t('提供商 Session ID'), detail.provider_session_id],
                    [t('使用适配器'), detail.adapter_name],
                    [t('创建时间'), timestamp2string(detail.created_at)],
                    [t('过期时间'), timestamp2string(detail.expires_at)],
                  ].map(([k, v]) => (
                    <div key={k}>
                      <Text type='tertiary' size='small'>{k}</Text>
                      <div style={{ marginTop: 4 }}>{v}</div>
                    </div>
                  ))}
                </div>
              </TabPane>
              
              <TabPane tab={t('执行动作历史')} itemKey="actions">
                <Table
                  size='small'
                  dataSource={detail.actions || []}
                  rowKey='action_id'
                  style={{ marginTop: 16 }}
                  columns={[
                    { title: t('动作ID'), dataIndex: 'action_id', width: 200 },
                    { title: t('类型'), dataIndex: 'action_type' },
                    { title: t('状态'), dataIndex: 'status', render: (v) => <Tag color={v==='succeeded'?'green':(v==='failed'?'red':'blue')}>{v}</Tag> },
                    { title: t('开始时间'), dataIndex: 'started_at', render: timestamp2string },
                    { 
                      title: t('耗时'), 
                      render: (_, r) => ((r.completed_at && r.completed_at > 0 && r.started_at > 0) ? `${r.completed_at - r.started_at}s` : '-') 
                    },
                    { 
                      title: t('详情'), 
                      render: (_, r) => (
                        <details style={{ cursor: 'pointer' }}>
                          <summary style={{ color: 'var(--semi-color-primary)', outline: 'none' }}>{t('查看 JSON')}</summary>
                          <pre style={{ fontSize: 10, padding: 8, background: 'var(--semi-color-fill-0)', borderRadius: 4, maxWidth: 300, overflow: 'auto' }}>
                            {JSON.stringify({
                              input: safeParse(r.input_json),
                              output: safeParse(r.output_json),
                              error: safeParse(r.error_json),
                            }, null, 2)}
                          </pre>
                        </details>
                      ) 
                    },
                  ]}
                  pagination={false}
                  empty={t('暂无执行动作记录')}
                />
              </TabPane>

            </Tabs>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export default function BgSessionsPage() {
  const { t } = useTranslation();
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [status, setStatus] = useState('');
  const [model, setModel] = useState('');
  const [dateRange, setDateRange] = useState([]);
  const [selectedRecord, setSelectedRecord] = useState(null);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams({ p: page, page_size: pageSize });
      if (status) params.set('status', status);
      if (model) params.set('model', model);
      if (dateRange && dateRange.length === 2 && dateRange[0] && dateRange[1]) {
        params.set('start_timestamp', Math.floor(dateRange[0].valueOf() / 1000));
        params.set('end_timestamp', Math.floor(dateRange[1].valueOf() / 1000));
      }
      const res = await API.get(`/api/bg/sessions?${params}`);
      if (res.data?.success) {
        const d = res.data.data;
        setRows(d.items ?? []);
        setTotal(d.total ?? 0);
      } else {
        showError(res.data?.message || t('获取失败'));
      }
    } catch (e) {
      showError(t('获取会话列表失败'));
    } finally {
      setLoading(false);
    }
  }, [page, pageSize, status, model, dateRange]);

  React.useEffect(() => { fetchData(); }, [fetchData]);

  const columns = [
    {
      title: 'Session ID',
      dataIndex: 'session_id',
      width: 200,
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
    { title: t('模型'), dataIndex: 'model', width: 140 },
    { title: t('适配器'), dataIndex: 'adapter_name', width: 120 },
    {
      title: t('创建时间'),
      dataIndex: 'created_at',
      width: 150,
      render: timestamp2string,
    },
    {
      title: t('过期时间'),
      dataIndex: 'expires_at',
      width: 150,
      render: timestamp2string,
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
    <div style={{ padding: '24px', maxWidth: 1400, margin: '64px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 会话管理')}</Title>
        <Text type='tertiary' size='small'>{t('查看和管理长时间运行的沙盒会话')}</Text>
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
          rowKey='session_id'
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
        <SessionDetailModal
          record={selectedRecord}
          onClose={() => setSelectedRecord(null)}
          onRefresh={fetchData}
        />
      )}
    </div>
  );
}
