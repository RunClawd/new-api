import React, { useState, useEffect, useCallback } from 'react';
import { Card, Table, Tag, Typography, Space, Input, Select, Button } from '@douyinfe/semi-ui';
import { IconTick, IconClose, IconSearch, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError } from '../../helpers';

const { Title, Text } = Typography;

const DOMAIN_COLORS = {
  llm: 'blue',
  video: 'purple',
  sandbox: 'orange',
};

const MODE_COLORS = {
  sync: 'cyan',
  stream: 'teal',
  async: 'indigo',
  session: 'violet',
};

export default function BgCapabilitiesPage() {
  const { t } = useTranslation();
  const [allRows, setAllRows] = useState([]);
  const [loading, setLoading] = useState(false);
  const [domain, setDomain] = useState('');
  const [status, setStatus] = useState('');
  const [q, setQ] = useState('');
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);

  const filteredRows = React.useMemo(() => {
    return allRows.filter((r) => {
      if (domain && r.domain !== domain) return false;
      if (status && r.status !== status) return false;
      if (q && !r.capability_name?.toLowerCase().includes(q.toLowerCase())) return false;
      return true;
    });
  }, [allRows, domain, status, q]);

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const res = await API.get(`/api/bg/capabilities?p=1&page_size=1000`);
      if (res.data?.success) {
        setAllRows(res.data.data.items ?? []);
      } else {
        showError(res.data?.message || t('获取失败'));
      }
    } catch (e) {
      showError(t('获取能力列表失败'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const columns = [
    {
      title: t('能力名'),
      dataIndex: 'capability_name',
      render: (v) => (
        <Text code style={{ fontSize: 12 }}>{v}</Text>
      ),
    },
    {
      title: t('领域'),
      dataIndex: 'domain',
      width: 100,
      render: (v) => <Tag color={DOMAIN_COLORS[v] ?? 'grey'}>{v}</Tag>,
    },
    { title: t('动作'), dataIndex: 'action', width: 100 },
    { title: t('级别'), dataIndex: 'tier', width: 90 },
    { title: t('计费单位'), dataIndex: 'billable_unit', width: 110 },
    {
      title: t('支持模式'),
      dataIndex: 'supported_modes',
      width: 200,
      render: (v) => {
        const modes = Array.isArray(v) ? v : (v ? String(v).split(',') : []);
        return (
          <span style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
            {modes.map((m) => (
              <Tag key={m} color={MODE_COLORS[m.trim()] ?? 'grey'} size='small'>
                {m.trim()}
              </Tag>
            ))}
          </span>
        );
      },
    },
    {
      title: t('可取消'),
      dataIndex: 'supports_cancel',
      width: 80,
      render: (v) =>
        v ? (
          <IconTick style={{ color: '#06a77d' }} />
        ) : (
          <IconClose style={{ color: 'var(--semi-color-text-2)' }} />
        ),
    },
    {
      title: t('状态'),
      dataIndex: 'status',
      width: 80,
      render: (v) => (
        <Tag color={v === 'active' ? 'green' : 'grey'}>
          {v === 'active' ? t('启用') : t('停用')}
        </Tag>
      ),
    },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '64px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 能力管理')}</Title>
        <Text type='tertiary' size='small'>{t('已注册的 BaseGate 能力合约，数据来源于能力种子和注册接口')}</Text>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }} bodyStyle={{ padding: 0 }}>
        <div style={{ padding: '16px 20px 8px' }}>
          <Space wrap>
            <Input
              value={q}
              onChange={setQ}
              placeholder={t('搜索能力名')}
              style={{ width: 220 }}
              prefix={<IconSearch size='small' />}
              showClear
            />
            <Select
              value={domain}
              onChange={setDomain}
              optionList={[
                { value: '', label: t('全部领域') },
                ...Object.keys(DOMAIN_COLORS).map((k) => ({ value: k, label: k }))
              ]}
              style={{ width: 130 }}
              placeholder={t('领域')}
            />
            <Select
              value={status}
              onChange={setStatus}
              optionList={[
                { value: '', label: t('全部状态') },
                { value: 'active', label: t('启用') },
                { value: 'deprecated', label: t('停用') },
              ]}
              style={{ width: 130 }}
              placeholder={t('状态')}
            />
            <Button icon={<IconRefresh />} onClick={fetchData} loading={loading} type='tertiary'>
              {t('刷新')}
            </Button>
          </Space>
        </div>
        <Table
          columns={columns}
          dataSource={filteredRows.slice((page - 1) * pageSize, page * pageSize)}
          loading={loading}
          rowKey='capability_name'
          pagination={{
            currentPage: page,
            pageSize,
            total: filteredRows.length,
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
          empty={
            <div style={{ padding: '32px 0', textAlign: 'center', color: 'var(--semi-color-text-2)' }}>
              {t('暂无能力数据，请确保已执行 SeedBgCapabilities')}
            </div>
          }
        />
      </Card>
    </div>
  );
}
