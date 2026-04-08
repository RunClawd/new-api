import React from 'react';
import { Card, Tag, Typography } from '@douyinfe/semi-ui';
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

  const handleRefresh = () => {
    refreshStats();
    refreshTable();
  };

  return (
    <div style={{ padding: '24px', maxWidth: 1400, margin: '0 auto' }}>
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
