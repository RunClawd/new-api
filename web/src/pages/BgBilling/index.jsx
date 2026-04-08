import React, { useState, useCallback } from 'react';
import { Card, Table, Tag, Button, Space, Input, Typography, Tabs, TabPane, DatePicker } from '@douyinfe/semi-ui';
import { IconSearch, IconRefresh } from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import { API, showError, timestamp2string } from '../../helpers';
import BgUsageStats from '../../components/table/bg-usage/BgUsageStats';

const { Title, Text } = Typography;

export default function BgBillingPage() {
  const { t } = useTranslation();
  
  const [activeTab, setActiveTab] = useState('records');
  
  // Stats
  const [stats, setStats] = useState(null);
  const [statsLoading, setStatsLoading] = useState(false);

  // Records state
  const [rRows, setRRows] = useState([]);
  const [rTotal, setRTotal] = useState(0);
  const [rLoading, setRLoading] = useState(false);
  const [rPage, setRPage] = useState(1);
  const [rPageSize, setRPageSize] = useState(20);
  const [rOrgId, setROrgId] = useState('');
  const [rResponseId, setRResponseId] = useState('');
  const [rModel, setRModel] = useState('');
  const [rDateRange, setRDateRange] = useState([]);

  // Ledger state
  const [lRows, setLRows] = useState([]);
  const [lTotal, setLTotal] = useState(0);
  const [lLoading, setLLoading] = useState(false);
  const [lPage, setLPage] = useState(1);
  const [lPageSize, setLPageSize] = useState(20);
  const [lOrgId, setLOrgId] = useState('');
  const [lDateRange, setLDateRange] = useState([]);

  const fetchStats = useCallback(async () => {
    setStatsLoading(true);
    try {
      const res = await API.get('/api/bg/usage/stats');
      if (res.data?.success) {
        setStats(res.data.data);
      }
    } catch (e) {
      console.error(e);
    } finally {
      setStatsLoading(false);
    }
  }, []);

  const fetchRecords = useCallback(async () => {
    setRLoading(true);
    try {
      const params = new URLSearchParams({ p: rPage, page_size: rPageSize });
      if (rOrgId) params.set('org_id', rOrgId);
      if (rResponseId) params.set('response_id', rResponseId);
      if (rModel) params.set('model', rModel);
      if (rDateRange && rDateRange.length === 2 && rDateRange[0] && rDateRange[1]) {
        params.set('start_timestamp', Math.floor(rDateRange[0].valueOf() / 1000));
        params.set('end_timestamp', Math.floor(rDateRange[1].valueOf() / 1000));
      }
      const res = await API.get(`/api/bg/billing/records?${params}`);
      if (res.data?.success) {
        setRRows(res.data.data.items ?? []);
        setRTotal(res.data.data.total ?? 0);
      } else {
        showError(res.data?.message || t('获取计费记录失败'));
      }
    } catch (e) {
      showError(t('获取计费记录失败'));
    } finally {
      setRLoading(false);
    }
  }, [rPage, rPageSize, rOrgId, rResponseId, rModel, rDateRange, t]);

  const fetchLedger = useCallback(async () => {
    setLLoading(true);
    try {
      const params = new URLSearchParams({ p: lPage, page_size: lPageSize });
      if (lOrgId) params.set('org_id', lOrgId);
      if (lDateRange && lDateRange.length === 2 && lDateRange[0] && lDateRange[1]) {
        params.set('start_timestamp', Math.floor(lDateRange[0].valueOf() / 1000));
        params.set('end_timestamp', Math.floor(lDateRange[1].valueOf() / 1000));
      }
      const res = await API.get(`/api/bg/billing/ledger?${params}`);
      if (res.data?.success) {
        setLRows(res.data.data.items ?? []);
        setLTotal(res.data.data.total ?? 0);
      } else {
        showError(res.data?.message || t('获取账本记录失败'));
      }
    } catch (e) {
      showError(t('获取账本记录失败'));
    } finally {
      setLLoading(false);
    }
  }, [lPage, lPageSize, lOrgId, lDateRange, t]);

  React.useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  React.useEffect(() => {
    if (activeTab === 'records') fetchRecords();
    else fetchLedger();
  }, [activeTab, fetchRecords, fetchLedger]);

  const rColumns = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: t('组织 ID'), dataIndex: 'org_id', width: 90 },
    { title: 'Response ID', dataIndex: 'response_id', width: 200, render: v => <Text code>{v}</Text> },
    { title: t('模型'), dataIndex: 'model', width: 150 },
    { title: t('数量'), dataIndex: 'quantity', width: 100 },
    { title: t('单价 ($)'), dataIndex: 'unit_price', width: 100, render: v => Number(v).toFixed(6) },
    { title: t('费用 ($)'), dataIndex: 'amount', width: 100, render: v => <Text strong type="danger">{Number(v).toFixed(6)}</Text> },
    { title: t('计费时间'), dataIndex: 'created_at', width: 160, render: timestamp2string },
  ];

  const lColumns = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    { title: t('组织 ID'), dataIndex: 'org_id', width: 90 },
    { title: 'Response ID', dataIndex: 'response_id', width: 200, render: v => v ? <Text code>{v}</Text> : '-' },
    { title: t('金额 ($)'), dataIndex: 'amount', width: 120, render: v => <Text strong type="danger">{Number(-v).toFixed(6)}</Text> },
    { title: t('余额 ($)'), dataIndex: 'balance', width: 120, render: v => <Text strong>{Number(v).toFixed(6)}</Text> },
    { title: t('扣费描述'), dataIndex: 'description', ellipsis: true },
    { title: t('时间'), dataIndex: 'created_at', width: 160, render: timestamp2string },
  ];

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '64px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>{t('BaseGate 计费与账本')}</Title>
        <Text type='tertiary' size='small'>{t('查看所有沙盒会话和请求产生的确切计费账单与余额扣减明细')}</Text>
      </div>

      <BgUsageStats stats={stats} loading={statsLoading} />

      <Card shadows='hover' style={{ borderRadius: 12, marginTop: 24 }} bodyStyle={{ padding: 0 }}>
        <Tabs type="line" activeKey={activeTab} onChange={setActiveTab} style={{ margin: '16px 20px 0' }}>
          <TabPane tab={t('计费明细 (Records)')} itemKey="records" />
          <TabPane tab={t('余额流水 (Ledger)')} itemKey="ledger" />
        </Tabs>
        
        {activeTab === 'records' && (
          <>
            <div style={{ padding: '16px 20px 8px' }}>
              <Space wrap>
                <DatePicker
                  type='dateTimeRange'
                  value={rDateRange}
                  onChange={setRDateRange}
                  style={{ width: 340 }}
                  placeholder={[t('开始时间'), t('结束时间')]}
                  showClear
                />
                <Input value={rOrgId} onChange={setROrgId} placeholder={t('组织 ID')} style={{ width: 120 }} showClear />
                <Input value={rResponseId} onChange={setRResponseId} placeholder={t('Response ID')} style={{ width: 200 }} showClear />
                <Input value={rModel} onChange={setRModel} placeholder={t('模型名称')} style={{ width: 150 }} showClear />
                <Button icon={<IconRefresh />} onClick={fetchRecords} loading={rLoading} type='tertiary'>{t('刷新')}</Button>
              </Space>
            </div>
            <Table
              columns={rColumns}
              dataSource={rRows}
              loading={rLoading}
              rowKey='id'
              pagination={{
                currentPage: rPage,
                pageSize: rPageSize,
                total: rTotal,
                onPageChange: setRPage,
                onPageSizeChange: (v) => { setRPageSize(v); setRPage(1); },
                pageSizeOpts: [10, 20, 50],
                showSizeChanger: true,
                showTotal: true,
              }}
              scroll={{ x: 'max-content' }}
              size='middle'
            />
          </>
        )}

        {activeTab === 'ledger' && (
          <>
            <div style={{ padding: '16px 20px 8px' }}>
              <Space wrap>
                <DatePicker
                  type='dateTimeRange'
                  value={lDateRange}
                  onChange={setLDateRange}
                  style={{ width: 340 }}
                  placeholder={[t('开始时间'), t('结束时间')]}
                  showClear
                />
                <Input value={lOrgId} onChange={setLOrgId} placeholder={t('组织 ID')} style={{ width: 120 }} showClear />
                <Button icon={<IconRefresh />} onClick={fetchLedger} loading={lLoading} type='tertiary'>{t('刷新')}</Button>
              </Space>
            </div>
            <Table
              columns={lColumns}
              dataSource={lRows}
              loading={lLoading}
              rowKey='id'
              pagination={{
                currentPage: lPage,
                pageSize: lPageSize,
                total: lTotal,
                onPageChange: setLPage,
                onPageSizeChange: (v) => { setLPageSize(v); setLPage(1); },
                pageSizeOpts: [10, 20, 50],
                showSizeChanger: true,
                showTotal: true,
              }}
              scroll={{ x: 'max-content' }}
              size='middle'
            />
          </>
        )}
      </Card>
    </div>
  );
}
