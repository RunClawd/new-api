import React from 'react';
import { renderModelTag } from '../../../helpers/render';

function formatTimeBucket(value, granularity) {
  if (!value) return '-';
  const d = new Date(value);
  if (granularity === 'hour') {
    return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
  }
  if (granularity === 'month') {
    return d.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long' });
  }
  return d.toLocaleDateString('zh-CN');
}

function formatNumber(n) {
  if (n == null) return '-';
  return Number(n).toLocaleString();
}

function formatCost(n) {
  if (n == null) return '-';
  return `$${Number(n).toFixed(4)}`;
}

// t is optional — falls back to identity for non-i18n usage
export function getBgUsageColumns(granularity, includeCost = true, t = (s) => s) {
  return [
    {
      title: t('时间段'),
      dataIndex: 'time_bucket',
      key: 'time_bucket',
      width: 160,
      render: (v) => formatTimeBucket(v, granularity),
    },
    {
      title: t('模型'),
      dataIndex: 'model',
      key: 'model',
      width: 200,
      render: (v) => v ? renderModelTag(v) : <span style={{ color: 'var(--semi-color-text-2)' }}>—</span>,
    },
    {
      title: t('总用量'),
      dataIndex: 'total_units',
      key: 'total_units',
      width: 110,
      render: formatNumber,
    },
    {
      title: t('输入 tokens'),
      dataIndex: 'total_input',
      key: 'total_input',
      width: 120,
      render: formatNumber,
    },
    {
      title: t('输出 tokens'),
      dataIndex: 'total_output',
      key: 'total_output',
      width: 120,
      render: formatNumber,
    },
    ...(includeCost
      ? [
          {
            title: t('费用'),
            dataIndex: 'total_cost',
            key: 'total_cost',
            width: 110,
            render: formatCost,
          },
        ]
      : []),
  ];
}

