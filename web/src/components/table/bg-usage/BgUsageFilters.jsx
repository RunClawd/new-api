import React from 'react';
import { DatePicker, Select, Input, Button, Space } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { IconSearch, IconRefresh } from '@douyinfe/semi-icons';

export default function BgUsageFilters({
  startTimestamp,
  endTimestamp,
  model,
  granularity,
  onStartChange,
  onEndChange,
  onModelChange,
  onGranularityChange,
  onRefresh,
  loading,
}) {
  const { t } = useTranslation();

  const GRANULARITY_OPTIONS = [
    { label: t('按小时'), value: 'hour' },
    { label: t('按天'), value: 'day' },
    { label: t('按月'), value: 'month' },
  ];
  return (
    <Space wrap style={{ marginBottom: 16 }}>
      <DatePicker
        type='dateRange'
        value={[
          startTimestamp ? new Date(startTimestamp * 1000) : null,
          endTimestamp ? new Date(endTimestamp * 1000) : null,
        ]}
        onChange={(dates) => {
          if (dates && dates[0]) {
            onStartChange(Math.floor(dates[0].getTime() / 1000));
          }
          if (dates && dates[1]) {
            onEndChange(Math.floor(dates[1].getTime() / 1000));
          }
        }}
        placeholder={[t('开始日期'), t('结束日期')]}
        style={{ width: 260 }}
        density='compact'
      />
      <Select
        value={granularity}
        onChange={onGranularityChange}
        optionList={GRANULARITY_OPTIONS}
        style={{ width: 120 }}
        size='default'
        placeholder={t('粒度')}
      />
      <Input
        value={model}
        onChange={onModelChange}
        placeholder={t('按模型筛选')}
        style={{ width: 200 }}
        prefix={<IconSearch size='small' />}
        showClear
      />
      <Button
        icon={<IconRefresh />}
        onClick={onRefresh}
        loading={loading}
        type='tertiary'
      >
        {t('刷新')}
      </Button>
    </Space>
  );
}
