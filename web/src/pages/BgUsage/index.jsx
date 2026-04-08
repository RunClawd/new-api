import React, { useState } from 'react';
import { Card, Tag, Typography, Tabs, TabPane } from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import { useTranslation } from 'react-i18next';
import { useBgUsageData } from '../../hooks/bg-usage/useBgUsageData';
import BgUsageStats from '../../components/table/bg-usage/BgUsageStats';
import BgUsageFilters from '../../components/table/bg-usage/BgUsageFilters';
import BgUsageTable from '../../components/table/bg-usage/BgUsageTable';

const { Title } = Typography;

export default function BgUsagePage() {
  const { t } = useTranslation();
  const {
    stats,
    statsLoading,
    rows,
    total,
    tableLoading,
    page,
    pageSize,
    setPage,
    setPageSize,
    startTimestamp,
    endTimestamp,
    model,
    granularity,
    setStartTimestamp,
    setEndTimestamp,
    setModel,
    setGranularity,
    refreshStats,
    refreshTable,
  } = useBgUsageData();

  const [activeChartTab, setActiveChartTab] = useState('requests');

  const handleRefresh = () => {
    refreshStats();
    refreshTable();
  };

  const handleExport = () => {
    if (!rows || rows.length === 0) return;
    const header = ['Date', 'Model', 'Cost', 'Requests', 'Tokens', 'Succeeded', 'Failed'];
    const csvContent = rows.map(r => [
      r.time_bucket, r.model, r.total_cost, r.total_requests, r.total_tokens, r.succeeded_count, r.failed_count
    ].join(','));
    csvContent.unshift(header.join(','));
    const blob = new Blob([csvContent.join('\n')], { type: 'text/csv;charset=utf-8;' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.setAttribute('download', `bg_usage_${model || 'all'}.csv`);
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  };

  const commonChartSpec = {
    data: [{ id: 'barData', values: rows }],
    xField: 'time_bucket',
    seriesField: 'model',
    legends: { visible: true, orient: 'bottom' },
    tooltip: { mark: { title: true, content: [{ key: 'model', value: (d) => String(d.value) }] } }
  };

  const chartSpecRequests = {
    ...commonChartSpec,
    type: 'bar',
    yField: 'total_requests',
    axes: [{ orient: 'left', title: { visible: true, text: t('调用次数') } }, { orient: 'bottom' }],
  };

  const chartSpecCost = {
    ...commonChartSpec,
    type: 'line',
    point: { style: { visible: true } },
    yField: 'total_cost',
    axes: [{ orient: 'left', title: { visible: true, text: t('消耗金额 $') } }, { orient: 'bottom' }],
  };

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '64px auto 0' }}>
      <div style={{ marginBottom: 20 }}>
        <Title heading={4} style={{ margin: 0 }}>
          {t('BaseGate 用量概览')}
        </Title>
        <Typography.Text type='tertiary' size='small' style={{ marginTop: 4 }}>
          {t('跨所有项目和提供商的 BaseGate 请求用量汇总')}
        </Typography.Text>
      </div>

      {/* KPI Cards */}
      <BgUsageStats stats={stats} loading={statsLoading} />

      {/* Adapter Health Strip */}
      {stats?.adapters?.length > 0 && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 20 }}>
          {stats.adapters.map((a) => (
            <Tag
              key={a.name}
              color={a.state === 'closed' ? 'green' : a.state === 'open' ? 'red' : 'orange'}
              size='large'
              style={{ borderRadius: 8, padding: '4px 12px' }}
            >
              {a.name}
              {a.state !== 'closed' && ` (${a.state}${a.failure_count ? ` ×${a.failure_count}` : ''})`}
            </Tag>
          ))}
        </div>
      )}

      {/* Chart Panel */}
      {rows.length > 0 && (
        <Card shadows='hover' style={{ borderRadius: 12, marginBottom: 20 }} bodyStyle={{ padding: 16 }}>
          <Tabs type="line" activeKey={activeChartTab} onChange={setActiveChartTab}>
            <TabPane tab={t('请求数趋势')} itemKey="requests" />
            <TabPane tab={t('用量消耗趋势')} itemKey="cost" />
          </Tabs>
          <div style={{ height: 350, marginTop: 16 }}>
            <VChart
              spec={activeChartTab === 'requests' ? chartSpecRequests : chartSpecCost}
            />
          </div>
        </Card>
      )}

      {/* Table Card */}
      <Card
        title={t('用量明细')}
        shadows='hover'
        style={{ borderRadius: 12 }}
        headerExtraContent={
          <BgUsageFilters
            startTimestamp={startTimestamp}
            endTimestamp={endTimestamp}
            model={model}
            granularity={granularity}
            onStartChange={setStartTimestamp}
            onEndChange={setEndTimestamp}
            onModelChange={setModel}
            onGranularityChange={setGranularity}
            onRefresh={handleRefresh}
            onExport={handleExport}
            loading={tableLoading || statsLoading}
          />
        }
      >
        <BgUsageTable
          rows={rows}
          total={total}
          loading={tableLoading}
          page={page}
          pageSize={pageSize}
          granularity={granularity}
          onPageChange={setPage}
          onPageSizeChange={setPageSize}
        />
      </Card>
    </div>
  );
}
