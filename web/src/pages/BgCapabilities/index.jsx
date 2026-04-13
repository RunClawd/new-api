import React, { useState, useEffect, useCallback } from 'react';
import { Card, Table, Tag, Typography, Space, Input, Select, Button, Descriptions } from '@douyinfe/semi-ui';
import { IconTick, IconClose, IconSearch, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, isAdmin } from '../../helpers';

const { Title, Text, Paragraph } = Typography;

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
  const [expandedRowKeys, setExpandedRowKeys] = useState([]);

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
      // Admin users see all capabilities (including inactive); dev users see active-only with pricing.
      const url = isAdmin()
        ? '/api/bg/capabilities?p=1&page_size=1000'
        : '/api/bg/dev/capabilities';
      const res = await API.get(url);
      if (res.data?.success) {
        // Admin response is paginated (items), dev response is a flat array
        const data = res.data.data;
        setAllRows(Array.isArray(data) ? data : (data.items ?? []));
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

  // Expandable row render: detail panel with schema preview + example request
  const expandedRowRender = (record) => {
    const modes = Array.isArray(record.supported_modes)
      ? record.supported_modes
      : (record.supported_modes ? String(record.supported_modes).split(',').map(s => s.trim()) : []);

    const examplePayload = JSON.stringify({
      model: record.capability_name,
      input: { messages: [{ role: 'user', content: 'Hello!' }] },
      execution_options: { mode: modes.includes('stream') ? 'stream' : (modes[0] || 'sync') },
    }, null, 2);

    return (
      <div style={{ padding: '12px 24px', background: 'var(--semi-color-fill-0)', borderRadius: 8 }}>
        <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
          {/* Left: capability info */}
          <div style={{ flex: 1, minWidth: 300 }}>
            <Text strong style={{ fontSize: 14, marginBottom: 8, display: 'block' }}>{t('能力详情')}</Text>
            <Descriptions row size='small'>
              <Descriptions.Item itemKey={t('能力名')}><Text code>{record.capability_name}</Text></Descriptions.Item>
              <Descriptions.Item itemKey={t('领域')}>{record.domain}</Descriptions.Item>
              <Descriptions.Item itemKey={t('动作')}>{record.action}</Descriptions.Item>
              <Descriptions.Item itemKey={t('级别')}>{record.tier}</Descriptions.Item>
              <Descriptions.Item itemKey={t('计费单位')}>{record.billable_unit}</Descriptions.Item>
              <Descriptions.Item itemKey={t('可取消')}>{record.supports_cancel ? t('是') : t('否')}</Descriptions.Item>
              <Descriptions.Item itemKey={t('支持模式')}>
                <Space>
                  {modes.map(m => <Tag key={m} color={MODE_COLORS[m] ?? 'grey'} size='small'>{m}</Tag>)}
                </Space>
              </Descriptions.Item>
              <Descriptions.Item itemKey={t('定价模式')}>
                {record.pricing_mode === 'ratio' ? t('按量') : record.pricing_mode === 'price' ? t('按次') : t('未配置')}
              </Descriptions.Item>
              <Descriptions.Item itemKey={t('单价')}>
                {record.unit_price > 0
                  ? (record.pricing_mode === 'price' ? `$${Number(record.unit_price).toFixed(4)}/req` : `$${Number(record.unit_price * 1000000).toFixed(2)}/1M`)
                  : '—'
                }
              </Descriptions.Item>
            </Descriptions>
            {record.description && (
              <div style={{ marginTop: 12 }}>
                <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 4 }}>{t('描述')}</Text>
                <Paragraph style={{ margin: 0 }}>{record.description}</Paragraph>
              </div>
            )}
            {/* Phase 15 schema preview placeholder */}
            {(record.input_schema_json || record.output_schema_json) && (
              <div style={{ marginTop: 12 }}>
                <Text type='tertiary' size='small' style={{ display: 'block', marginBottom: 4 }}>{t('Schema 预览')}</Text>
                {record.input_schema_json && (
                  <div style={{ marginBottom: 8 }}>
                    <Text size='small' strong>{t('输入 Schema')}</Text>
                    <pre style={{ margin: '4px 0', padding: 8, background: 'var(--semi-color-fill-1)', borderRadius: 6, fontSize: 11, maxHeight: 200, overflow: 'auto' }}>
                      {record.input_schema_json}
                    </pre>
                  </div>
                )}
                {record.output_schema_json && (
                  <div>
                    <Text size='small' strong>{t('输出 Schema')}</Text>
                    <pre style={{ margin: '4px 0', padding: 8, background: 'var(--semi-color-fill-1)', borderRadius: 6, fontSize: 11, maxHeight: 200, overflow: 'auto' }}>
                      {record.output_schema_json}
                    </pre>
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Right: example request */}
          <div style={{ flex: 1, minWidth: 300 }}>
            <Text strong style={{ fontSize: 14, marginBottom: 8, display: 'block' }}>{t('示例请求')}</Text>
            <div style={{ position: 'relative' }}>
              <pre style={{
                margin: 0,
                padding: 12,
                background: 'var(--semi-color-fill-1)',
                borderRadius: 8,
                fontSize: 12,
                fontFamily: 'monospace',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-all',
                maxHeight: 300,
                overflow: 'auto',
              }}>
{`curl -X POST /v1/bg/responses \\
  -H "Authorization: Bearer sk-xxx" \\
  -H "Content-Type: application/json" \\
  -d '${examplePayload}'`}
              </pre>
            </div>
          </div>
        </div>
      </div>
    );
  };

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
      title: t('定价模式'),
      dataIndex: 'pricing_mode',
      width: 100,
      render: (v) => {
        const colors = { ratio: 'blue', price: 'green', none: 'grey' };
        const labels = { ratio: t('按量'), price: t('按次'), none: t('未配置') };
        return <Tag color={colors[v] ?? 'grey'}>{labels[v] ?? v}</Tag>;
      },
    },
    {
      title: t('单价'),
      dataIndex: 'unit_price',
      width: 110,
      render: (v, r) => {
        if (!v || v === 0) return <Text type='tertiary'>—</Text>;
        if (r.pricing_mode === 'price') return `$${Number(v).toFixed(4)}/req`;
        // Ratio mode: unit_price is per-token ($)
        return `$${Number(v * 1000000).toFixed(2)}/1M`;
      },
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
          expandedRowKeys={expandedRowKeys}
          onExpandedRowsChange={(keys) => setExpandedRowKeys(keys)}
          expandedRowRender={expandedRowRender}
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
