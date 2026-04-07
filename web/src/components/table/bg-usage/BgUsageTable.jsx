import React from 'react';
import { Table } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { getBgUsageColumns } from './BgUsageColumnDefs';

export default function BgUsageTable({
  rows,
  total,
  loading,
  page,
  pageSize,
  granularity,
  onPageChange,
  onPageSizeChange,
}) {
  const { t } = useTranslation();
  const columns = getBgUsageColumns(granularity, true, t);

  return (
    <Table
      columns={columns}
      dataSource={rows}
      loading={loading}
      rowKey={(r, i) => `${r.time_bucket}-${r.model}-${i}`}
      pagination={{
        currentPage: page,
        pageSize,
        total,
        onPageChange,
        onPageSizeChange,
        pageSizeOpts: [10, 20, 50, 100],
        showSizeChanger: true,
        showTotal: true,
      }}
      scroll={{ x: 'max-content' }}
      size='middle'
      empty={
        <div style={{ padding: '32px 0', textAlign: 'center', color: 'var(--semi-color-text-2)' }}>
          {t('暂无用量数据')}
        </div>
      }
    />
  );
}
