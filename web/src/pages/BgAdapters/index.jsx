import React, { useState, useCallback, useEffect } from 'react';
import { Card, Table, Tag, Button, Space, Typography, Modal, Switch } from '@douyinfe/semi-ui';
import { IconRefresh, IconUndo } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../helpers';

const { Title, Text } = Typography;

const STATE_COLORS = {
  closed: 'green',
  open: 'red',
  half_open: 'orange',
  'half-open': 'orange',
};

const STATE_LABELS = {
  closed: 'Closed',
  open: 'Open',
  half_open: 'Half-Open',
  'half-open': 'Half-Open',
};

export default function BgAdaptersPage() {
  const { t } = useTranslation();
  const [adapters, setAdapters] = useState([]);
  const [loading, setLoading] = useState(false);
  const [reloading, setReloading] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);

  const fetchAdapters = useCallback(async (isSilent = false) => {
    if (!isSilent) setLoading(true);
    try {
      const res = await API.get('/api/bg/adapters');
      if (res.data?.success) {
        setAdapters(res.data.data ?? []);
      } else {
        if (!isSilent) showError(res.data?.message || t('获取失败'));
      }
    } catch (e) {
      if (!isSilent) showError(t('获取失败'));
    } finally {
      if (!isSilent) setLoading(false);
    }
  }, [t]);

  useEffect(() => { fetchAdapters(); }, [fetchAdapters]);

  useEffect(() => {
    let timer;
    if (autoRefresh) {
      timer = setInterval(() => {
        fetchAdapters(true);
      }, 5000);
    }
    return () => clearInterval(timer);
  }, [autoRefresh, fetchAdapters]);

  const handleReload = async () => {
    setReloading(true);
    try {
      const res = await API.post('/api/bg/adapters/reload');
      if (res.data?.success) {
        showSuccess(t('适配器注册表已重载') + ` (${res.data.data?.adapter_count ?? 0} adapters)`);
        await fetchAdapters();
      } else {
        showError(res.data?.message || t('重载失败'));
      }
    } catch (e) {
      showError(t('重载失败'));
    } finally {
      setReloading(false);
    }
  };

  const handleResetCircuit = async (adapterName) => {
    try {
      const res = await API.post(`/api/bg/adapters/${encodeURIComponent(adapterName)}/reset`);
      if (res.data?.success) {
        showSuccess(`${adapterName} circuit reset`);
        await fetchAdapters();
      } else {
        showError(res.data?.message || t('重置失败'));
      }
    } catch (e) {
      showError(t('重置失败'));
    }
  };

  const columns = [
    {
      title: t('适配器名称'),
      dataIndex: 'name',
      render: (v) => <Text code style={{ fontSize: 12 }}>{v}</Text>,
    },
    {
      title: t('提供商'),
      dataIndex: 'provider',
      width: 100,
      render: (v) => v ? <Tag>{v}</Tag> : <Text type='tertiary'>-</Text>,
    },
    {
      title: t('能力绑定'),
      dataIndex: 'capabilities',
      render: (caps) => (
        <span style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
          {(caps || []).map((c) => (
            <Tag key={c} color='blue' size='small'>{c}</Tag>
          ))}
        </span>
      ),
    },
    {
      title: t('熔断器状态'),
      dataIndex: 'circuit_state',
      width: 120,
      render: (v) => <Tag color={STATE_COLORS[v] ?? 'grey'}>{STATE_LABELS[v] ?? v}</Tag>,
    },
    {
      title: t('失败次数'),
      dataIndex: 'failure_count',
      width: 90,
      render: (v) => v > 0 ? <Text type='danger'>{v}</Text> : <Text type='tertiary'>0</Text>,
    },
    {
      title: t('冷却剩余'),
      dataIndex: 'cooldown_remaining_sec',
      width: 100,
      render: (v) => v > 0 ? `${v}s` : '-',
    },
    {
      title: t('操作'),
      width: 80,
      render: (_, r) => (
        r.circuit_state !== 'closed' ? (
          <Button
            size='small'
            type='warning'
            icon={<IconUndo />}
            onClick={() => {
              Modal.confirm({
                title: t('重置熔断器'),
                content: `${t('确认重置')} ${r.name} ${t('的熔断器状态为 Closed？')}`,
                onOk: () => handleResetCircuit(r.name),
              });
            }}
          >
            Reset
          </Button>
        ) : null
      ),
    },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '64px auto 0' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 20 }}>
        <div>
          <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 适配器')}</Title>
          <Text type='tertiary' size='small'>
            {t('已注册的 BaseGate 提供商适配器及其熔断器状态')}
          </Text>
        </div>
        <Space>
          <Text type='secondary'>{t('自动刷新')}</Text>
          <Switch 
            checked={autoRefresh} 
            onChange={setAutoRefresh} 
            size='small' 
            style={{ marginRight: 8 }}
          />
          <Button icon={<IconRefresh />} onClick={() => fetchAdapters(false)} loading={loading} type='tertiary'>
            {t('刷新')}
          </Button>
          <Button
            onClick={handleReload}
            loading={reloading}
            type='primary'
          >
            {t('重载注册表')}
          </Button>
        </Space>
      </div>

      <Card shadows='hover' style={{ borderRadius: 12 }}>
        <Table
          columns={columns}
          dataSource={adapters}
          loading={loading}
          rowKey='name'
          pagination={false}
          scroll={{ x: 'max-content' }}
          size='middle'
          empty={
            <div style={{ padding: '32px 0', textAlign: 'center', color: 'var(--semi-color-text-2)' }}>
              {t('暂无适配器注册')}
            </div>
          }
        />
      </Card>
    </div>
  );
}
