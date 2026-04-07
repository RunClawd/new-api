import React from 'react';
import { Card, Row, Col, Spin } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { Activity, CheckCircle, RotateCcw, DollarSign } from 'lucide-react';

function StatCard({ title, value, loading, icon, color, suffix }) {
  return (
    <Card
      shadows='hover'
      style={{ borderRadius: 12, height: '100%' }}
      bodyStyle={{ padding: '20px 24px' }}
    >
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 16 }}>
        <div
          style={{
            width: 44,
            height: 44,
            borderRadius: 10,
            background: `${color}18`,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
          }}
        >
          {React.cloneElement(icon, { size: 20, style: { color } })}
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12,
              color: 'var(--semi-color-text-2)',
              marginBottom: 4,
              fontWeight: 500,
            }}
          >
            {title}
          </div>
          {loading ? (
            <Spin size='small' />
          ) : (
            <div
              style={{
                fontSize: 24,
                fontWeight: 700,
                color: 'var(--semi-color-text-0)',
                lineHeight: 1.2,
              }}
            >
              {value}
              {suffix && (
                <span style={{ fontSize: 13, fontWeight: 400, marginLeft: 4, color: 'var(--semi-color-text-2)' }}>
                  {suffix}
                </span>
              )}
            </div>
          )}
        </div>
      </div>
    </Card>
  );
}

export default function BgUsageStats({ stats, loading }) {
  const { t } = useTranslation();
  const totalRequests = stats?.total_requests ?? '-';
  const succeededCount = stats?.succeeded_count ?? '-';
  const runningCount = stats?.running_count ?? '-';
  const totalCost = stats?.total_cost != null ? `$${Number(stats.total_cost).toFixed(4)}` : '-';
  const totalTokens = stats?.total_tokens != null
    ? Number(stats.total_tokens).toLocaleString()
    : '-';

  const successRate =
    stats?.total_requests > 0
      ? `${((stats.succeeded_count / stats.total_requests) * 100).toFixed(1)}%`
      : '-';

  return (
    <Row gutter={[16, 16]} style={{ marginBottom: 24 }}>
      <Col xs={24} sm={12} lg={6}>
        <StatCard
          title={t('总请求数')}
          value={totalRequests.toLocaleString?.() ?? totalRequests}
          loading={loading}
          icon={<Activity />}
          color='#1664FF'
        />
      </Col>
      <Col xs={24} sm={12} lg={6}>
        <StatCard
          title={t('成功率')}
          value={successRate}
          loading={loading}
          icon={<CheckCircle />}
          color='#06a77d'
        />
      </Col>
      <Col xs={24} sm={12} lg={6}>
        <StatCard
          title={t('进行中')}
          value={runningCount.toLocaleString?.() ?? runningCount}
          loading={loading}
          icon={<RotateCcw />}
          color='#FF8A00'
        />
      </Col>
      <Col xs={24} sm={12} lg={6}>
        <StatCard
          title={t('总消费')}
          value={totalCost}
          loading={loading}
          icon={<DollarSign />}
          color='#7442D4'
          suffix={totalTokens !== '-' ? `${totalTokens} tokens` : undefined}
        />
      </Col>
    </Row>
  );
}
